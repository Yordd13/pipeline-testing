// reportResult: reports a written product, choosing the BEAM-DIMAP or single-file report by its extension.
// reportBeamDimapResult: checks a .dim product and its .data folder were written and prints its bands and size.
// reportSingleFileResult: checks a single-file result was written and prints its path and size.
// checkFileWasWritten: fails when the expected file is missing, is a directory, or is empty.
// describeMissingFile: builds an error for a missing file that lists what its folder does contain.
// listBandsInDataDir: lists the .img band files in a .data folder, sorted, and the folder's total size.
// openResultFolderIfRequested: opens Explorer with the result selected when requested and running on Windows.

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func reportResult(config *Config, resultPath string) error {
	if strings.HasSuffix(resultPath, ".dim") {
		return reportBeamDimapResult(config, resultPath)
	}
	return reportSingleFileResult(config, resultPath)
}

func reportBeamDimapResult(config *Config, dimHeaderPath string) error {
	if err := checkFileWasWritten(dimHeaderPath); err != nil {
		return err
	}

	dataDir := strings.TrimSuffix(dimHeaderPath, ".dim") + ".data"
	bandFileNames, totalBytes, err := listBandsInDataDir(dataDir)
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Printf("Output: %s\n", dimHeaderPath)
	fmt.Printf("Bands:  %s\n", strings.Join(bandFileNames, ", "))
	fmt.Printf("Size:   %s\n", formatBytes(totalBytes))

	openResultFolderIfRequested(config, dimHeaderPath)
	return nil
}

func reportSingleFileResult(config *Config, resultPath string) error {
	if err := checkFileWasWritten(resultPath); err != nil {
		return err
	}

	fileInfo, err := os.Stat(resultPath)
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Printf("Output: %s\n", resultPath)
	fmt.Printf("Size:   %s\n", formatBytes(fileInfo.Size()))

	fmt.Println("\nOpen it in QGIS with the layer stretch set to min/max, or it looks all black.")
	openResultFolderIfRequested(config, resultPath)
	return nil
}

func checkFileWasWritten(expectedPath string) error {
	fileInfo, err := os.Stat(expectedPath)
	if err != nil {
		return describeMissingFile(expectedPath)
	}
	if fileInfo.IsDir() {
		return fmt.Errorf("%s is a folder, not a file", expectedPath)
	}
	if fileInfo.Size() == 0 {
		return fmt.Errorf("%s was created but is empty", expectedPath)
	}
	return nil
}

func describeMissingFile(expectedPath string) error {
	parentDir := filepath.Dir(expectedPath)

	entries, err := os.ReadDir(parentDir)
	if err != nil || len(entries) == 0 {
		return fmt.Errorf("nothing was written to %s", parentDir)
	}

	presentNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		presentNames = append(presentNames, entry.Name())
	}
	sort.Strings(presentNames)

	return fmt.Errorf("no %s was written; the folder contains: %s",
		filepath.Base(expectedPath), strings.Join(presentNames, ", "))
}

func listBandsInDataDir(dataDir string) (bandFileNames []string, totalBytes int64, err error) {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return nil, 0, fmt.Errorf("the %s folder is missing, so the product is incomplete",
			filepath.Base(dataDir))
	}

	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".img") {
			bandFileNames = append(bandFileNames, entry.Name())
		}
		if info, err := entry.Info(); err == nil {
			totalBytes += info.Size()
		}
	}

	if len(bandFileNames) == 0 {
		return nil, 0, fmt.Errorf("%s contains no image bands", dataDir)
	}
	sort.Strings(bandFileNames)
	return bandFileNames, totalBytes, nil
}

func openResultFolderIfRequested(config *Config, resultPath string) {
	if !config.OpenResultFolderWhenDone {
		return
	}
	if runtime.GOOS != "windows" {
		return
	}
	exec.Command("explorer", "/select,"+absolutePath(resultPath)).Run()
}
