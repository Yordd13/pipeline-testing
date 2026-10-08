// WriteText: writes a small text file, creating its folder first.
// CaptureStdout: runs the function and returns what it printed to standard output.

package testsupport

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func WriteText(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func CaptureStdout(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = writer
	done := make(chan string)
	go func() {
		contents, _ := io.ReadAll(reader)
		done <- string(contents)
	}()
	defer func() { os.Stdout = original }()
	fn()
	writer.Close()
	os.Stdout = original
	return <-done
}
