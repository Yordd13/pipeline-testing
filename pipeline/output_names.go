// nameOutputsAfterScene: renames both stages' output products after the scene's mission and acquisition start.
// sceneIdentity: extracts the mission-plus-start stem and acquisition stamp from a Sentinel-1 product name.

package main

import (
	"strings"
)

const (
	preprocessedNameSuffix = "_preprocessed.dim"
	detectionNameSuffix    = "_ships.dim"

	acquisitionStampLength = len("20260831T041309")
)

func nameOutputsAfterScene(config *Config, sceneFileName string) {
	stem, acquiredAt, ok := sceneIdentity(sceneFileName)
	if !ok {
		return
	}

	config.SceneAcquisitionStamp = acquiredAt
	if config.ResultNameGivenExplicitly {
		return
	}
	config.ResultFileName = stem + preprocessedNameSuffix
	config.DetectionResultFileName = stem + detectionNameSuffix
}

func sceneIdentity(sceneFileName string) (stem, acquiredAt string, ok bool) {
	base := strings.TrimSuffix(sceneFileName, ".zip")
	base = strings.TrimSuffix(base, ".SAFE")

	fields := strings.Split(base, "_")
	if len(fields) < 5 {
		return "", "", false
	}

	mission, start := fields[0], fields[4]
	if !strings.HasPrefix(mission, "S1") || len(start) != acquisitionStampLength {
		return "", "", false
	}
	return mission + "_" + start, start, true
}
