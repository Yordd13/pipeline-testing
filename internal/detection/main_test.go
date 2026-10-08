// TestMain: runs the package tests with the fake docker and explorer first on PATH.

package detection

import (
	"testing"

	"radarpipeline/internal/testsupport"
)

func TestMain(m *testing.M) { testsupport.Main(m) }
