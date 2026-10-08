// TestSceneIdentity: checks the satellite-and-timestamp stem is parsed from Sentinel-1 names and refused for others.
// TestNameOutputsAfterScene: checks output names follow the scene unless -output was given, with a fallback for unknown names.
// TestContainerMemoryFollowsHeap: checks the Docker memory limit scales with the SNAP heap and always exceeds it.

package main

import (
	"fmt"
	"testing"
)

func TestSceneIdentity(t *testing.T) {
	cases := []struct {
		sceneFileName string
		wantStem      string
		wantStamp     string
		wantOK        bool
	}{
		{"S1C_IW_GRDH_1SDV_20260831T041309_20260831T041334_009234_01258F_67D4.SAFE",
			"S1C_20260831T041309", "20260831T041309", true},
		{"S1C_IW_GRDH_1SDV_20260831T041309_20260831T041334_009234_01258F_67D4.SAFE.zip",
			"S1C_20260831T041309", "20260831T041309", true},
		{"S1A_IW_GRDH_1SDV_20260819T041309_20260819T041334_009059_011FB5_7333",
			"S1A_20260819T041309", "20260819T041309", true},
		{"S1D_IW_GRDH_1SDV_20260901T040502_20260901T040527_004379_008176_E58B.SAFE",
			"S1D_20260901T040502", "20260901T040502", true},
		{"something_random.zip", "", "", false},
		{"", "", "", false},
		{"S1C_IW_GRDH_1SDV_short_x_y_z_w.SAFE", "", "", false},
	}

	for _, c := range cases {
		stem, stamp, ok := sceneIdentity(c.sceneFileName)
		if stem != c.wantStem || stamp != c.wantStamp || ok != c.wantOK {
			t.Errorf("sceneIdentity(%q) = (%q, %q, %v), want (%q, %q, %v)",
				c.sceneFileName, stem, stamp, ok, c.wantStem, c.wantStamp, c.wantOK)
		}
	}
}

func TestNameOutputsAfterScene(t *testing.T) {
	scene := "S1C_IW_GRDH_1SDV_20260831T041309_20260831T041334_009234_01258F_67D4.SAFE"

	config := newDefaultConfig()
	nameOutputsAfterScene(&config, scene)
	if config.ResultFileName != "S1C_20260831T041309_preprocessed.dim" {
		t.Errorf("stage 1 name = %q", config.ResultFileName)
	}
	if config.DetectionResultFileName != "S1C_20260831T041309_ships.dim" {
		t.Errorf("stage 2 name = %q", config.DetectionResultFileName)
	}
	if config.SceneAcquisitionStamp != "20260831T041309" {
		t.Errorf("stamp = %q", config.SceneAcquisitionStamp)
	}

	explicit := newDefaultConfig()
	explicit.ResultNameGivenExplicitly = true
	explicit.ResultFileName = "mine.dim"
	nameOutputsAfterScene(&explicit, scene)
	if explicit.ResultFileName != "mine.dim" {
		t.Errorf("-output was overridden: %q", explicit.ResultFileName)
	}
	if explicit.SceneAcquisitionStamp != "20260831T041309" {
		t.Error("the dated CSV should still get its stamp")
	}

	unknown := newDefaultConfig()
	nameOutputsAfterScene(&unknown, "not_a_sentinel_product.zip")
	if unknown.ResultFileName != "result.dim" {
		t.Errorf("fallback broken: %q", unknown.ResultFileName)
	}
}

func TestContainerMemoryFollowsHeap(t *testing.T) {
	cases := []struct{ heapGB, wantLimitGB int }{
		{6, 8},
		{8, 11},
		{10, 13},
		{12, 16},
		{1, 2},
	}

	for _, c := range cases {
		config := newDefaultConfig()
		config.SnapHeapGB = c.heapGB
		if got := config.ContainerMemoryLimitGB(); got != c.wantLimitGB {
			t.Errorf("heap %d GB → container %d GB, want %d", c.heapGB, got, c.wantLimitGB)
		}
		if got, want := config.ContainerMemoryLimit(), fmt.Sprintf("%dg", c.wantLimitGB); got != want {
			t.Errorf("docker flag = %q, want %q", got, want)
		}
	}

	for heapGB := 1; heapGB <= 64; heapGB++ {
		config := newDefaultConfig()
		config.SnapHeapGB = heapGB
		if config.ContainerMemoryLimitGB() <= heapGB {
			t.Errorf("heap %d GB got a container limit of %d GB",
				heapGB, config.ContainerMemoryLimitGB())
		}
	}
}
