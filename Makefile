IMAGE_NAME = s3dbdump
IMAGE_TAG = latest
GARAGE_IMAGE = docker.io/dxflrs/garage:v2.4.1
GARAGE_ACCESS_KEY = GK3515373e4c851ebaad366558
GARAGE_SECRET_KEY = 7d37d093435a41f2aab8f13c19ba067d9776c90215f56614adad6ece597dbb34
GARAGE_BUCKET = dbdumps
GARAGE_REGION = us-east-2
NETWORK_NAME = s3dbdump
GARAGE_CONTAINER = s3dbdump-garage
DATABASE_CONTAINER = mariadb
TEMP_VOLUME = s3dbdump
DB_CONTAINER = test-mariadb
DB_PASSWORD = password
.PHONY: build test clean test-deps network garage-deploy database

test-deps:
	@which podman >/dev/null 2>&1 || (echo "❌ podman is required but not installed. Aborting." && exit 1)
	@curl --help all 2>/dev/null | grep -q -- --aws-sigv4 || (echo "❌ curl with --aws-sigv4 is required. Aborting." && exit 1)

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

garage-deploy: database
	@echo "ℹ️ Cleaning up any existing Garage container..."
	podman stop $(GARAGE_CONTAINER) 2>/dev/null || true
	podman rm $(GARAGE_CONTAINER) 2>/dev/null || true

	@echo "ℹ️ Starting Garage container without persistent storage..."
	podman run -dt \
		--name $(GARAGE_CONTAINER) \
		--network $(NETWORK_NAME) \
		-p 3900:3900 \
		-v "$(CURDIR)/scripts/garage.toml:/etc/garage.toml:ro" \
		-e GARAGE_DEFAULT_ACCESS_KEY="$(GARAGE_ACCESS_KEY)" \
		-e GARAGE_DEFAULT_SECRET_KEY="$(GARAGE_SECRET_KEY)" \
		-e GARAGE_DEFAULT_BUCKET="$(GARAGE_BUCKET)" \
		$(GARAGE_IMAGE) \
		/garage server --single-node --default-bucket

	@echo "ℹ️ Waiting for Garage to create the bucket..."
	@i=0; until podman exec $(GARAGE_CONTAINER) /garage bucket list 2>/dev/null | grep -q '$(GARAGE_BUCKET)'; do \
		i=$$((i+1)); \
		if [ $$i -gt 30 ]; then \
			echo "❌ Garage did not become ready"; \
			podman logs $(GARAGE_CONTAINER) || true; \
			exit 1; \
		fi; \
		sleep 2; \
	done
	podman logs $(GARAGE_CONTAINER)

	@echo "ℹ️ Creating temporary volume for backup data..."
	podman volume create $(TEMP_VOLUME) || true

test: build garage-deploy
	@echo "ℹ️ Running backup test..."
	podman run --rm \
		--network $(NETWORK_NAME) \
		-v $(TEMP_VOLUME):/tmp \
		-e AWS_ACCESS_KEY_ID='$(GARAGE_ACCESS_KEY)' \
		-e AWS_SECRET_ACCESS_KEY='$(GARAGE_SECRET_KEY)' \
		-e S3_ENDPOINT='http://$(GARAGE_CONTAINER):3900' \
		-e S3_BUCKET='$(GARAGE_BUCKET)' \
		-e DB_HOST='$(DB_CONTAINER)' \
		-e DB_PORT='3306' \
		-e DB_USER='root' \
		-e DB_PASSWORD='$(DB_PASSWORD)' \
		-e DB_ALL_DATABASES='1' \
		-e DB_DUMP_PATH='/tmp' \
		-e DB_DUMP_FILE_KEEP_DAYS='7' \
		localhost/$(IMAGE_NAME):$(IMAGE_TAG)

	@echo "ℹ️ Verifying backup files in Garage..."
	@listing=$$(curl -sS --aws-sigv4 "aws:amz:$(GARAGE_REGION):s3" \
		--user "$(GARAGE_ACCESS_KEY):$(GARAGE_SECRET_KEY)" \
		"http://127.0.0.1:3900/$(GARAGE_BUCKET)?list-type=2"); \
	printf '%s\n' "$$listing"; \
	if printf '%s\n' "$$listing" | grep -q ".sql.gz"; then \
		echo "✅ Integration test passed: Found backup(s) in Garage bucket"; \
	else \
		echo "❌ Integration test failed: No backups found in Garage bucket"; \
		exit 1; \
	fi

clean:
	@echo "ℹ️ Cleaning up any existing containers..."
	podman stop $(GARAGE_CONTAINER) s3dbdump 2>/dev/null || true
	podman rm $(GARAGE_CONTAINER) s3dbdump 2>/dev/null || true
	podman stop $(DB_CONTAINER) 2>/dev/null || true
	podman rm $(DB_CONTAINER) 2>/dev/null || true
