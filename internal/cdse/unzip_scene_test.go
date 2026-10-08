// TestUnpackingRefusesEntriesOutsideTheProduct: checks zip entries escaping the product folder abort unpacking and leave nothing.
// TestAFolderNeedsNoUnpacking: checks an unpacked .SAFE folder is used as is and a .zip name gets no second extension.

package cdse

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"radarpipeline/internal/config"
)

func TestUnpackingRefusesEntriesOutsideTheProduct(t *testing.T) {
	cases := map[string]string{
		"climbs out":        "../evil.txt",
		"absolute":          "/etc/evil.txt",
		"beside the folder": "other.SAFE/evil.txt",
	}
	for name, entry := range cases {
		t.Run(name, func(t *testing.T) {
			scenes := t.TempDir()
			archive := filepath.Join(scenes, "product.SAFE.zip")
			file, err := os.Create(archive)
			if err != nil {
				t.Fatal(err)
			}
			writer := zip.NewWriter(file)
			w, _ := writer.Create("product.SAFE/manifest.safe")
			w.Write([]byte("m"))
			w, _ = writer.Create(entry)
			w.Write([]byte("evil"))
			writer.Close()
			file.Close()

			cfg := config.NewDefaultConfig()
			cfg.ScenesDir = scenes
			if _, err := EnsureSceneIsUnpacked(&cfg, "product.SAFE.zip"); err == nil {
				t.Fatalf("an archive with %q was unpacked", entry)
			}
			if _, err := os.Stat(filepath.Join(scenes, "product.SAFE")); !os.IsNotExist(err) {
				t.Error("a half-unpacked folder was left behind")
			}
		})
	}
}

func TestAFolderNeedsNoUnpacking(t *testing.T) {
	cfg := config.NewDefaultConfig()
	if name, err := EnsureSceneIsUnpacked(&cfg, "product.SAFE"); err != nil || name != "product.SAFE" {
		t.Errorf("name = %q, %v", name, err)
	}
	if got := ensureZipExtension("x.zip"); got != "x.zip" {
		t.Errorf("ensureZipExtension added a second .zip: %q", got)
	}
}
