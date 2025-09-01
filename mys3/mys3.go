package mys3

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func UploadToS3(filename string) error {

	if os.Getenv("S3_BUCKET") == "" {
		return fmt.Errorf("S3_BUCKET is not set")
	}

	var cfg aws.Config
	var err error

	if endpoint := os.Getenv("S3_ENDPOINT"); endpoint != "" {
		cfg, err = config.LoadDefaultConfig(context.TODO(),
			config.WithRegion("us-east-1"),
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
				os.Getenv("AWS_ACCESS_KEY_ID"),
				os.Getenv("AWS_SECRET_ACCESS_KEY"),
				"",
			)),
			config.WithEndpointResolver(aws.EndpointResolverFunc(
				func(service, region string) (aws.Endpoint, error) {
					return aws.Endpoint{
						PartitionID:       "aws",
						URL:               endpoint,
						SigningRegion:     "us-east-2",
						HostnameImmutable: true,
					}, nil
				},
			)),
		)
	} else {
		cfg, err = config.LoadDefaultConfig(context.TODO(),
			config.WithRegion(os.Getenv("AWS_REGION")),
		)
	}

	if err != nil {
		return fmt.Errorf("unable to load AWS SDK config: %w", err)
	}

	s3Client := s3.NewFromConfig(cfg)

	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("unable to open file %q: %w", filename, err)
	}
	defer file.Close()

	fileInfo, _ := file.Stat()
	var size = fileInfo.Size()
	buffer := make([]byte, size)
	file.Read(buffer)

	_, err = s3Client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket: aws.String(os.Getenv("S3_BUCKET")),
		Key:    aws.String(filepath.Base(filename)),
		Body:   bytes.NewReader(buffer),
		ACL:    types.ObjectCannedACLPrivate,
	})
	if err != nil {
		return fmt.Errorf("unable to upload %q to %q: %w", filename, os.Getenv("S3_BUCKET"), err)
	}

	log.Println("Successfully uploaded", filename, "to", os.Getenv("S3_BUCKET"))

	return nil
}

func KeepOnlyNBackups(keepBackups string) error {
	keepBackupsInt, keepBackupsErr := strconv.Atoi(keepBackups)
	if keepBackupsErr != nil {
		return fmt.Errorf("invalid DB_DUMP_FILE_KEEP_DAYS value: %w", keepBackupsErr)
	}

	var cfg aws.Config
	var err error

	if endpoint := os.Getenv("S3_ENDPOINT"); endpoint != "" {
		cfg, err = config.LoadDefaultConfig(context.TODO(),
			config.WithRegion("us-east-1"),
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
				os.Getenv("AWS_ACCESS_KEY_ID"),
				os.Getenv("AWS_SECRET_ACCESS_KEY"),
				"",
			)),
			config.WithEndpointResolver(aws.EndpointResolverFunc(
				func(service, region string) (aws.Endpoint, error) {
					return aws.Endpoint{
						PartitionID:       "aws",
						URL:               endpoint,
						SigningRegion:     "us-east-2",
						HostnameImmutable: true,
					}, nil
				},
			)),
		)
	} else {
		cfg, err = config.LoadDefaultConfig(context.TODO(),
			config.WithRegion(os.Getenv("AWS_REGION")),
		)
	}

	if err != nil {
		return fmt.Errorf("unable to load AWS SDK config: %w", err)
	}

	s3Client := s3.NewFromConfig(cfg)

	resp, err := s3Client.ListObjectsV2(context.TODO(), &s3.ListObjectsV2Input{
		Bucket: aws.String(os.Getenv("S3_BUCKET")),
	})
	if err != nil {
		return fmt.Errorf("unable to list objects in bucket %q: %w", os.Getenv("S3_BUCKET"), err)
	}

	dbBackups := make(map[string][]types.Object)
	for _, obj := range resp.Contents {
		dbName := extractDatabaseName(*obj.Key)
		dbBackups[dbName] = append(dbBackups[dbName], obj)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var deleteErrors []error

	for dbName, backups := range dbBackups {
		sort.Slice(backups, func(i, j int) bool {
			return backups[i].LastModified.After(*backups[j].LastModified)
		})

		if len(backups) > keepBackupsInt {
			objectsToDelete := backups[keepBackupsInt:]

			// Use the new WaitGroup.Go method for cleaner goroutine management
			wg.Go(func() {
				for _, obj := range objectsToDelete {
					_, err := s3Client.DeleteObject(context.TODO(), &s3.DeleteObjectInput{
						Bucket: aws.String(os.Getenv("S3_BUCKET")),
						Key:    obj.Key,
					})
					if err != nil {
						mu.Lock()
						deleteErrors = append(deleteErrors, fmt.Errorf("unable to delete object %q: %w", *obj.Key, err))
						mu.Unlock()
					} else {
						log.Printf("Deleted old backup for database %s: %s", dbName, *obj.Key)
					}
				}
			})
		}
	}

	// Wait for all deletion operations to complete
	wg.Wait()

	// Return the first error if any occurred
	if len(deleteErrors) > 0 {
		return deleteErrors[0]
	}

	dumpDir := os.Getenv("DB_DUMP_PATH")
	if dumpDir == "" {
		dumpDir = "./dumps"
	}

	files, err := os.ReadDir(dumpDir)
	if err != nil {
		return fmt.Errorf("unable to list files in directory %q: %w", dumpDir, err)
	}

	for _, file := range files {
		filePath := filepath.Join(dumpDir, file.Name())
		err := os.Remove(filePath)
		if err != nil {
			return fmt.Errorf("unable to delete file %q: %w", filePath, err)
		}
	}

	return nil
}

func extractDatabaseName(filename string) string {
	parts := strings.Split(filename, "-")
	if len(parts) > 0 {
		return parts[0]
	}
	return filename
}
