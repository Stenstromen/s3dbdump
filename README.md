# s3dbdump

![s3dbdump](s3dbdump.webp)

A tool to dump a MariaDB (MySQL) database to a file and upload it to S3 or MinIO, with gzip compression.

## Table of Contents

- [s3dbdump](#s3dbdump)
  - [Table of Contents](#table-of-contents)
  - [Usage](#usage)
    - [Run dump all databases to MinIO bucket using Podman](#run-dump-all-databases-to-minio-bucket-using-podman)
    - [Example Kubernetes Cronjob](#example-kubernetes-cronjob)
    - [Environment variables](#environment-variables)

## Usage

### Run dump all databases to MinIO bucket using Podman

```bash
mkdir -p dumps # Important since running as non-root

podman run --rm \
-v $(pwd)/dumps:/tmp/dumps:rw \
-e AWS_ACCESS_KEY_ID='<access-key-id>' \
-e AWS_SECRET_ACCESS_KEY='<secret-access-key>' \
-e S3_ENDPOINT='https://minio.example.com' \
-e S3_BUCKET='dbdumps' \
-e DB_HOST='localhost' \
-e DB_PORT='3306' \
-e DB_USER='root' \
-e DB_PASSWORD='password' \
-e DB_ALL_DATABASES='1' \
-e DB_DUMP_PATH='/tmp/dumps' \
-e DB_DUMP_FILE_KEEP_DAYS='7' \
ghcr.io/stenstromen/s3dbdump:latest
```

### Example Kubernetes Cronjob

```yaml
apiVersion: v1
data:
  db-password: QUtJQTVYUVcyUFFFRUs1RktZRlM=
  minio-access-key-id: QUtJQTVYUVcyUFFFRUs1RktZRlM=
  minio-secret-access-key: czNjcjN0
kind: Secret
metadata:
  name: db-dump-secrets
  namespace: default
type: Opaque

---

apiVersion: batch/v1
kind: CronJob
metadata:
  name: mariadb-backup-s3
  namespace: default
spec:
  schedule: "0 6 * * *"
  successfulJobsHistoryLimit: 0
  concurrencyPolicy: Replace
  failedJobsHistoryLimit: 1
  jobTemplate:
    spec:
      activeDeadlineSeconds: 3600
      backoffLimit: 2
      template:
        spec:
          containers:
            - env:
                - name: DB_HOST
                  value: database.default.svc.cluster.local
                - name: DB_USER
                  value: root
                - name: DB_PASSWORD
                  valueFrom:
                    secretKeyRef:
                      name: db-dump-secrets
                      key: db-password
                - name: DB_ALL_DATABASES
                  value: "1"
                - name: DB_DUMP_FILE_KEEP_DAYS
                  value: "7"
                - name: DB_DUMP_PATH
                  value: /tmp
                - name: S3_BUCKET
                  value: dbbak
                - name: S3_ENDPOINT
                  value: http://minio.default.svc.cluster.local:9000
                - name: AWS_ACCESS_KEY_ID
                  valueFrom:
                    secretKeyRef:
                      name: db-dump-secrets
                      key: minio-access-key-id
                - name: AWS_SECRET_ACCESS_KEY
                  valueFrom:
                    secretKeyRef:
                      name: db-dump-secrets
                      key: minio-secret-access-key
              securityContext:
                runAsUser: 65534
                runAsGroup: 65534
                privileged: false
                runAsNonRoot: true
                readOnlyRootFilesystem: true
                allowPrivilegeEscalation: false
                procMount: Default
                capabilities:
                  drop: ["ALL"]
                seccompProfile:
                  type: RuntimeDefault
              image: ghcr.io/stenstromen/s3dbdump:latest
              imagePullPolicy: IfNotPresent
              name: backup
              terminationMessagePath: /dev/termination-log
              terminationMessagePolicy: File
              volumeMounts:
                - name: tmp
                  mountPath: /tmp
          dnsPolicy: ClusterFirst
          restartPolicy: Never
          schedulerName: default-scheduler
          terminationGracePeriodSeconds: 30
          volumes:
            - name: tmp
              emptyDir: {}

```

### Environment variables

| Environment Variable     | Required | Default Value             | Description                         |
| ------------------------ | -------- | ------------------------- | ----------------------------------- |
| `AWS_ACCESS_KEY_ID`      | Yes      | -                         | AWS access key ID                   |
| `AWS_SECRET_ACCESS_KEY`  | Yes      | -                         | AWS secret access key               |
| `AWS_REGION`             | Yes      | `us-east-2` for custom endpoints | SigV4 region. With `S3_ENDPOINT`, this must match the server region (Garage’s default is `garage`) |
| `S3_BUCKET`              | Yes      | -                         | S3 bucket name                      |
| `S3_ENDPOINT`            | No       | -                         | Custom S3 endpoint (e.g. for MinIO) |
| `DB_HOST`                | Yes      | -                         | Database host                       |
| `DB_PORT`                | No       | 3306                      | Database port                       |
| `DB_USER`                | Yes      | -                         | Database user                       |
| `DB_PASSWORD`            | Yes      | -                         | Database password                   |
| `DB_NAME`                | Yes      | -                         | Database name to dump               |
| `DB_ALL_DATABASES`       | No       | 0                         | Set to 1 to dump all databases      |
| `DB_GZIP`                | No       | 1                         | Enable gzip compression             |
| `DB_DUMP_PATH`           | No       | ./dumps                   | Directory to store dumps            |
| `DB_DUMP_FILENAME`       | No       | %s-20060102T150405.sql.gz | Dump filename format                |
| `DB_DUMP_FILE_KEEP_DAYS` | No       | 7                         | Number of days to keep backups      |
