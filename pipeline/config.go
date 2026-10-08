// newDefaultConfig: returns a Config filled with the default directories, file names, heap size and timeouts.
// Config.GraphFilePath: returns the local path of the preprocessing graph file.
// Config.SceneFilePath: returns the local path of the scene file.
// Config.DetectionGraphFilePath: returns the local path of the stage 2 detection graph file.
// Config.ContainerMemoryLimitGB: returns the container memory limit in GB, the SNAP heap times 1.3 rounded up.
// Config.ContainerMemoryLimit: returns the container memory limit formatted for docker --memory, such as "11g".

package main

import (
	"fmt"
	"math"
	"path/filepath"

	"radarpipeline/internal/resultsdb"
)

const (
	containerGraphsDir   = "/graphs"
	containerScenesDir   = "/scenes"
	containerOutputDir   = "/out"
	containerSnapHomeDir = "/root/.snap"
)

const (
	snapImageDigest        = "quay.io/bcdev/snap13@sha256:10bb3484129d5f5f879c6f5293083880957230ad5dabdc5d9acf629f3a98461b"
	gptExecutablePath      = "/opt/esa-snap/bin/gpt"
	beamDimapFormatTag     = "<formatName>BEAM-DIMAP</formatName>"
	calibrationOperatorTag = "<operator>Calibration</operator>"
)

type Config struct {
	GraphsDir string
	ScenesDir string
	OutputDir string

	AuxDataDir string

	MySQLEnvFile string

	TilesDir string

	TileRetentionRuns int

	GraphFileName  string
	SceneFileName  string
	ResultFileName string

	DetectionGraphFileName  string
	DetectionResultFileName string
	SkipDetection           bool
	KeepIntermediate        bool

	KeepVectorData bool

	SearchAreaName string

	SearchOnly bool

	ExportAOIPath string

	PassesToSurvey int
	CDSEProductID  string

	SceneAcquisitionStamp string

	ResultNameGivenExplicitly bool

	GraphInputParamName  string
	GraphOutputParamName string

	SnapHeapGB int

	ProcessingTimeoutMinutes int

	OpenResultFolderWhenDone bool
}

func newDefaultConfig() Config {
	return Config{
		GraphsDir:    "graphs",
		ScenesDir:    "scenes",
		OutputDir:    "out",
		AuxDataDir:   "auxdata",
		MySQLEnvFile: resultsdb.DefaultEnvFile,

		TilesDir:          `C:\ship-tiles`,
		TileRetentionRuns: 30,

		GraphFileName:  "preprocess_full.xml",
		ResultFileName: "result.dim",

		DetectionGraphFileName:  "ShipDetection.xml",
		DetectionResultFileName: "ships.dim",

		GraphInputParamName:  "input",
		GraphOutputParamName: "output",

		SnapHeapGB: 8,

		PassesToSurvey: 1,

		ProcessingTimeoutMinutes: 150,
	}
}

func (c *Config) GraphFilePath() string {
	return filepath.Join(c.GraphsDir, c.GraphFileName)
}

func (c *Config) SceneFilePath() string {
	return filepath.Join(c.ScenesDir, c.SceneFileName)
}

func (c *Config) DetectionGraphFilePath() string {
	return filepath.Join(c.GraphsDir, c.DetectionGraphFileName)
}

const containerMemoryHeadroomFactor = 1.3

func (c *Config) ContainerMemoryLimitGB() int {
	return int(math.Ceil(float64(c.SnapHeapGB) * containerMemoryHeadroomFactor))
}

func (c *Config) ContainerMemoryLimit() string {
	return fmt.Sprintf("%dg", c.ContainerMemoryLimitGB())
}
