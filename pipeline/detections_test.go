// TestRemoveMasksSection: checks the Masks block is cut from a DIMAP header while everything around it is kept.
// TestRemoveMasksSectionWithoutMasks: checks a header with no Masks block is left byte-for-byte unchanged.
// namedEntry.Name: returns the fake directory entry's name.
// namedEntry.IsDir: reports that the fake directory entry is never a folder.
// namedEntry.Type: returns a zero file mode for the fake directory entry.
// namedEntry.Info: returns os.ErrNotExist because the fake directory entry has no file info.
// TestFindDetectionFile: checks the detection CSV is picked over SNAP's placeholder pins and ground control point files.
// TestMakeDetectionProductViewable: checks the detection list and its mask are removed, and a missing header is an error.

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoveMasksSection(t *testing.T) {
	const header = `<Dimap_Document name="x.dim">
    <Data_Access>keep me</Data_Access>
    <Masks>
        <Mask type="Geometry"><NAME value="ShipDetections" /></Mask>
    </Masks>
    <Image_Interpretation>keep me too</Image_Interpretation>
</Dimap_Document>`

	path := filepath.Join(t.TempDir(), "x.dim")
	if err := os.WriteFile(path, []byte(header), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := removeMasksSection(path); err != nil {
		t.Fatal(err)
	}

	rewritten, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(rewritten)

	if strings.Contains(got, "Masks") || strings.Contains(got, "ShipDetections") {
		t.Errorf("the masks block survived:\n%s", got)
	}
	for _, keep := range []string{"keep me", "keep me too", "</Dimap_Document>"} {
		if !strings.Contains(got, keep) {
			t.Errorf("removal took %q with it:\n%s", keep, got)
		}
	}
	if strings.Contains(got, ".tmp") {
		t.Error("a temporary file leaked into the header")
	}
}

func TestRemoveMasksSectionWithoutMasks(t *testing.T) {
	const header = `<Dimap_Document name="x.dim"><Data_Access/></Dimap_Document>`

	path := filepath.Join(t.TempDir(), "x.dim")
	if err := os.WriteFile(path, []byte(header), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := removeMasksSection(path); err != nil {
		t.Fatal(err)
	}

	unchanged, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(unchanged) != header {
		t.Errorf("a header with no masks was rewritten:\n%s", unchanged)
	}
}

type namedEntry string

func (n namedEntry) Name() string               { return string(n) }
func (n namedEntry) IsDir() bool                { return false }
func (n namedEntry) Type() os.FileMode          { return 0 }
func (n namedEntry) Info() (os.FileInfo, error) { return nil, os.ErrNotExist }

func TestFindDetectionFile(t *testing.T) {
	cases := []struct {
		name    string
		entries []string
		want    string
	}{
		{"named after the node", []string{"pins.csv", "ShipDetections_Object-Discrimination.csv"}, "ShipDetections_Object-Discrimination.csv"},
		{"any other CSV", []string{"pins.csv", "ground_control_points.csv", "targets.CSV"}, "targets.CSV"},
		{"only SNAP's placeholders", []string{"pins.csv", "ground_control_points.csv", "notes.txt"}, ""},
		{"nothing", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			entries := make([]os.DirEntry, 0, len(c.entries))
			for _, name := range c.entries {
				entries = append(entries, namedEntry(name))
			}
			got, found := findDetectionFile(entries)
			if got != c.want || found != (c.want != "") {
				t.Errorf("found %q (%v), want %q", got, found, c.want)
			}
		})
	}
}

func TestMakeDetectionProductViewable(t *testing.T) {
	dir := t.TempDir()
	config := newDefaultConfig()

	if err := makeDetectionProductViewable(&config, filepath.Join(dir, "bare.dim")); err != nil {
		t.Errorf("a product with no vector data: %v", err)
	}
	writeText(t, filepath.Join(dir, "quiet.data", vectorDataDirName, "pins.csv"), "placeholder")
	if err := makeDetectionProductViewable(&config, filepath.Join(dir, "quiet.dim")); err != nil {
		t.Errorf("a product with no detections: %v", err)
	}

	ships := writeShipsProduct(t, dir, "ships", []string{"target_000\t1\t1\t43\t28\t20\t60"})
	captureStdout(t, func() {
		if err := makeDetectionProductViewable(&config, ships); err != nil {
			t.Error(err)
		}
	})
	if _, err := os.Stat(filepath.Join(dir, "ships.data", vectorDataDirName, "ShipDetections.csv")); !os.IsNotExist(err) {
		t.Error("the detection list survived")
	}
	header, _ := os.ReadFile(ships)
	if strings.Contains(string(header), "<Masks>") {
		t.Errorf("the mask survived in the header:\n%s", header)
	}

	broken := writeShipsProduct(t, dir, "broken", []string{"target_000\t1\t1\t43\t28\t20\t60"})
	os.Remove(broken)
	captureStdout(t, func() {
		if err := makeDetectionProductViewable(&config, broken); err == nil {
			t.Error("a product without a header was reported as cleared")
		}
	})
}
