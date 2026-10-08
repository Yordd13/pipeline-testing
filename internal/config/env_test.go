// TestLoadEnvFile: checks a missing .env is fine, a valid one sets variables and a malformed one is an error.

package config

import (
	"os"
	"strings"
	"testing"
)

func TestLoadEnvFile(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := LoadEnvFile(); err != nil {
		t.Errorf("no .env is fine, got %v", err)
	}

	if err := os.WriteFile(EnvFileName, []byte("PIPELINE_TEST_ONLY_VALUE=42\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Unsetenv("PIPELINE_TEST_ONLY_VALUE") })
	if err := LoadEnvFile(); err != nil || os.Getenv("PIPELINE_TEST_ONLY_VALUE") != "42" {
		t.Errorf("err = %v, value = %q", err, os.Getenv("PIPELINE_TEST_ONLY_VALUE"))
	}

	if err := os.WriteFile(EnvFileName, []byte("BROKEN='never closed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := LoadEnvFile(); err == nil || !strings.Contains(err.Error(), "could not read .env") {
		t.Errorf("err = %v, want a broken .env reported", err)
	}
}
