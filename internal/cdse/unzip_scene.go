// EnsureSceneIsUnpacked: unpacks a scene zip next to itself unless already done, returning the folder name.
// extractZip: extracts every entry of a zip into the destination folder and returns the bytes written.
// extractZipEntry: extracts one zip entry to its safe target path, creating directories as needed.
// safeJoin: resolves a zip entry name under the destination, refusing paths outside the expected top folder.

package cdse

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"radarpipeline/internal/config"
)

func EnsureSceneIsUnpacked(cfg *config.Config, sceneFileName string) (string, error) {
	if !strings.HasSuffix(strings.ToLower(sceneFileName), ".zip") {
		return sceneFileName, nil
	}

	unpackedName := strings.TrimSuffix(sceneFileName, filepath.Ext(sceneFileName))
	unpackedPath := filepath.Join(cfg.ScenesDir, unpackedName)

	if info, err := os.Stat(unpackedPath); err == nil && info.IsDir() {
		fmt.Printf("Unpacked: %s (already there)\n", unpackedPath)
		return unpackedName, nil
	}

	archivePath := filepath.Join(cfg.ScenesDir, sceneFileName)
	fmt.Printf("Unpacking %s ...\n", sceneFileName)

	startedAt := time.Now()
	bytesWritten, err := extractZip(archivePath, cfg.ScenesDir, unpackedName)
	if err != nil {
		os.RemoveAll(unpackedPath)
		return "", fmt.Errorf("unpacking %s failed: %w", sceneFileName, err)
	}

	fmt.Printf("Unpacked %.2f GB in %s\n",
		float64(bytesWritten)/(1024*1024*1024), time.Since(startedAt).Round(time.Second))
	return unpackedName, nil
}

func extractZip(archivePath, destDir, expectedTopDir string) (int64, error) {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return 0, err
	}
	defer reader.Close()

	var totalBytes int64
	for _, entry := range reader.File {
		written, err := extractZipEntry(entry, destDir, expectedTopDir)
		if err != nil {
			return 0, err
		}
		totalBytes += written
	}
	return totalBytes, nil
}

func extractZipEntry(entry *zip.File, destDir, expectedTopDir string) (int64, error) {
	targetPath, err := safeJoin(destDir, entry.Name, expectedTopDir)
	if err != nil {
		return 0, err
	}

	if entry.FileInfo().IsDir() {
		return 0, os.MkdirAll(targetPath, 0o755)
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return 0, err
	}

	source, err := entry.Open()
	if err != nil {
		return 0, err
	}
	defer source.Close()

	target, err := os.Create(targetPath)
	if err != nil {
		return 0, err
	}

	written, copyErr := io.Copy(target, source)
	closeErr := target.Close()
	if copyErr != nil {
		return 0, copyErr
	}
	if closeErr != nil {
		return 0, closeErr
	}
	return written, nil
}

func safeJoin(destDir, entryName, expectedTopDir string) (string, error) {
	cleaned := filepath.Clean(filepath.FromSlash(entryName))
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("refusing zip entry with an unsafe path: %q", entryName)
	}

	targetPath := filepath.Join(destDir, cleaned)

	allowedRoot := filepath.Join(destDir, expectedTopDir)
	if targetPath != allowedRoot && !strings.HasPrefix(targetPath, allowedRoot+string(os.PathSeparator)) {
		return "", fmt.Errorf("zip entry %q is outside the expected %s directory",
			entryName, expectedTopDir)
	}
	return targetPath, nil
}
