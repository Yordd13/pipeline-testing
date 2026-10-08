// FindDetectionFile: picks the ship detection CSV out of a vector_data listing, skipping SNAP's placeholders.
// MakeDetectionProductViewable: removes the detection vector data and masks from the product so SNAP can render it.
// removeMasksSection: removes the <Masks> block from a BEAM-DIMAP header, rewriting it via a temporary file.

package snap

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"radarpipeline/internal/config"
)

const VectorDataDirName = "vector_data"

func FindDetectionFile(entries []os.DirEntry) (string, bool) {
	for _, entry := range entries {
		name := entry.Name()
		if strings.Contains(strings.ToLower(name), "shipdetections") {
			return name, true
		}
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".csv") {
			continue
		}
		if name == "pins.csv" || name == "ground_control_points.csv" {
			continue
		}
		return name, true
	}
	return "", false
}

const (
	masksSectionStart = "<Masks>"
	masksSectionEnd   = "</Masks>"
)

func MakeDetectionProductViewable(cfg *config.Config, detectionProductPath string) error {
	if cfg.KeepVectorData {
		fmt.Println("\nLeft the detection geometry in place (-keep-vector-data);")
		fmt.Println("  SNAP may not render this product's bands.")
		return nil
	}

	vectorDir := filepath.Join(
		strings.TrimSuffix(detectionProductPath, ".dim")+".data", VectorDataDirName)

	entries, err := os.ReadDir(vectorDir)
	if err != nil {
		return nil
	}
	detectionFile, found := FindDetectionFile(entries)
	if !found {
		return nil
	}

	if err := os.Remove(filepath.Join(vectorDir, detectionFile)); err != nil {
		return fmt.Errorf("could not remove %s: %w", detectionFile, err)
	}
	if err := removeMasksSection(detectionProductPath); err != nil {
		return err
	}

	fmt.Println("\nRemoved the detection geometry from the product so SNAP can render it.")
	fmt.Println("  The detections are in MySQL and in the ship_bit_msk band.")
	return nil
}

func removeMasksSection(dimHeaderPath string) error {
	contents, err := os.ReadFile(dimHeaderPath)
	if err != nil {
		return err
	}

	header := string(contents)
	start := strings.Index(header, masksSectionStart)
	end := strings.Index(header, masksSectionEnd)
	if start < 0 || end < start {
		return nil
	}

	trimmed := header[:start] + header[end+len(masksSectionEnd):]

	temporaryPath := dimHeaderPath + ".tmp"
	if err := os.WriteFile(temporaryPath, []byte(trimmed), 0o644); err != nil {
		return err
	}
	return os.Rename(temporaryPath, dimHeaderPath)
}
