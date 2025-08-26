module github.com/stenstromen/s3dbdump

replace github.com/stenstromen/s3dbdump => ./

go 1.24.0

require (
	github.com/aws/aws-sdk-go-v2/credentials v1.17.71
	github.com/go-sql-driver/mysql v1.9.3
)

require (
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.0 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.16.33 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.4 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.4 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.8.3 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.0 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.8.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.19.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.25.6 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.30.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.34.1 // indirect
	github.com/aws/smithy-go v1.22.5 // indirect
)

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/aws/aws-sdk-go-v2 v1.38.1
	github.com/aws/aws-sdk-go-v2/config v1.29.18
	github.com/aws/aws-sdk-go-v2/service/s3 v1.87.1
	github.com/jamf/go-mysqldump v0.8.1
)
