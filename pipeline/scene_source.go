// downloadSceneByProductID: signs in to CDSE and downloads a product as a zip into scenes, unless already there.
// ensureZipExtension: appends .zip to a product name that does not already end in it.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func downloadSceneByProductID(config *Config, productID, knownProductName string) (string, error) {
	credentials, err := readCDSECredentials()
	if err != nil {
		return "", err
	}

	fmt.Println("Signing in to CDSE...")
	accessToken, err := requestCDSEAccessToken(credentials)
	if err != nil {
		return "", err
	}

	productName := knownProductName
	if productName == "" {
		if productName, err = fetchProductName(accessToken, productID); err != nil {
			return "", err
		}
	}
	sceneFileName := ensureZipExtension(productName)

	destinationPath := filepath.Join(config.ScenesDir, sceneFileName)
	if _, err := os.Stat(destinationPath); err == nil {
		fmt.Printf("Scene: %s (already downloaded)\n", destinationPath)
		return sceneFileName, nil
	}

	fmt.Printf("Downloading %s ...\n", sceneFileName)
	if err := downloadProductArchive(accessToken, productID, destinationPath); err != nil {
		return "", err
	}
	return sceneFileName, nil
}

func ensureZipExtension(productName string) string {
	if strings.HasSuffix(productName, ".zip") {
		return productName
	}
	return productName + ".zip"
}
