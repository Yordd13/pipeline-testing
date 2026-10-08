// ReadCDSECredentials: reads the CDSE username and password from environment variables, failing if either is empty.
// requestCDSEAccessToken: signs in to the CDSE identity service with a password grant and returns the access token.
// fetchProductName: looks up a product in the CDSE catalogue by ID and returns its name.
// downloadProductArchive: downloads a product archive from CDSE to the destination path using a bearer token.
// writeBodyToPartFileThenRename: streams a body into a .part file and renames it into place only once fully written.
// getWithBearerToken: sends an authorized GET request and returns the body, failing on any non-200 status.

package cdse

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

const (
	cdseTokenURL       = "https://identity.dataspace.copernicus.eu/auth/realms/CDSE/protocol/openid-connect/token"
	cdseCatalogueURL   = "https://catalogue.dataspace.copernicus.eu/odata/v1/Products"
	cdseDownloadURL    = "https://download.dataspace.copernicus.eu/odata/v1/Products"
	cdsePublicClientID = "cdse-public"

	productDownloadTimeout = 2 * time.Hour
)

type CDSECredentials struct {
	Username string
	Password string
}

func ReadCDSECredentials() (CDSECredentials, error) {
	credentials := CDSECredentials{
		Username: os.Getenv("CDSE_USERNAME"),
		Password: os.Getenv("CDSE_PASSWORD"),
	}
	if credentials.Username == "" || credentials.Password == "" {
		return CDSECredentials{}, fmt.Errorf(
			"downloading needs CDSE_USERNAME and CDSE_PASSWORD")
	}
	return credentials, nil
}

func requestCDSEAccessToken(credentials CDSECredentials) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "password")
	form.Set("username", credentials.Username)
	form.Set("password", credentials.Password)
	form.Set("client_id", cdsePublicClientID)

	response, err := http.PostForm(cdseTokenURL, form)
	if err != nil {
		return "", fmt.Errorf("sign-in failed: %w", err)
	}
	defer response.Body.Close()

	responseBody, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("sign-in returned %d: %s",
			response.StatusCode, responseBody)
	}

	var tokenResponse struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(responseBody, &tokenResponse); err != nil {
		return "", fmt.Errorf("could not read the sign-in response: %w", err)
	}
	if tokenResponse.AccessToken == "" {
		return "", fmt.Errorf("sign-in succeeded but returned no token")
	}
	return tokenResponse.AccessToken, nil
}

func fetchProductName(accessToken, productID string) (string, error) {
	requestURL := fmt.Sprintf("%s(%s)", cdseCatalogueURL, productID)
	responseBody, err := getWithBearerToken(requestURL, accessToken, 0)
	if err != nil {
		return "", fmt.Errorf("looking up product %s failed: %w", productID, err)
	}

	var productInfo struct {
		Name string `json:"Name"`
	}
	if err := json.Unmarshal(responseBody, &productInfo); err != nil || productInfo.Name == "" {
		return "", fmt.Errorf("could not find a name for product %s", productID)
	}
	return productInfo.Name, nil
}

func downloadProductArchive(accessToken, productID, destinationPath string) error {
	request, err := http.NewRequest(http.MethodGet,
		fmt.Sprintf("%s(%s)/$value", cdseDownloadURL, productID), nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{Timeout: productDownloadTimeout}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		responseBody, _ := io.ReadAll(response.Body)
		return fmt.Errorf("download returned %d: %s", response.StatusCode, responseBody)
	}

	bytesWritten, err := writeBodyToPartFileThenRename(response.Body, destinationPath)
	if err != nil {
		return err
	}

	fmt.Printf("Downloaded %.2f GB to %s\n", float64(bytesWritten)/(1024*1024*1024), destinationPath)
	return nil
}

func writeBodyToPartFileThenRename(body io.Reader, destinationPath string) (int64, error) {
	partialPath := destinationPath + ".part"

	partialFile, err := os.Create(partialPath)
	if err != nil {
		return 0, err
	}

	bytesWritten, copyErr := io.Copy(partialFile, body)
	closeErr := partialFile.Close()

	if copyErr != nil {
		os.Remove(partialPath)
		return 0, fmt.Errorf("download stopped partway: %w", copyErr)
	}
	if closeErr != nil {
		os.Remove(partialPath)
		return 0, fmt.Errorf("could not finish writing the download: %w", closeErr)
	}
	if err := os.Rename(partialPath, destinationPath); err != nil {
		return 0, err
	}
	return bytesWritten, nil
}

func getWithBearerToken(requestURL, accessToken string, timeout time.Duration) ([]byte, error) {
	request, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)

	client := http.DefaultClient
	if timeout > 0 {
		client = &http.Client{Timeout: timeout}
	}

	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	responseBody, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request returned %d: %s", response.StatusCode, responseBody)
	}
	return responseBody, nil
}
