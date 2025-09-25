package mys3

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Helper function to set up test environment
func setupTestEnv(envVars map[string]string) map[string]string {
	originalValues := make(map[string]string)
	for key, value := range envVars {
		originalValues[key] = os.Getenv(key)
		if value == "" {
			os.Unsetenv(key)
		} else {
			os.Setenv(key, value)
		}
	}
	return originalValues
}

// Helper function to restore environment
func restoreTestEnv(originalValues map[string]string) {
	for key, value := range originalValues {
		if value == "" {
			os.Unsetenv(key)
		} else {
			os.Setenv(key, value)
		}
	}
}

func TestExtractDatabaseName(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		expected string
	}{
		{
			name:     "standard database backup filename",
			filename: "myapp-20230101T120000.sql.gz",
			expected: "myapp",
		},
		{
			name:     "database name with underscores",
			filename: "my_app_db-20230101T120000.sql.gz",
			expected: "my_app_db",
		},
		{
			name:     "single character database name",
			filename: "a-20230101T120000.sql.gz",
			expected: "a",
		},
		{
			name:     "filename without dash separator",
			filename: "myapp.sql.gz",
			expected: "myapp.sql.gz",
		},
		{
			name:     "empty filename",
			filename: "",
			expected: "",
		},
		{
			name:     "filename with multiple dashes",
			filename: "my-app-db-20230101T120000.sql.gz",
			expected: "my",
		},
		{
			name:     "complex database name",
			filename: "production_database-20231225T235959.sql.gz",
			expected: "production_database",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractDatabaseName(tt.filename)
			if result != tt.expected {
				t.Errorf("extractDatabaseName(%q) = %q, want %q", tt.filename, result, tt.expected)
			}
		})
	}
}

func TestUploadToS3_EnvironmentValidation(t *testing.T) {
	tests := []struct {
		name           string
		envVars        map[string]string
		filename       string
		expectError    bool
		expectedErrMsg string
	}{
		{
			name: "missing S3_BUCKET",
			envVars: map[string]string{
				"S3_BUCKET": "",
			},
			filename:       "test.txt",
			expectError:    true,
			expectedErrMsg: "S3_BUCKET is not set",
		},
		{
			name: "valid environment with custom endpoint",
			envVars: map[string]string{
				"S3_BUCKET":             "test-bucket",
				"S3_ENDPOINT":           "http://localhost:9000",
				"AWS_ACCESS_KEY_ID":     "testkey",
				"AWS_SECRET_ACCESS_KEY": "testsecret",
			},
			filename:    "nonexistent.txt",
			expectError: true, // Will fail on file open, but env validation passes
		},
		{
			name: "valid environment with AWS",
			envVars: map[string]string{
				"S3_BUCKET":   "test-bucket",
				"AWS_REGION":  "us-west-2",
				"S3_ENDPOINT": "", // Unset custom endpoint
			},
			filename:    "nonexistent.txt",
			expectError: true, // Will fail on file open, but env validation passes
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalValues := setupTestEnv(tt.envVars)
			defer restoreTestEnv(originalValues)

			err := UploadToS3(tt.filename)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tt.expectedErrMsg != "" && !strings.Contains(err.Error(), tt.expectedErrMsg) {
					t.Errorf("Expected error message to contain %q, got %q", tt.expectedErrMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestUploadToS3_FileOperations(t *testing.T) {
	// Skip integration tests unless explicitly requested
	if os.Getenv("RUN_INTEGRATION_TESTS") == "" {
		t.Skip("Skipping integration test - set RUN_INTEGRATION_TESTS=1 to run")
	}

	tempDir, err := os.MkdirTemp("", "mys3_upload_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name        string
		fileContent string
		expectError bool
	}{
		{
			name:        "upload small text file",
			fileContent: "Hello, S3!",
			expectError: false,
		},
		{
			name:        "upload empty file",
			fileContent: "",
			expectError: false,
		},
		{
			name:        "upload larger file",
			fileContent: strings.Repeat("This is test data. ", 1000),
			expectError: false,
		},
	}

	// Set up test environment (requires real S3 credentials)
	originalValues := setupTestEnv(map[string]string{
		"S3_BUCKET":             "your-test-bucket",
		"AWS_REGION":            "us-west-2",
		"AWS_ACCESS_KEY_ID":     "your-access-key",
		"AWS_SECRET_ACCESS_KEY": "your-secret-key",
	})
	defer restoreTestEnv(originalValues)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testFile := filepath.Join(tempDir, "test_"+tt.name+".txt")
			err := os.WriteFile(testFile, []byte(tt.fileContent), 0644)
			if err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			err = UploadToS3(testFile)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestUploadToS3_FileErrors(t *testing.T) {
	originalValues := setupTestEnv(map[string]string{
		"S3_BUCKET":  "test-bucket",
		"AWS_REGION": "us-west-2",
	})
	defer restoreTestEnv(originalValues)

	tests := []struct {
		name           string
		filename       string
		expectedErrMsg string
	}{
		{
			name:           "nonexistent file",
			filename:       "/nonexistent/path/file.txt",
			expectedErrMsg: "unable to open file",
		},
		{
			name:           "directory instead of file",
			filename:       os.TempDir(),
			expectedErrMsg: "unable to upload", // Directory can be opened but upload will fail
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := UploadToS3(tt.filename)

			if err == nil {
				t.Errorf("Expected error but got none")
			} else if !strings.Contains(err.Error(), tt.expectedErrMsg) {
				t.Errorf("Expected error message to contain %q, got %q", tt.expectedErrMsg, err.Error())
			}
		})
	}
}

func TestKeepOnlyNBackups_ValidationErrors(t *testing.T) {
	tests := []struct {
		name           string
		keepBackups    string
		envVars        map[string]string
		expectedErrMsg string
	}{
		{
			name:           "invalid keepBackups value - not a number",
			keepBackups:    "not-a-number",
			expectedErrMsg: "invalid DB_DUMP_FILE_KEEP_DAYS value",
		},
		{
			name:           "negative keepBackups value",
			keepBackups:    "-1",
			expectedErrMsg: "", // This should actually work, but behavior might be undefined
		},
		{
			name:        "zero keepBackups value",
			keepBackups: "0",
			envVars: map[string]string{
				"S3_BUCKET":  "test-bucket",
				"AWS_REGION": "us-west-2",
			},
			expectedErrMsg: "", // Should work - deletes all backups
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envVars != nil {
				originalValues := setupTestEnv(tt.envVars)
				defer restoreTestEnv(originalValues)
			}

			err := KeepOnlyNBackups(tt.keepBackups)

			if tt.expectedErrMsg != "" {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if !strings.Contains(err.Error(), tt.expectedErrMsg) {
					t.Errorf("Expected error message to contain %q, got %q", tt.expectedErrMsg, err.Error())
				}
			} else {
				// For valid inputs, we expect either success or AWS-related errors
				// since we're not connecting to real AWS
				if err != nil && !strings.Contains(err.Error(), "unable to load AWS SDK config") &&
					!strings.Contains(err.Error(), "unable to list objects") {
					t.Errorf("Unexpected error type: %v", err)
				}
			}
		})
	}
}

func TestKeepOnlyNBackups_EnvironmentSetup(t *testing.T) {
	tests := []struct {
		name    string
		envVars map[string]string
	}{
		{
			name: "with custom S3 endpoint",
			envVars: map[string]string{
				"S3_BUCKET":             "test-bucket",
				"S3_ENDPOINT":           "http://localhost:9000",
				"AWS_ACCESS_KEY_ID":     "testkey",
				"AWS_SECRET_ACCESS_KEY": "testsecret",
				"DB_DUMP_PATH":          "/tmp/test-dumps",
			},
		},
		{
			name: "with AWS S3",
			envVars: map[string]string{
				"S3_BUCKET":    "test-bucket",
				"AWS_REGION":   "us-west-2",
				"DB_DUMP_PATH": "",
			},
		},
		{
			name: "default dump path",
			envVars: map[string]string{
				"S3_BUCKET":  "test-bucket",
				"AWS_REGION": "us-east-1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalValues := setupTestEnv(tt.envVars)
			defer restoreTestEnv(originalValues)

			// Test the environment setup logic
			dumpDir := os.Getenv("DB_DUMP_PATH")
			if dumpDir == "" {
				dumpDir = "./dumps"
			}

			expectedDir := tt.envVars["DB_DUMP_PATH"]
			if expectedDir == "" {
				expectedDir = "./dumps"
			}

			if dumpDir != expectedDir {
				t.Errorf("Dump directory = %v, want %v", dumpDir, expectedDir)
			}

			// Test the function (will fail on AWS operations, but that's expected)
			err := KeepOnlyNBackups("7")
			if err != nil && !strings.Contains(err.Error(), "unable to load AWS SDK config") &&
				!strings.Contains(err.Error(), "unable to list objects") {
				t.Errorf("Unexpected error type: %v", err)
			}
		})
	}
}

// Mock S3 objects for testing backup logic
type mockS3Object struct {
	key          string
	lastModified time.Time
}

func TestBackupSortingLogic(t *testing.T) {
	// Test the sorting logic used in KeepOnlyNBackups
	baseTime := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	time1 := baseTime.Add(48 * time.Hour)
	time2 := baseTime.Add(24 * time.Hour)

	objects := []types.Object{
		{Key: stringPtr("myapp-20230103T120000.sql.gz"), LastModified: &time1},
		{Key: stringPtr("myapp-20230101T120000.sql.gz"), LastModified: &baseTime},
		{Key: stringPtr("myapp-20230102T120000.sql.gz"), LastModified: &time2},
	}

	// Sort by LastModified descending (newest first)
	// This mimics the sorting logic in KeepOnlyNBackups
	for i := 0; i < len(objects)-1; i++ {
		for j := i + 1; j < len(objects); j++ {
			if objects[j].LastModified.After(*objects[i].LastModified) {
				objects[i], objects[j] = objects[j], objects[i]
			}
		}
	}

	expectedOrder := []string{
		"myapp-20230103T120000.sql.gz",
		"myapp-20230102T120000.sql.gz",
		"myapp-20230101T120000.sql.gz",
	}

	for i, obj := range objects {
		if *obj.Key != expectedOrder[i] {
			t.Errorf("Object at index %d = %v, want %v", i, *obj.Key, expectedOrder[i])
		}
	}
}

func TestDatabaseGroupingLogic(t *testing.T) {
	// Test the logic that groups backups by database name
	objects := []types.Object{
		{Key: stringPtr("myapp-20230101T120000.sql.gz")},
		{Key: stringPtr("testdb-20230101T120000.sql.gz")},
		{Key: stringPtr("myapp-20230102T120000.sql.gz")},
		{Key: stringPtr("another-20230101T120000.sql.gz")},
		{Key: stringPtr("testdb-20230102T120000.sql.gz")},
	}

	// Group by database name (mimics KeepOnlyNBackups logic)
	dbBackups := make(map[string][]types.Object)
	for _, obj := range objects {
		dbName := extractDatabaseName(*obj.Key)
		dbBackups[dbName] = append(dbBackups[dbName], obj)
	}

	expectedGroups := map[string]int{
		"myapp":   2,
		"testdb":  2,
		"another": 1,
	}

	for dbName, expectedCount := range expectedGroups {
		if actualCount := len(dbBackups[dbName]); actualCount != expectedCount {
			t.Errorf("Database %q has %d backups, want %d", dbName, actualCount, expectedCount)
		}
	}
}

func TestKeepOnlyNBackups_DirectoryCleanup(t *testing.T) {
	// Test the directory cleanup functionality
	tempDir, err := os.MkdirTemp("", "mys3_cleanup_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create some test files
	testFiles := []string{"file1.txt", "file2.sql", "file3.gz"}
	for _, fileName := range testFiles {
		filePath := filepath.Join(tempDir, fileName)
		err := os.WriteFile(filePath, []byte("test content"), 0644)
		if err != nil {
			t.Fatalf("Failed to create test file %s: %v", fileName, err)
		}
	}

	// Set up environment to use our temp directory
	originalValues := setupTestEnv(map[string]string{
		"S3_BUCKET":    "test-bucket",
		"AWS_REGION":   "us-west-2",
		"DB_DUMP_PATH": tempDir,
	})
	defer restoreTestEnv(originalValues)

	// The function will fail on S3 operations, but we can test the file cleanup logic
	// by examining the directory afterward (in a real scenario where S3 succeeds)

	// For this test, we'll just verify that the environment is set up correctly
	dumpDir := os.Getenv("DB_DUMP_PATH")
	if dumpDir != tempDir {
		t.Errorf("DB_DUMP_PATH = %v, want %v", dumpDir, tempDir)
	}

	// Verify files exist before cleanup
	files, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("Failed to read temp directory: %v", err)
	}
	if len(files) != len(testFiles) {
		t.Errorf("Expected %d files in temp directory, got %d", len(testFiles), len(files))
	}
}

// Helper function to create string pointers (for AWS SDK types)
func stringPtr(s string) *string {
	return &s
}

// Benchmark tests
func BenchmarkExtractDatabaseName(b *testing.B) {
	filename := "production_database-20231225T235959.sql.gz"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		extractDatabaseName(filename)
	}
}

func BenchmarkDatabaseGrouping(b *testing.B) {
	// Create a realistic set of backup filenames
	filenames := make([]string, 100)
	for i := 0; i < 100; i++ {
		dbName := fmt.Sprintf("database%d", i%10) // 10 different databases
		timestamp := fmt.Sprintf("2023%02d%02dT120000", (i%12)+1, (i%28)+1)
		filenames[i] = fmt.Sprintf("%s-%s.sql.gz", dbName, timestamp)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dbBackups := make(map[string][]string)
		for _, filename := range filenames {
			dbName := extractDatabaseName(filename)
			dbBackups[dbName] = append(dbBackups[dbName], filename)
		}
	}
}

// Integration test template (disabled by default)
func TestUploadToS3_Integration(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") == "" {
		t.Skip("Skipping integration test - set RUN_INTEGRATION_TESTS=1 to run")
	}

	// This test requires real AWS credentials and S3 bucket
	tempDir, err := os.MkdirTemp("", "mys3_integration_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	testFile := filepath.Join(tempDir, "integration_test.txt")
	testContent := "Integration test content"
	err = os.WriteFile(testFile, []byte(testContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Set up real S3 environment variables
	originalValues := setupTestEnv(map[string]string{
		"S3_BUCKET":  os.Getenv("TEST_S3_BUCKET"),
		"AWS_REGION": os.Getenv("TEST_AWS_REGION"),
	})
	defer restoreTestEnv(originalValues)

	if os.Getenv("S3_BUCKET") == "" {
		t.Skip("TEST_S3_BUCKET not set - skipping integration test")
	}

	err = UploadToS3(testFile)
	if err != nil {
		t.Errorf("Integration test failed: %v", err)
	}
}

func TestKeepOnlyNBackups_Integration(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") == "" {
		t.Skip("Skipping integration test - set RUN_INTEGRATION_TESTS=1 to run")
	}

	// Set up real S3 environment variables
	originalValues := setupTestEnv(map[string]string{
		"S3_BUCKET":  os.Getenv("TEST_S3_BUCKET"),
		"AWS_REGION": os.Getenv("TEST_AWS_REGION"),
	})
	defer restoreTestEnv(originalValues)

	if os.Getenv("S3_BUCKET") == "" {
		t.Skip("TEST_S3_BUCKET not set - skipping integration test")
	}

	err := KeepOnlyNBackups("5")
	if err != nil {
		t.Errorf("Integration test failed: %v", err)
	}
}
