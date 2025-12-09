package storage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewFileManager(t *testing.T) {
	tempDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	fm := NewFileManager(tempDir, logger)
	if fm == nil {
		t.Fatal("FileManager should not be nil")
	}

	if fm.tempDir != tempDir {
		t.Errorf("Expected tempDir %s, got %s", tempDir, fm.tempDir)
	}
}

func TestWriteFileAtomic(t *testing.T) {
	tempDir := t.TempDir()
	fm := createTestFileManager(t, tempDir)

	testData := []byte("This is test data for atomic write")
	testFile := filepath.Join(tempDir, "subdir", "test.txt")

	err := fm.WriteFileAtomic(testFile, testData)
	if err != nil {
		t.Fatalf("WriteFileAtomic failed: %v", err)
	}

	// Verify file exists and has correct content
	readData, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read written file: %v", err)
	}

	if !bytes.Equal(testData, readData) {
		t.Errorf("File content mismatch. Expected %s, got %s", string(testData), string(readData))
	}

	// Verify directory was created
	if _, err := os.Stat(filepath.Dir(testFile)); os.IsNotExist(err) {
		t.Error("Parent directory was not created")
	}
}

func TestCalculateChecksum(t *testing.T) {
	tempDir := t.TempDir()
	fm := createTestFileManager(t, tempDir)

	testData := []byte("Test data for checksum calculation")
	testFile := filepath.Join(tempDir, "checksum_test.txt")

	// Create test file
	if err := os.WriteFile(testFile, testData, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Calculate checksum
	checksum, err := fm.CalculateChecksum(testFile)
	if err != nil {
		t.Fatalf("CalculateChecksum failed: %v", err)
	}

	// Verify checksum is correct
	hasher := sha256.New()
	hasher.Write(testData)
	expectedChecksum := hex.EncodeToString(hasher.Sum(nil))

	if checksum != expectedChecksum {
		t.Errorf("Checksum mismatch. Expected %s, got %s", expectedChecksum, checksum)
	}
}

func TestVerifyChecksum(t *testing.T) {
	tempDir := t.TempDir()
	fm := createTestFileManager(t, tempDir)

	testData := []byte("Test data for checksum verification")
	testFile := filepath.Join(tempDir, "verify_test.txt")

	// Create test file
	if err := os.WriteFile(testFile, testData, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Calculate expected checksum
	hasher := sha256.New()
	hasher.Write(testData)
	correctChecksum := hex.EncodeToString(hasher.Sum(nil))
	incorrectChecksum := "incorrect_checksum"

	// Test correct checksum
	valid, err := fm.VerifyChecksum(testFile, correctChecksum)
	if err != nil {
		t.Fatalf("VerifyChecksum failed: %v", err)
	}
	if !valid {
		t.Error("Valid checksum was reported as invalid")
	}

	// Test incorrect checksum
	valid, err = fm.VerifyChecksum(testFile, incorrectChecksum)
	if err != nil {
		t.Fatalf("VerifyChecksum failed on incorrect checksum: %v", err)
	}
	if valid {
		t.Error("Invalid checksum was reported as valid")
	}
}

func TestCopyFileAtomic(t *testing.T) {
	tempDir := t.TempDir()
	fm := createTestFileManager(t, tempDir)

	testData := []byte("Test data for atomic copy operation")
	srcFile := filepath.Join(tempDir, "source.txt")
	dstFile := filepath.Join(tempDir, "subdir", "destination.txt")

	// Create source file
	if err := os.WriteFile(srcFile, testData, 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	// Copy file atomically
	checksum, err := fm.CopyFileAtomic(srcFile, dstFile)
	if err != nil {
		t.Fatalf("CopyFileAtomic failed: %v", err)
	}

	// Verify destination file exists and has correct content
	dstData, err := os.ReadFile(dstFile)
	if err != nil {
		t.Fatalf("Failed to read destination file: %v", err)
	}

	if !bytes.Equal(testData, dstData) {
		t.Errorf("Copied file content mismatch. Expected %s, got %s", string(testData), string(dstData))
	}

	// Verify checksum is correct
	expectedChecksum, err := fm.CalculateChecksum(dstFile)
	if err != nil {
		t.Fatalf("Failed to calculate checksum of destination: %v", err)
	}

	if checksum != expectedChecksum {
		t.Errorf("Returned checksum %s doesn't match actual checksum %s", checksum, expectedChecksum)
	}

	// Verify destination directory was created
	if _, err := os.Stat(filepath.Dir(dstFile)); os.IsNotExist(err) {
		t.Error("Destination directory was not created")
	}
}

func TestMoveFileAtomic(t *testing.T) {
	tempDir := t.TempDir()
	fm := createTestFileManager(t, tempDir)

	testData := []byte("Test data for atomic move operation")
	srcFile := filepath.Join(tempDir, "source.txt")
	dstFile := filepath.Join(tempDir, "subdir", "destination.txt")

	// Create source file
	if err := os.WriteFile(srcFile, testData, 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	// Move file atomically
	err := fm.MoveFileAtomic(srcFile, dstFile)
	if err != nil {
		t.Fatalf("MoveFileAtomic failed: %v", err)
	}

	// Verify source file no longer exists
	if _, err := os.Stat(srcFile); !os.IsNotExist(err) {
		t.Error("Source file still exists after move")
	}

	// Verify destination file exists and has correct content
	dstData, err := os.ReadFile(dstFile)
	if err != nil {
		t.Fatalf("Failed to read destination file: %v", err)
	}

	if !bytes.Equal(testData, dstData) {
		t.Errorf("Moved file content mismatch. Expected %s, got %s", string(testData), string(dstData))
	}
}

func TestWriteAndReadMetadata(t *testing.T) {
	tempDir := t.TempDir()
	fm := createTestFileManager(t, tempDir)

	mediaPath := filepath.Join(tempDir, "movies", "test-movie", "video.mkv")
	metadata := &FileMetadata{
		JellyfinID:   "jellyfin-123",
		OriginalName: "Test Movie.mkv",
		Size:         1024 * 1024 * 500, // 500MB
		Checksum:     "abcdef123456789",
		DownloadedAt: time.Now(),
		ContentType:  "video/x-matroska",
		URL:          "http://jellyfin.example.com/video.mkv",
	}

	// Create directory structure
	if err := os.MkdirAll(filepath.Dir(mediaPath), 0755); err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}

	// Write metadata
	err := fm.WriteMetadata(mediaPath, metadata)
	if err != nil {
		t.Fatalf("WriteMetadata failed: %v", err)
	}

	// Read metadata back
	readMetadata, err := fm.ReadMetadata(mediaPath)
	if err != nil {
		t.Fatalf("ReadMetadata failed: %v", err)
	}

	// Verify metadata fields
	if readMetadata.JellyfinID != metadata.JellyfinID {
		t.Errorf("JellyfinID mismatch. Expected %s, got %s", metadata.JellyfinID, readMetadata.JellyfinID)
	}
	if readMetadata.OriginalName != metadata.OriginalName {
		t.Errorf("OriginalName mismatch. Expected %s, got %s", metadata.OriginalName, readMetadata.OriginalName)
	}
	if readMetadata.Size != metadata.Size {
		t.Errorf("Size mismatch. Expected %d, got %d", metadata.Size, readMetadata.Size)
	}
	if readMetadata.Checksum != metadata.Checksum {
		t.Errorf("Checksum mismatch. Expected %s, got %s", metadata.Checksum, readMetadata.Checksum)
	}

	// Verify metadata file exists
	metadataPath := filepath.Join(filepath.Dir(mediaPath), ".meta.json")
	if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
		t.Error("Metadata file was not created")
	}
}

func TestGetTempFilePath(t *testing.T) {
	tempDir := t.TempDir()
	fm := createTestFileManager(t, tempDir)

	testID := "test-download-123"
	tempPath := fm.GetTempFilePath(testID)

	expectedPath := filepath.Join(tempDir, testID+".tmp")
	if tempPath != expectedPath {
		t.Errorf("Expected temp path %s, got %s", expectedPath, tempPath)
	}
}

func TestCleanupTempFiles(t *testing.T) {
	tempDir := t.TempDir()
	fm := createTestFileManager(t, tempDir)

	now := time.Now()

	// Create old temp file (older than 24 hours)
	oldTempFile := filepath.Join(tempDir, "old.tmp")
	if err := os.WriteFile(oldTempFile, []byte("old"), 0644); err != nil {
		t.Fatalf("Failed to create old temp file: %v", err)
	}
	oldTime := now.Add(-48 * time.Hour)
	if err := os.Chtimes(oldTempFile, oldTime, oldTime); err != nil {
		t.Fatalf("Failed to change old temp file time: %v", err)
	}

	// Create new temp file (less than 24 hours old)
	newTempFile := filepath.Join(tempDir, "new.tmp")
	if err := os.WriteFile(newTempFile, []byte("new"), 0644); err != nil {
		t.Fatalf("Failed to create new temp file: %v", err)
	}

	// Create non-temp file
	regularFile := filepath.Join(tempDir, "regular.txt")
	if err := os.WriteFile(regularFile, []byte("regular"), 0644); err != nil {
		t.Fatalf("Failed to create regular file: %v", err)
	}

	// Run cleanup
	err := fm.CleanupTempFiles()
	if err != nil {
		t.Fatalf("CleanupTempFiles failed: %v", err)
	}

	// Verify old temp file was removed
	if _, err := os.Stat(oldTempFile); !os.IsNotExist(err) {
		t.Error("Old temp file was not removed")
	}

	// Verify new temp file still exists
	if _, err := os.Stat(newTempFile); os.IsNotExist(err) {
		t.Error("New temp file was incorrectly removed")
	}

	// Verify regular file still exists
	if _, err := os.Stat(regularFile); os.IsNotExist(err) {
		t.Error("Regular file was incorrectly removed")
	}
}

func TestGetFileSize(t *testing.T) {
	tempDir := t.TempDir()
	fm := createTestFileManager(t, tempDir)

	testData := make([]byte, 1024) // 1KB
	testFile := filepath.Join(tempDir, "size_test.txt")

	if err := os.WriteFile(testFile, testData, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	size, err := fm.GetFileSize(testFile)
	if err != nil {
		t.Fatalf("GetFileSize failed: %v", err)
	}

	if size != 1024 {
		t.Errorf("Expected file size 1024, got %d", size)
	}

	// Test non-existent file
	_, err = fm.GetFileSize(filepath.Join(tempDir, "non-existent.txt"))
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
}

func TestFileExists(t *testing.T) {
	tempDir := t.TempDir()
	fm := createTestFileManager(t, tempDir)

	existingFile := filepath.Join(tempDir, "exists.txt")
	nonExistentFile := filepath.Join(tempDir, "does-not-exist.txt")

	// Create existing file
	if err := os.WriteFile(existingFile, []byte("exists"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test existing file
	if !fm.FileExists(existingFile) {
		t.Error("Existing file reported as non-existent")
	}

	// Test non-existent file
	if fm.FileExists(nonExistentFile) {
		t.Error("Non-existent file reported as existing")
	}
}

func TestEnsureDirectory(t *testing.T) {
	tempDir := t.TempDir()
	fm := createTestFileManager(t, tempDir)

	testDir := filepath.Join(tempDir, "level1", "level2", "level3")

	err := fm.EnsureDirectory(testDir)
	if err != nil {
		t.Fatalf("EnsureDirectory failed: %v", err)
	}

	// Verify directory was created
	if _, err := os.Stat(testDir); os.IsNotExist(err) {
		t.Error("Directory was not created")
	}

	// Test with existing directory (should not error)
	err = fm.EnsureDirectory(testDir)
	if err != nil {
		t.Fatalf("EnsureDirectory failed on existing directory: %v", err)
	}
}

func TestRemoveFile(t *testing.T) {
	tempDir := t.TempDir()
	fm := createTestFileManager(t, tempDir)

	testFile := filepath.Join(tempDir, "remove_test.txt")

	// Create test file
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Remove file
	err := fm.RemoveFile(testFile)
	if err != nil {
		t.Fatalf("RemoveFile failed: %v", err)
	}

	// Verify file was removed
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Error("File was not removed")
	}

	// Test removing non-existent file (should not error)
	err = fm.RemoveFile(testFile)
	if err != nil {
		t.Fatalf("RemoveFile failed on non-existent file: %v", err)
	}
}

func TestGetFileInfo(t *testing.T) {
	tempDir := t.TempDir()
	fm := createTestFileManager(t, tempDir)

	testData := []byte("test file info")
	testFile := filepath.Join(tempDir, "info_test.txt")

	if err := os.WriteFile(testFile, testData, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	info, err := fm.GetFileInfo(testFile)
	if err != nil {
		t.Fatalf("GetFileInfo failed: %v", err)
	}

	if info.Size() != int64(len(testData)) {
		t.Errorf("Expected file size %d, got %d", len(testData), info.Size())
	}

	if info.Name() != "info_test.txt" {
		t.Errorf("Expected file name 'info_test.txt', got %s", info.Name())
	}

	if info.IsDir() {
		t.Error("File reported as directory")
	}
}

func TestAtomicOperationsConcurrency(t *testing.T) {
	tempDir := t.TempDir()
	fm := createTestFileManager(t, tempDir)

	// Test concurrent atomic writes to different files
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(id int) {
			testData := []byte(fmt.Sprintf("concurrent test data %d", id))
			testFile := filepath.Join(tempDir, fmt.Sprintf("concurrent_%d.txt", id))

			err := fm.WriteFileAtomic(testFile, testData)
			if err != nil {
				t.Errorf("Concurrent WriteFileAtomic failed for file %d: %v", id, err)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify all files were created correctly
	for i := 0; i < 10; i++ {
		testFile := filepath.Join(tempDir, fmt.Sprintf("concurrent_%d.txt", i))
		expectedData := []byte(fmt.Sprintf("concurrent test data %d", i))

		actualData, err := os.ReadFile(testFile)
		if err != nil {
			t.Errorf("Failed to read concurrent file %d: %v", i, err)
			continue
		}

		if !bytes.Equal(expectedData, actualData) {
			t.Errorf("Concurrent file %d has incorrect content", i)
		}
	}
}

// Helper function to create a test file manager
func createTestFileManager(t *testing.T, tempDir string) *FileManager {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	return NewFileManager(tempDir, logger)
}
