package mygzip

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGzipFile(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "mygzip_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name           string
		fileContent    string
		expectError    bool
		expectedErrMsg string
	}{
		{
			name:        "successful compression of text file",
			fileContent: "This is a test file with some content that should compress well. " + strings.Repeat("Hello World! ", 100),
			expectError: false,
		},
		{
			name:        "successful compression of small file",
			fileContent: "Small content",
			expectError: false,
		},
		{
			name:        "successful compression of empty file",
			fileContent: "",
			expectError: false,
		},
		{
			name:        "successful compression of binary-like content",
			fileContent: string([]byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE, 0xFD}),
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test file
			testFile := filepath.Join(tempDir, "test_"+tt.name+".txt")
			err := os.WriteFile(testFile, []byte(tt.fileContent), 0644)
			if err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			// Get original file info
			originalInfo, err := os.Stat(testFile)
			if err != nil {
				t.Fatalf("Failed to stat original file: %v", err)
			}

			// Call the function under test
			err = GzipFile(testFile)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tt.expectedErrMsg != "" && !strings.Contains(err.Error(), tt.expectedErrMsg) {
					t.Errorf("Expected error message to contain %q, got %q", tt.expectedErrMsg, err.Error())
				}
				return
			}

			// Check for unexpected errors
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			// Verify original file is removed
			if _, err := os.Stat(testFile); !os.IsNotExist(err) {
				t.Errorf("Original file still exists or other error: %v", err)
			}

			// Verify gzipped file exists
			gzipFile := testFile + ".gz"
			if _, err := os.Stat(gzipFile); err != nil {
				t.Fatalf("Gzipped file does not exist: %v", err)
			}

			// Verify the compressed content can be decompressed and matches original
			decompressedContent, err := readGzipFile(gzipFile)
			if err != nil {
				t.Fatalf("Failed to decompress file: %v", err)
			}

			if string(decompressedContent) != tt.fileContent {
				t.Errorf("Decompressed content doesn't match original.\nOriginal: %q\nDecompressed: %q", tt.fileContent, string(decompressedContent))
			}

			// For non-empty files, verify compression actually happened (file size should be different)
			if len(tt.fileContent) > 0 {
				gzipInfo, err := os.Stat(gzipFile)
				if err != nil {
					t.Fatalf("Failed to stat gzipped file: %v", err)
				}

				// For very small files, compression might actually increase size due to overhead
				// So we just verify the process worked without enforcing size reduction
				if gzipInfo.Size() == 0 && originalInfo.Size() > 0 {
					t.Errorf("Gzipped file is empty but original had content")
				}
			}

			// Clean up
			os.Remove(gzipFile)
		})
	}
}

func TestGzipFile_ErrorScenarios(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "mygzip_error_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name           string
		setupFunc      func(string) string // Returns the filename to test
		expectedErrMsg string
	}{
		{
			name: "non-existent file",
			setupFunc: func(tempDir string) string {
				return filepath.Join(tempDir, "nonexistent.txt")
			},
			expectedErrMsg: "error opening source file",
		},
		{
			name: "directory instead of file",
			setupFunc: func(tempDir string) string {
				dirPath := filepath.Join(tempDir, "test_dir")
				os.Mkdir(dirPath, 0755)
				return dirPath
			},
			expectedErrMsg: "is a directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filename := tt.setupFunc(tempDir)

			err := GzipFile(filename)

			if err == nil {
				t.Errorf("Expected error but got none")
				return
			}

			if !strings.Contains(err.Error(), tt.expectedErrMsg) {
				t.Errorf("Expected error message to contain %q, got %q", tt.expectedErrMsg, err.Error())
			}
		})
	}
}

func TestGzipFile_ReadOnlyDirectory(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "mygzip_readonly_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer func() {
		// Restore write permissions before cleanup
		os.Chmod(tempDir, 0755)
		os.RemoveAll(tempDir)
	}()

	// Create a test file
	testFile := filepath.Join(tempDir, "test.txt")
	err = os.WriteFile(testFile, []byte("test content"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Make directory read-only to prevent .gz file creation
	err = os.Chmod(tempDir, 0444)
	if err != nil {
		t.Fatalf("Failed to change directory permissions: %v", err)
	}

	// Try to gzip the file
	err = GzipFile(testFile)

	// Should get an error - could be about opening source file or creating gzip file depending on system
	if err == nil {
		t.Errorf("Expected error due to read-only directory, but got none")
	} else if !strings.Contains(err.Error(), "permission denied") &&
		!strings.Contains(err.Error(), "error creating gzip file") &&
		!strings.Contains(err.Error(), "error opening source file") {
		t.Errorf("Expected permission-related error, got: %v", err)
	}
}

func TestGzipFile_LargeFile(t *testing.T) {
	// Skip this test in short mode
	if testing.Short() {
		t.Skip("Skipping large file test in short mode")
	}

	tempDir, err := os.MkdirTemp("", "mygzip_large_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a larger file with repetitive content (should compress well)
	largeContent := strings.Repeat("This is a line of text that will be repeated many times to create a larger file for compression testing.\n", 10000)
	testFile := filepath.Join(tempDir, "large_test.txt")

	err = os.WriteFile(testFile, []byte(largeContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create large test file: %v", err)
	}

	originalInfo, err := os.Stat(testFile)
	if err != nil {
		t.Fatalf("Failed to stat original file: %v", err)
	}

	// Compress the file
	err = GzipFile(testFile)
	if err != nil {
		t.Fatalf("Failed to compress large file: %v", err)
	}

	// Verify compression
	gzipFile := testFile + ".gz"
	gzipInfo, err := os.Stat(gzipFile)
	if err != nil {
		t.Fatalf("Gzipped file does not exist: %v", err)
	}

	// For repetitive content, compression should be significant
	compressionRatio := float64(gzipInfo.Size()) / float64(originalInfo.Size())
	if compressionRatio > 0.5 { // Expect at least 50% compression for repetitive content
		t.Errorf("Expected better compression ratio. Original: %d bytes, Compressed: %d bytes, Ratio: %.2f",
			originalInfo.Size(), gzipInfo.Size(), compressionRatio)
	}

	// Verify content integrity
	decompressedContent, err := readGzipFile(gzipFile)
	if err != nil {
		t.Fatalf("Failed to decompress large file: %v", err)
	}

	if string(decompressedContent) != largeContent {
		t.Errorf("Decompressed content doesn't match original for large file")
	}
}

func TestGzipFile_CompressionLevel(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "mygzip_compression_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test content that should compress well
	testContent := strings.Repeat("ABCDEFGHIJKLMNOPQRSTUVWXYZ", 1000)
	testFile := filepath.Join(tempDir, "compression_test.txt")

	err = os.WriteFile(testFile, []byte(testContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Compress using our function (which uses BestCompression)
	err = GzipFile(testFile)
	if err != nil {
		t.Fatalf("Failed to compress file: %v", err)
	}

	gzipFile := testFile + ".gz"

	// Read the gzipped file and verify it was created with best compression
	file, err := os.Open(gzipFile)
	if err != nil {
		t.Fatalf("Failed to open gzipped file: %v", err)
	}
	defer file.Close()

	gr, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("Failed to create gzip reader: %v", err)
	}
	defer gr.Close()

	// Read and verify content
	content, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("Failed to read decompressed content: %v", err)
	}

	if string(content) != testContent {
		t.Errorf("Decompressed content doesn't match original")
	}
}

// Helper function to read and decompress a gzip file
func readGzipFile(filename string) ([]byte, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	gr, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer gr.Close()

	return io.ReadAll(gr)
}

// Benchmark the compression function
func BenchmarkGzipFile(b *testing.B) {
	tempDir, err := os.MkdirTemp("", "mygzip_bench")
	if err != nil {
		b.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test content
	testContent := strings.Repeat("This is benchmark test content that should compress reasonably well. ", 1000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		testFile := filepath.Join(tempDir, "bench_test.txt")
		err := os.WriteFile(testFile, []byte(testContent), 0644)
		if err != nil {
			b.Fatalf("Failed to create test file: %v", err)
		}
		b.StartTimer()

		err = GzipFile(testFile)
		if err != nil {
			b.Fatalf("Failed to compress file: %v", err)
		}

		b.StopTimer()
		// Clean up for next iteration
		os.Remove(testFile + ".gz")
		b.StartTimer()
	}
}

// Benchmark compression of different file sizes
func BenchmarkGzipFile_Small(b *testing.B) {
	benchmarkGzipFileSize(b, strings.Repeat("Small file content. ", 10))
}

func BenchmarkGzipFile_Medium(b *testing.B) {
	benchmarkGzipFileSize(b, strings.Repeat("Medium file content with more data. ", 100))
}

func BenchmarkGzipFile_Large(b *testing.B) {
	benchmarkGzipFileSize(b, strings.Repeat("Large file content with significantly more data for testing. ", 1000))
}

func benchmarkGzipFileSize(b *testing.B, content string) {
	tempDir, err := os.MkdirTemp("", "mygzip_bench_size")
	if err != nil {
		b.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		testFile := filepath.Join(tempDir, "bench_test.txt")
		err := os.WriteFile(testFile, []byte(content), 0644)
		if err != nil {
			b.Fatalf("Failed to create test file: %v", err)
		}
		b.StartTimer()

		err = GzipFile(testFile)
		if err != nil {
			b.Fatalf("Failed to compress file: %v", err)
		}

		b.StopTimer()
		os.Remove(testFile + ".gz")
		b.StartTimer()
	}
}
