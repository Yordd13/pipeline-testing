// LoadEnvFile: loads the .env file into the environment if it exists, failing only when it cannot be parsed.

package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

const EnvFileName = ".env"

func LoadEnvFile() error {
	if _, err := os.Stat(EnvFileName); err != nil {
		return nil
	}
	if err := godotenv.Load(); err != nil {
		return fmt.Errorf("could not read .env: %w", err)
	}
	return nil
}
