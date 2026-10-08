// AbsolutePath: returns the absolute form of a path, or the path unchanged if it cannot be resolved.

package config

import (
	"path/filepath"
)

func AbsolutePath(path string) string {
	resolved, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return resolved
}
