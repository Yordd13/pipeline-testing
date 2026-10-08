// FormatBytes: formats a byte count as a rounded figure in GB or MB.

package config

import (
	"fmt"
)

func FormatBytes(count int64) string {
	const gigabyte = 1024 * 1024 * 1024
	if count >= gigabyte {
		return fmt.Sprintf("%.1f GB", float64(count)/gigabyte)
	}
	return fmt.Sprintf("%.1f MB", float64(count)/(1024*1024))
}
