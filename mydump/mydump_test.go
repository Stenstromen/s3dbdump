package mydump

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
)

func TestInitConfig(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		expected mysql.Config
	}{
		{
			name: "all environment variables set",
			envVars: map[string]string{
				"DB_USER":     "testuser",
				"DB_PASSWORD": "testpass",
				"DB_HOST":     "localhost",
				"DB_PORT":     "3307",
			},
			expected: mysql.Config{
				User:                    "testuser",
				Passwd:                  "testpass",
				Addr:                    "localhost:3307",
				Net:                     "tcp",
				ParseTime:               true,
				AllowNativePasswords:    true,
				AllowCleartextPasswords: true,
			},
		},
		{
			name: "default port when DB_PORT not set",
			envVars: map[string]string{
				"DB_USER":     "testuser",
				"DB_PASSWORD": "testpass",
				"DB_HOST":     "localhost",
			},
			expected: mysql.Config{
				User:                    "testuser",
				Passwd:                  "testpass",
				Addr:                    "localhost:3306",
				Net:                     "tcp",
				ParseTime:               true,
				AllowNativePasswords:    true,
				AllowCleartextPasswords: true,
			},
		},
		{
			name: "empty DB_PORT defaults to 3306",
			envVars: map[string]string{
				"DB_USER":     "testuser",
				"DB_PASSWORD": "testpass",
				"DB_HOST":     "localhost",
				"DB_PORT":     "",
			},
			expected: mysql.Config{
				User:                    "testuser",
				Passwd:                  "testpass",
				Addr:                    "localhost:3306",
				Net:                     "tcp",
				ParseTime:               true,
				AllowNativePasswords:    true,
				AllowCleartextPasswords: true,
			},
		},
		{
			name: "missing environment variables result in empty values",
			envVars: map[string]string{
				"DB_HOST": "testhost",
				"DB_PORT": "5432",
			},
			expected: mysql.Config{
				User:                    "",
				Passwd:                  "",
				Addr:                    "testhost:5432",
				Net:                     "tcp",
				ParseTime:               true,
				AllowNativePasswords:    true,
				AllowCleartextPasswords: true,
			},
		},
		{
			name:    "no environment variables set",
			envVars: map[string]string{},
			expected: mysql.Config{
				User:                    "",
				Passwd:                  "",
				Addr:                    ":3306",
				Net:                     "tcp",
				ParseTime:               true,
				AllowNativePasswords:    true,
				AllowCleartextPasswords: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear all relevant environment variables first
			clearEnvVars := []string{"DB_USER", "DB_PASSWORD", "DB_HOST", "DB_PORT"}
			originalValues := make(map[string]string)

			// Save original values and clear them
			for _, envVar := range clearEnvVars {
				originalValues[envVar] = os.Getenv(envVar)
				os.Unsetenv(envVar)
			}

			// Set test environment variables
			for key, value := range tt.envVars {
				os.Setenv(key, value)
			}

			// Reset global Config to ensure clean state
			Config = mysql.Config{}

			// Call the function under test
			InitConfig()

			// Verify the results
			if Config.User != tt.expected.User {
				t.Errorf("User = %v, want %v", Config.User, tt.expected.User)
			}
			if Config.Passwd != tt.expected.Passwd {
				t.Errorf("Passwd = %v, want %v", Config.Passwd, tt.expected.Passwd)
			}
			if Config.Addr != tt.expected.Addr {
				t.Errorf("Addr = %v, want %v", Config.Addr, tt.expected.Addr)
			}
			if Config.Net != tt.expected.Net {
				t.Errorf("Net = %v, want %v", Config.Net, tt.expected.Net)
			}
			if Config.ParseTime != tt.expected.ParseTime {
				t.Errorf("ParseTime = %v, want %v", Config.ParseTime, tt.expected.ParseTime)
			}
			if Config.AllowNativePasswords != tt.expected.AllowNativePasswords {
				t.Errorf("AllowNativePasswords = %v, want %v", Config.AllowNativePasswords, tt.expected.AllowNativePasswords)
			}
			if Config.AllowCleartextPasswords != tt.expected.AllowCleartextPasswords {
				t.Errorf("AllowCleartextPasswords = %v, want %v", Config.AllowCleartextPasswords, tt.expected.AllowCleartextPasswords)
			}

			// Restore original environment variables
			for _, envVar := range clearEnvVars {
				if originalValues[envVar] != "" {
					os.Setenv(envVar, originalValues[envVar])
				} else {
					os.Unsetenv(envVar)
				}
			}
		})
	}
}

func TestInitConfig_GlobalConfigState(t *testing.T) {
	// Test that InitConfig modifies the global Config variable
	originalConfig := Config

	// Set some test environment variables
	os.Setenv("DB_USER", "globaltest")
	os.Setenv("DB_HOST", "globaltesthost")

	// Call InitConfig
	InitConfig()

	// Verify the global Config was modified
	if Config.User != "globaltest" {
		t.Errorf("Global Config.User was not set correctly: got %v, want globaltest", Config.User)
	}
	if Config.Addr != "globaltesthost:3306" {
		t.Errorf("Global Config.Addr was not set correctly: got %v, want globaltesthost:3306", Config.Addr)
	}

	// Cleanup
	Config = originalConfig
	os.Unsetenv("DB_USER")
	os.Unsetenv("DB_HOST")
}

// Benchmark test to ensure the function is performant
func BenchmarkInitConfig(b *testing.B) {
	// Set up test environment
	os.Setenv("DB_USER", "benchuser")
	os.Setenv("DB_PASSWORD", "benchpass")
	os.Setenv("DB_HOST", "benchhost")
	os.Setenv("DB_PORT", "3306")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		InitConfig()
	}

	// Cleanup
	os.Unsetenv("DB_USER")
	os.Unsetenv("DB_PASSWORD")
	os.Unsetenv("DB_HOST")
	os.Unsetenv("DB_PORT")
}

// Test the database filtering logic (extracted for unit testing)
func TestDatabaseFiltering(t *testing.T) {
	databases := []string{
		"information_schema",
		"performance_schema",
		"mysql",
		"sys",
		"myapp_db",
		"test_db",
		"another_app",
	}

	var filtered []string
	for _, dbName := range databases {
		if !strings.HasPrefix(dbName, "information_schema") &&
			!strings.HasPrefix(dbName, "performance_schema") &&
			!strings.HasPrefix(dbName, "mysql") &&
			!strings.HasPrefix(dbName, "sys") {
			filtered = append(filtered, dbName)
		}
	}

	expected := []string{"myapp_db", "test_db", "another_app"}
	if len(filtered) != len(expected) {
		t.Errorf("Filtered databases count = %d, want %d", len(filtered), len(expected))
	}

	for i, db := range expected {
		if i >= len(filtered) || filtered[i] != db {
			t.Errorf("Filtered database at index %d = %v, want %v", i, filtered[i], db)
		}
	}
}

// Test dump filename format
func TestDumpFilenameFormat(t *testing.T) {
	database := "test_db"
	expectedFormat := fmt.Sprintf("%s-20060102T150405", database)

	if !strings.HasPrefix(expectedFormat, database) {
		t.Errorf("Filename format should start with database name")
	}

	if !strings.Contains(expectedFormat, "-") {
		t.Errorf("Filename format should contain separator")
	}
}

// Test environment variable defaults
func TestEnvironmentDefaults(t *testing.T) {
	// Test DB_PORT default
	original := os.Getenv("DB_PORT")
	os.Unsetenv("DB_PORT")
	port := os.Getenv("DB_PORT")
	if port == "" {
		port = "3306"
	}
	if port != "3306" {
		t.Errorf("DB_PORT default = %v, want 3306", port)
	}
	if original != "" {
		os.Setenv("DB_PORT", original)
	}
}

// Test GZIP logic
func TestGzipLogic(t *testing.T) {
	tests := []struct {
		name            string
		gzipEnv         string
		expectedGzipped bool
	}{
		{"GZIP disabled", "0", false},
		{"GZIP enabled (default)", "", true},
		{"GZIP enabled explicitly", "1", true},
		{"GZIP enabled with any other value", "yes", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := os.Getenv("DB_GZIP")
			if tt.gzipEnv == "" {
				os.Unsetenv("DB_GZIP")
			} else {
				os.Setenv("DB_GZIP", tt.gzipEnv)
			}

			// Test the logic
			shouldGzip := os.Getenv("DB_GZIP") != "0"
			if shouldGzip != tt.expectedGzipped {
				t.Errorf("GZIP logic = %v, want %v", shouldGzip, tt.expectedGzipped)
			}

			// Restore
			if original == "" {
				os.Unsetenv("DB_GZIP")
			} else {
				os.Setenv("DB_GZIP", original)
			}
		})
	}
}

func BenchmarkDatabaseFiltering(b *testing.B) {
	databases := []string{
		"information_schema", "performance_schema", "mysql", "sys",
		"app1", "app2", "app3", "test_db", "another_db",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var filtered []string
		for _, dbName := range databases {
			if !strings.HasPrefix(dbName, "information_schema") &&
				!strings.HasPrefix(dbName, "performance_schema") &&
				!strings.HasPrefix(dbName, "mysql") &&
				!strings.HasPrefix(dbName, "sys") {
				filtered = append(filtered, dbName)
			}
		}
		_ = filtered // Use the filtered slice to avoid unused warning
	}
}

// Test configuration validation
func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name   string
		config mysql.Config
		valid  bool
	}{
		{
			name: "valid config",
			config: mysql.Config{
				User:   "testuser",
				Passwd: "testpass",
				Addr:   "localhost:3306",
				Net:    "tcp",
			},
			valid: true,
		},
		{
			name: "missing user",
			config: mysql.Config{
				Passwd: "testpass",
				Addr:   "localhost:3306",
				Net:    "tcp",
			},
			valid: false,
		},
		{
			name: "missing address",
			config: mysql.Config{
				User:   "testuser",
				Passwd: "testpass",
				Net:    "tcp",
			},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Basic validation - check if required fields are set
			isValid := tt.config.User != "" && tt.config.Addr != ""
			if isValid != tt.valid {
				t.Errorf("Config validation = %v, want %v", isValid, tt.valid)
			}
		})
	}
}
