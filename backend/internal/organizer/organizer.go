package organizer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"curio/internal/models"
)

type FailureRecord struct {
	BatchID      string `json:"batch_id"`
	FileID       string `json:"file_id"`
	OriginalPath string `json:"original_path"`
	CurrentPath  string `json:"current_path"`
	FailedPath   string `json:"failed_path"`
	ErrorCode    string `json:"error_code"`
	ErrorMessage string `json:"error_message"`
	CreatedAt    string `json:"created_at"`
}

func MoveFile(source, target string) error {
	return MoveFileContext(context.Background(), source, target)
}

func MoveFileContext(ctx context.Context, source, target string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if filepath.Clean(source) == filepath.Clean(target) {
		return nil
	}
	if _, err := os.Stat(target); err == nil {
		if _, sourceErr := os.Stat(source); os.IsNotExist(sourceErr) {
			return nil
		}
		return errors.New(models.ErrTargetPathExists)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if err := os.Rename(source, target); err == nil {
		return nil
	}
	if err := copyFile(ctx, source, target); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		_ = os.Remove(target)
		return err
	}
	return os.Remove(source)
}

func PathExists(value string) bool {
	_, err := os.Stat(value)
	return err == nil
}

func ArchiveFailure(ctx context.Context, dirs models.DirectoryConfig, file models.MediaFile, code, message string) (string, error) {
	targetDir := filepath.Join(dirs.FailedPath, code, file.BatchID, "files")
	targetPath := filepath.Join(targetDir, FailureFileName(file))
	if err := MoveFileContext(ctx, file.CurrentPath, targetPath); err != nil {
		return "", err
	}
	record := FailureRecord{
		BatchID:      file.BatchID,
		FileID:       file.ID,
		OriginalPath: file.OriginalPath,
		CurrentPath:  file.CurrentPath,
		FailedPath:   targetPath,
		ErrorCode:    code,
		ErrorMessage: message,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	payload, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return targetPath, err
	}
	if err := os.WriteFile(targetPath+".error.json", payload, 0o644); err != nil {
		return targetPath, err
	}
	return targetPath, nil
}

func FailureFileName(file models.MediaFile) string {
	ext := filepath.Ext(file.OriginalName)
	base := strings.TrimSuffix(file.OriginalName, ext)
	suffix := strings.TrimSpace(file.FileHash)
	if len(suffix) > 12 {
		suffix = suffix[:12]
	}
	if suffix == "" {
		suffix = strings.ReplaceAll(file.ID, "-", "")
		if len(suffix) > 12 {
			suffix = suffix[:12]
		}
	}
	if suffix == "" {
		return file.OriginalName
	}
	return base + "." + suffix + ext
}

func MigrateCollection(sourceRoot, targetRoot string) error {
	info, err := os.Stat(sourceRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}
	if err := os.MkdirAll(targetRoot, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(sourceRoot)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		source := filepath.Join(sourceRoot, entry.Name())
		target := filepath.Join(targetRoot, entry.Name())
		if _, err := os.Stat(target); err == nil {
			return errors.New(models.ErrTargetPathExists)
		}
		if err := os.Rename(source, target); err != nil {
			return err
		}
	}
	return os.RemoveAll(sourceRoot)
}

func RemoveEmptyParents(startDir, stopRoot string) {
	startDir = filepath.Clean(startDir)
	stopRoot = filepath.Clean(stopRoot)
	for startDir != "" && startDir != "." && startDir != stopRoot {
		entries, err := os.ReadDir(startDir)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(startDir); err != nil {
			return
		}
		next := filepath.Dir(startDir)
		if next == startDir {
			return
		}
		startDir = next
	}
}

func copyFile(ctx context.Context, source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer output.Close()
	buf := make([]byte, 1024*1024)
	for {
		if err := ctx.Err(); err != nil {
			_ = output.Close()
			_ = os.Remove(target)
			return err
		}
		n, readErr := input.Read(buf)
		if n > 0 {
			if _, err := output.Write(buf[:n]); err != nil {
				_ = output.Close()
				_ = os.Remove(target)
				return err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			_ = output.Close()
			_ = os.Remove(target)
			return readErr
		}
	}
	if err := output.Sync(); err != nil {
		_ = output.Close()
		_ = os.Remove(target)
		return err
	}
	return nil
}
