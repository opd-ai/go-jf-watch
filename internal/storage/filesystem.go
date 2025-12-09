package storage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/natefinch/atomic"
)

// FileManager handles atomic file operations for safe concurrent access.
// It provides checksumming, metadata management, and atomic writes
// to prevent corruption during downloads or system crashes.
type FileManager struct {
	tempDir string
	logger  *slog.Logger
}

// FileMetadata represents metadata stored alongside media files.
type FileMetadata struct {
	JellyfinID   string    `json:"jellyfin_id"`
	OriginalName string    `json:"original_name"`
	Size         int64     `json:"size"`
	Checksum     string    `json:"checksum"`
	DownloadedAt time.Time `json:"downloaded_at"`
	ContentType  string    `json:"content_type"`
	URL          string    `json:"url,omitempty"`
}

// NewFileManager creates a new file manager with the specified temp directory.
func NewFileManager(tempDir string, logger *slog.Logger) *FileManager {
	return &FileManager{
		tempDir: tempDir,
		logger:  logger,
	}
}

// WriteFileAtomic writes data to a file atomically using a temporary file.
// This prevents corruption if the write is interrupted.
func (f *FileManager) WriteFileAtomic(filename string, data []byte) error {
	f.logger.Debug("Writing file atomically",
		"filename", filename,
		"size_bytes", len(data))

	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := atomic.WriteFile(filename, bytes.NewReader(data)); err != nil {
		return fmt.Errorf("atomic write failed for %s: %w", filename, err)
	}

	return nil
}

// CopyFileAtomic copies a file atomically from source to destination.
// It calculates checksums during the copy for integrity verification.
func (f *FileManager) CopyFileAtomic(src, dst string) (string, error) {
	f.logger.Debug("Copying file atomically",
		"src", src,
		"dst", dst)

	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return "", fmt.Errorf("failed to create destination directory: %w", err)
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return "", fmt.Errorf("failed to open source file: %w", err)
	}
	defer srcFile.Close()

	// Use atomic.WriteFile with a custom writer that calculates checksum
	hasher := sha256.New()

	err = atomic.WriteFile(dst, &atomicCopyReader{
		reader: io.TeeReader(srcFile, hasher),
		logger: f.logger,
	})

	if err != nil {
		return "", fmt.Errorf("atomic copy failed: %w", err)
	}

	checksum := hex.EncodeToString(hasher.Sum(nil))

	f.logger.Debug("File copied successfully",
		"dst", dst,
		"checksum", checksum)

	return checksum, nil
}

// atomicCopyReader wraps an io.Reader to work with atomic.WriteFile
type atomicCopyReader struct {
	reader io.Reader
	logger *slog.Logger
}

func (r *atomicCopyReader) Read(p []byte) (n int, err error) {
	return r.reader.Read(p)
}

// MoveFileAtomic moves a file from source to destination atomically.
// It first attempts a rename (fastest), falling back to copy+delete if needed.
func (f *FileManager) MoveFileAtomic(src, dst string) error {
	f.logger.Debug("Moving file atomically",
		"src", src,
		"dst", dst)

	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Try rename first (fastest, works if on same filesystem)
	if err := os.Rename(src, dst); err == nil {
		return nil
	}

	// Fallback to copy + delete
	if _, err := f.CopyFileAtomic(src, dst); err != nil {
		return fmt.Errorf("failed to copy during move: %w", err)
	}

	if err := os.Remove(src); err != nil {
		f.logger.Warn("Failed to remove source after copy",
			"src", src,
			"error", err)
		// Don't fail the move operation - destination file exists
	}

	return nil
}

// CalculateChecksum calculates SHA256 checksum of a file.
func (f *FileManager) CalculateChecksum(filename string) (string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", fmt.Errorf("failed to open file for checksum: %w", err)
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("failed to calculate checksum: %w", err)
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// VerifyChecksum verifies that a file's checksum matches the expected value.
func (f *FileManager) VerifyChecksum(filename, expectedChecksum string) (bool, error) {
	actualChecksum, err := f.CalculateChecksum(filename)
	if err != nil {
		return false, err
	}

	match := actualChecksum == expectedChecksum
	if !match {
		f.logger.Warn("Checksum mismatch",
			"filename", filename,
			"expected", expectedChecksum,
			"actual", actualChecksum)
	}

	return match, nil
}

// WriteMetadata writes metadata to a .meta.json file alongside the media file.
func (f *FileManager) WriteMetadata(mediaPath string, metadata *FileMetadata) error {
	metadataPath := filepath.Join(filepath.Dir(mediaPath), ".meta.json")

	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	return f.WriteFileAtomic(metadataPath, data)
}

// ReadMetadata reads metadata from a .meta.json file.
func (f *FileManager) ReadMetadata(mediaPath string) (*FileMetadata, error) {
	metadataPath := filepath.Join(filepath.Dir(mediaPath), ".meta.json")

	data, err := os.ReadFile(metadataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("metadata file not found")
		}
		return nil, fmt.Errorf("failed to read metadata: %w", err)
	}

	var metadata FileMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return &metadata, nil
}

// GetTempFilePath generates a temporary file path for downloads.
func (f *FileManager) GetTempFilePath(id string) string {
	return filepath.Join(f.tempDir, fmt.Sprintf("%s.tmp", id))
}

// CleanupTempFiles removes old temporary files (older than 24 hours).
func (f *FileManager) CleanupTempFiles() error {
	f.logger.Debug("Cleaning up temporary files", "temp_dir", f.tempDir)

	if _, err := os.Stat(f.tempDir); os.IsNotExist(err) {
		return nil // Nothing to clean
	}

	entries, err := os.ReadDir(f.tempDir)
	if err != nil {
		return fmt.Errorf("failed to read temp directory: %w", err)
	}

	cutoff := time.Now().Add(-24 * time.Hour)
	cleanedCount := 0

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoff) {
			tempPath := filepath.Join(f.tempDir, entry.Name())
			if err := os.Remove(tempPath); err != nil {
				f.logger.Warn("Failed to remove old temp file",
					"path", tempPath,
					"error", err)
				continue
			}
			cleanedCount++
		}
	}

	if cleanedCount > 0 {
		f.logger.Info("Cleaned up temporary files",
			"count", cleanedCount)
	}

	return nil
}

// GetFileSize returns the size of a file in bytes.
func (f *FileManager) GetFileSize(filename string) (int64, error) {
	info, err := os.Stat(filename)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// FileExists checks if a file exists and is accessible.
func (f *FileManager) FileExists(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil
}

// EnsureDirectory creates a directory and all parent directories if they don't exist.
func (f *FileManager) EnsureDirectory(dir string) error {
	return os.MkdirAll(dir, 0755)
}

// RemoveFile removes a file and logs the operation.
func (f *FileManager) RemoveFile(filename string) error {
	f.logger.Debug("Removing file", "filename", filename)

	if err := os.Remove(filename); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove file %s: %w", filename, err)
	}

	return nil
}

// GetFileInfo returns file information including size and modification time.
func (f *FileManager) GetFileInfo(filename string) (os.FileInfo, error) {
	return os.Stat(filename)
}
