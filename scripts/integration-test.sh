#!/usr/bin/env bash
# Integration test.
# MariaDB must already be listening on 127.0.0.1:3306 (user root, password testpass123).
# This script starts Garage on 127.0.0.1:3900.
set -euo pipefail

cd "$(dirname "$0")/.."

garage_image="docker.io/dxflrs/garage:v2.4.1"
garage_access_key="GK3515373e4c851ebaad366558"
garage_secret_key="7d37d093435a41f2aab8f13c19ba067d9776c90215f56614adad6ece597dbb34"
garage_bucket="dbdumps"
garage_region="us-east-2"
garage_container="s3dbdump-garage"

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

if ! curl --help all 2>/dev/null | grep -q -- --aws-sigv4; then
  echo "curl with --aws-sigv4 is required to list objects in Garage"
  exit 1
fi

echo "Starting Garage"
docker rm -f "$garage_container" >/dev/null 2>&1 || true
docker run -d --name "$garage_container" --network host \
  -v "$(pwd)/scripts/garage.toml:/etc/garage.toml:ro" \
  -e GARAGE_DEFAULT_ACCESS_KEY="$garage_access_key" \
  -e GARAGE_DEFAULT_SECRET_KEY="$garage_secret_key" \
  -e GARAGE_DEFAULT_BUCKET="$garage_bucket" \
  "$garage_image" \
  /garage server --single-node --default-bucket

echo "Waiting for Garage"
if ! timeout 60 bash -c "until docker exec '$garage_container' /garage bucket list 2>/dev/null | grep -q '$garage_bucket'; do sleep 2; done"; then
  echo "Garage did not become ready"
  docker logs "$garage_container" || true
  exit 1
fi
echo "Garage is ready"

docker build --load -t s3dbdump .
docker volume create s3dbdump-temp
docker run --rm -v s3dbdump-temp:/tmp alpine:latest chown -R 65534:65534 /tmp

docker run \
  --rm \
  --name s3dbdump \
  --network host \
  -v s3dbdump-temp:/tmp \
  -e AWS_ACCESS_KEY_ID="$garage_access_key" \
  -e AWS_SECRET_ACCESS_KEY="$garage_secret_key" \
  -e AWS_REGION="$garage_region" \
  -e S3_ENDPOINT='http://127.0.0.1:3900' \
  -e S3_BUCKET="$garage_bucket" \
  -e DB_HOST='127.0.0.1' \
  -e DB_PORT='3306' \
  -e DB_USER='root' \
  -e DB_PASSWORD='testpass123' \
  -e DB_ALL_DATABASES='1' \
  -e DB_DUMP_PATH='/tmp' \
  -e DB_DUMP_FILE_KEEP_DAYS='7' \
  s3dbdump

listing="$(curl -sS --aws-sigv4 "aws:amz:${garage_region}:s3" \
  --user "${garage_access_key}:${garage_secret_key}" \
  "http://127.0.0.1:3900/${garage_bucket}?list-type=2")"
printf '%s\n' "$listing"
if grep -q ".sql.gz" <<<"$listing"; then
  echo "Integration test passed: found a backup in the Garage bucket"
else
  echo "Integration test failed: no backups found in the Garage bucket"
  exit 1
fi
