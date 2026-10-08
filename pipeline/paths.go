// absolutePath: returns the absolute form of a path, or the path unchanged if it cannot be resolved.

package main

import "path/filepath"

func absolutePath(path string) string {
	resolved, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return resolved
}
