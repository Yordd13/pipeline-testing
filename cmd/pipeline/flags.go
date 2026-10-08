// parseCommandLineFlags: parses the command-line flags into a Config, noting whether -output was given explicitly.

package main

import (
	"flag"

	"radarpipeline/internal/config"
)

func parseCommandLineFlags() config.Config {
	cfg := config.NewDefaultConfig()

	flag.StringVar(&cfg.SceneFileName, "scene", cfg.SceneFileName,
		"scene already in scenes/ (a .SAFE folder or a .zip)")
	flag.StringVar(&cfg.SearchAreaName, "area", cfg.SearchAreaName,
		"find and download the newest pass over this area (bulgaria, burgas)")
	flag.StringVar(&cfg.CDSEProductID, "product", cfg.CDSEProductID,
		"CDSE product id to download instead")

	flag.StringVar(&cfg.ExportAOIPath, "export-aoi", cfg.ExportAOIPath,
		"write the AOI polygon as GeoJSON to this path and exit")

	flag.StringVar(&cfg.TilesDir, "tiles-dir", cfg.TilesDir,
		"where to write the map tiles; keep this outside any synced folder")
	flag.IntVar(&cfg.TileRetentionRuns, "tile-retention", cfg.TileRetentionRuns,
		"how many runs of tiles to keep before pruning the oldest")
	flag.StringVar(&cfg.MySQLEnvFile, "mysql-env", cfg.MySQLEnvFile,
		"file holding APP_DB_PASSWORD for the results database")

	flag.BoolVar(&cfg.SearchOnly, "search-only", cfg.SearchOnly,
		"search the catalogue, report the pass and its coverage, then stop")
	flag.IntVar(&cfg.PassesToSurvey, "passes", cfg.PassesToSurvey,
		"with -search-only, survey this many recent passes instead of detailing one")

	flag.StringVar(&cfg.GraphFileName, "graph", cfg.GraphFileName,
		"graph file in graphs/")
	flag.StringVar(&cfg.ResultFileName, "output", cfg.ResultFileName,
		"name for the result file")

	flag.IntVar(&cfg.SnapHeapGB, "heap", cfg.SnapHeapGB,
		"JVM heap for SNAP in GB; the container limit follows at 1.3x")
	flag.IntVar(&cfg.ProcessingTimeoutMinutes, "timeout", cfg.ProcessingTimeoutMinutes,
		"give up on processing after this many minutes")

	flag.StringVar(&cfg.DetectionGraphFileName, "detection-graph", cfg.DetectionGraphFileName,
		"stage 2 graph in graphs/")
	flag.BoolVar(&cfg.SkipDetection, "no-detect", cfg.SkipDetection,
		"stop after preprocessing, without running ship detection")
	flag.BoolVar(&cfg.KeepIntermediate, "keep-intermediate", cfg.KeepIntermediate,
		"keep the run folder and the downloaded scenes instead of deleting them after success")

	flag.BoolVar(&cfg.KeepVectorData, "keep-vector-data", cfg.KeepVectorData,
		"leave the detection geometry in the product; SNAP then cannot render its bands")

	flag.BoolVar(&cfg.OpenResultFolderWhenDone, "open", cfg.OpenResultFolderWhenDone,
		"open the result folder when finished")

	flag.Parse()

	flag.Visit(func(givenFlag *flag.Flag) {
		if givenFlag.Name == "output" {
			cfg.ResultNameGivenExplicitly = true
		}
	})
	return cfg
}
