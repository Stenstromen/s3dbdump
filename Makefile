IMAGE_NAME = s3dbdump
IMAGE_TAG = latest
MINIO_ACCESS_KEY = minio
MINIO_SECRET_KEY = minio123
MINIO_BUCKET = dbdumps
NETWORK_NAME = s3dbdump
MINIO_CONTAINER = s3dbdump
DATABASE_CONTAINER = mariadb
TEMP_VOLUME = s3dbdump
DB_CONTAINER = test-mariadb
DB_PASSWORD = password
.PHONY: build test clean test-deps network minio-deploy database

test-deps:
	@which podman >/dev/null 2>&1 || (echo "❌ podman is required but not installed. Aborting." && exit 1)

build: test-deps
	@echo "ℹ️ Building application image..."
	podman build -t localhost/$(IMAGE_NAME):$(IMAGE_TAG) .

network: test-deps
	@echo "ℹ️ Creating podman network $(NETWORK_NAME)..."
	podman network create $(NETWORK_NAME) || true

database: network
	@echo "ℹ️ Starting MariaDB container..."
	podman run -d --name $(DB_CONTAINER) \
		--network $(NETWORK_NAME) \
		-e MYSQL_ROOT_PASSWORD=$(DB_PASSWORD) \
		docker.io/library/mariadb:latest

	@echo "ℹ️ Waiting for MariaDB to be ready..."
	sleep 5

	@echo "ℹ️ Importing database dumps using podman..."
	# Wait for MariaDB to be fully initialized
	podman exec -i $(DB_CONTAINER) bash -c 'until mariadb -u root -p$(DB_PASSWORD) -e "SELECT 1"; do sleep 1; echo "Waiting for MariaDB to be ready..."; done'
	
	# Create databases
	podman exec -i $(DB_CONTAINER) mariadb -u root -p"$(DB_PASSWORD)" -e "CREATE DATABASE IF NOT EXISTS nudiff;"
	podman exec -i $(DB_CONTAINER) mariadb -u root -p"$(DB_PASSWORD)" -e "CREATE DATABASE IF NOT EXISTS nudump;"
	
	# Copy and import SQL files
	podman cp migrations/nudiff.sql $(DB_CONTAINER):/tmp/nudiff.sql
	podman cp migrations/nudump.sql $(DB_CONTAINER):/tmp/nudump.sql
	podman exec $(DB_CONTAINER) bash -c "mariadb -u root -p'$(DB_PASSWORD)' nudiff < /tmp/nudiff.sql"
	podman exec $(DB_CONTAINER) bash -c "mariadb -u root -p'$(DB_PASSWORD)' nudump < /tmp/nudump.sql"
	
	@echo "✅ Database dumps imported successfully"

minio-deploy: database
	@echo "ℹ️ Cleaning up any existing MinIO container..."
	podman stop $(MINIO_CONTAINER) 2>/dev/null || true
	podman rm $(MINIO_CONTAINER) 2>/dev/null || true

	@echo "ℹ️ Starting MinIO container without persistent storage..."
	podman run -dt \
		--name $(MINIO_CONTAINER) \
		--network $(NETWORK_NAME) \
		-p 9000:9000 \
		-e "MINIO_ACCESS_KEY=$(MINIO_ACCESS_KEY)" \
		-e "MINIO_SECRET_KEY=$(MINIO_SECRET_KEY)" \
		docker.io/minio/minio server /data

	@echo "ℹ️ Waiting for MinIO to initialize..."
	sleep 10

	@echo "ℹ️ Verifying MinIO server status..."
	podman logs $(MINIO_CONTAINER)

	@echo "ℹ️ Creating bucket using MinIO Client..."
	podman run --rm --entrypoint /bin/sh \
		--network $(NETWORK_NAME) \
		docker.io/minio/mc -c " \
		mc alias set myminio http://$(MINIO_CONTAINER):9000 $(MINIO_ACCESS_KEY) $(MINIO_SECRET_KEY) && \
		mc mb myminio/$(MINIO_BUCKET) && \
		mc policy set public myminio/$(MINIO_BUCKET) \
		"
	
	@echo "ℹ️ Creating temporary volume for backup data..."
	podman volume create $(TEMP_VOLUME) || true

test: build minio-deploy
	@echo "ℹ️ Running backup test..."
	podman run --rm \
		--network $(NETWORK_NAME) \
		-v $(TEMP_VOLUME):/tmp \
		-e AWS_ACCESS_KEY_ID='$(MINIO_ACCESS_KEY)' \
		-e AWS_SECRET_ACCESS_KEY='$(MINIO_SECRET_KEY)' \
		-e S3_ENDPOINT='http://$(MINIO_CONTAINER):9000' \
		-e S3_BUCKET='$(MINIO_BUCKET)' \
		-e DB_HOST='$(DB_CONTAINER)' \
		-e DB_PORT='3306' \
		-e DB_USER='root' \
		-e DB_PASSWORD='$(DB_PASSWORD)' \
		-e DB_ALL_DATABASES='1' \
		-e DB_DUMP_PATH='/tmp' \
		-e DB_DUMP_FILE_KEEP_DAYS='7' \
		localhost/$(IMAGE_NAME):$(IMAGE_TAG)

	@echo "ℹ️ Verifying backup files in MinIO..."
	podman run --rm --entrypoint /bin/sh \
		--network $(NETWORK_NAME) \
		docker.io/minio/mc -c " \
		mc alias set myminio http://$(MINIO_CONTAINER):9000 $(MINIO_ACCESS_KEY) $(MINIO_SECRET_KEY) && \
		mc ls myminio/$(MINIO_BUCKET) \
		"
	
	@if podman run --rm --entrypoint /bin/sh \
		--network $(NETWORK_NAME) \
		docker.io/minio/mc -c " \
		mc alias set myminio http://$(MINIO_CONTAINER):9000 $(MINIO_ACCESS_KEY) $(MINIO_SECRET_KEY) && \
		mc ls myminio/$(MINIO_BUCKET) \
		" | grep -q ".sql.gz"; then \
		echo "✅ Integration test passed: Found backup(s) in MinIO bucket"; \
	else \
		echo "❌ Integration test failed: No backups found in MinIO bucket"; \
		exit 1; \
	fi

clean:
	@echo "ℹ️ Cleaning up any existing containers..."
	podman stop $(MINIO_CONTAINER) 2>/dev/null || true
	podman rm $(MINIO_CONTAINER) 2>/dev/null || true
	podman stop $(DB_CONTAINER) 2>/dev/null || true
	podman rm $(DB_CONTAINER) 2>/dev/null || true
