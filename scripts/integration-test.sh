#!/usr/bin/env bash
# Integration test.
# MariaDB must already be listening on 127.0.0.1:3306 (user root, password testpass123).
# This script starts MinIO on 127.0.0.1:9000 (user minio, password minio123).
set -euo pipefail

cd "$(dirname "$0")/.."

echo "Installing MySQL client"
DEBIAN_FRONTEND=noninteractive sudo apt-get update -qq
DEBIAN_FRONTEND=noninteractive sudo apt-get install -y -qq mysql-client

mkdir -p "${HOME}/.mysql"
cat > "${HOME}/.mysql/my.cnf" <<'EOF'
[client]
host=127.0.0.1
port=3306
user=root
password=testpass123
EOF
chmod 600 "${HOME}/.mysql/my.cnf"

echo "Waiting for MariaDB"
timeout 60 bash -c 'until mysqladmin --defaults-extra-file="$HOME/.mysql/my.cnf" ping --silent; do sleep 2; done'
echo "MariaDB is ready"

mysql --defaults-extra-file="${HOME}/.mysql/my.cnf" test -e "CREATE DATABASE IF NOT EXISTS nudump;"
mysql --defaults-extra-file="${HOME}/.mysql/my.cnf" test -e "CREATE DATABASE IF NOT EXISTS nudiff;"
mysql --defaults-extra-file="${HOME}/.mysql/my.cnf" nudump < migrations/nudump.sql
mysql --defaults-extra-file="${HOME}/.mysql/my.cnf" nudiff < migrations/nudiff.sql

# dl.min.io returns HTTP 410. The client is published on GitHub releases.
mc_release="RELEASE.2025-08-13T08-35-41Z"
case "$(uname -m)" in
  x86_64) mc_arch="amd64" ;;
  aarch64 | arm64) mc_arch="arm64" ;;
  *)
    echo "Unsupported architecture: $(uname -m)"
    exit 1
    ;;
esac
mc_url="https://github.com/minio/mc/releases/download/${mc_release}/mc.linux-${mc_arch}.${mc_release}"
mkdir -p "${HOME}/minio-binaries"
curl -fL "$mc_url" -o "${HOME}/minio-binaries/mc"
chmod +x "${HOME}/minio-binaries/mc"
mc="${HOME}/minio-binaries/mc"

# quay.io/minio/minio:latest-cicd defaults to the bare `minio` command, which
# exits without opening a port. GitHub Actions service containers cannot pass
# `server /data`, so start the server here.
echo "Starting MinIO"
docker run -d --name s3dbdump-minio --network host \
  -e MINIO_ROOT_USER=minio \
  -e MINIO_ROOT_PASSWORD=minio123 \
  -e MINIO_ACCESS_KEY=minio \
  -e MINIO_SECRET_KEY=minio123 \
  quay.io/minio/minio:latest-cicd server /data

echo "Waiting for MinIO"
if ! timeout 60 bash -c 'until curl -sf http://127.0.0.1:9000/minio/health/live >/dev/null; do sleep 2; done'; then
  echo "MinIO did not become ready"
  docker logs s3dbdump-minio || true
  exit 1
fi
echo "MinIO is ready"

"$mc" alias set myminio http://127.0.0.1:9000 minio minio123
"$mc" mb myminio/dbdumps
if ! "$mc" policy set public myminio/dbdumps; then
  "$mc" anonymous set public myminio/dbdumps
fi

docker build --load -t s3dbdump .
docker volume create s3dbdump-temp
docker run --rm -v s3dbdump-temp:/tmp alpine:latest chown -R 65534:65534 /tmp

docker run \
  --rm \
  --name s3dbdump \
  --network host \
  -v s3dbdump-temp:/tmp \
  -e AWS_ACCESS_KEY_ID='minio' \
  -e AWS_SECRET_ACCESS_KEY='minio123' \
  -e S3_ENDPOINT='http://127.0.0.1:9000' \
  -e S3_BUCKET='dbdumps' \
  -e DB_HOST='127.0.0.1' \
  -e DB_PORT='3306' \
  -e DB_USER='root' \
  -e DB_PASSWORD='testpass123' \
  -e DB_ALL_DATABASES='1' \
  -e DB_DUMP_PATH='/tmp' \
  -e DB_DUMP_FILE_KEEP_DAYS='7' \
  s3dbdump

if "$mc" ls myminio/dbdumps | grep -q ".sql.gz"; then
  echo "Integration test passed: found a backup in the MinIO bucket"
else
  echo "Integration test failed: no backups found in the MinIO bucket"
  exit 1
fi
