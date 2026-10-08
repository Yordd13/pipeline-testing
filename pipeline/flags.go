// parseCommandLineFlags: parses the command-line flags into a Config, noting whether -output was given explicitly.

package main

import "flag"

func parseCommandLineFlags() Config {
	config := newDefaultConfig()

	flag.StringVar(&config.SceneFileName, "scene", config.SceneFileName,
		"scene already in scenes/ (a .SAFE folder or a .zip)")
	flag.StringVar(&config.SearchAreaName, "area", config.SearchAreaName,
		"find and download the newest pass over this area (bulgaria, burgas)")
	flag.StringVar(&config.CDSEProductID, "product", config.CDSEProductID,
		"CDSE product id to download instead")

	flag.StringVar(&config.ExportAOIPath, "export-aoi", config.ExportAOIPath,
		"write the AOI polygon as GeoJSON to this path and exit")

	flag.StringVar(&config.TilesDir, "tiles-dir", config.TilesDir,
		"where to write the map tiles; keep this outside any synced folder")
	flag.IntVar(&config.TileRetentionRuns, "tile-retention", config.TileRetentionRuns,
		"how many runs of tiles to keep before pruning the oldest")
	flag.StringVar(&config.MySQLEnvFile, "mysql-env", config.MySQLEnvFile,
		"file holding APP_DB_PASSWORD for the results database")

	flag.BoolVar(&config.SearchOnly, "search-only", config.SearchOnly,
		"search the catalogue, report the pass and its coverage, then stop")
	flag.IntVar(&config.PassesToSurvey, "passes", config.PassesToSurvey,
		"with -search-only, survey this many recent passes instead of detailing one")

	flag.StringVar(&config.GraphFileName, "graph", config.GraphFileName,
		"graph file in graphs/")
	flag.StringVar(&config.ResultFileName, "output", config.ResultFileName,
		"name for the result file")

	flag.IntVar(&config.SnapHeapGB, "heap", config.SnapHeapGB,
		"JVM heap for SNAP in GB; the container limit follows at 1.3x")
	flag.IntVar(&config.ProcessingTimeoutMinutes, "timeout", config.ProcessingTimeoutMinutes,
		"give up on processing after this many minutes")

	flag.StringVar(&config.DetectionGraphFileName, "detection-graph", config.DetectionGraphFileName,
		"stage 2 graph in graphs/")
	flag.BoolVar(&config.SkipDetection, "no-detect", config.SkipDetection,
		"stop after preprocessing, without running ship detection")
	flag.BoolVar(&config.KeepIntermediate, "keep-intermediate", config.KeepIntermediate,
		"keep the run folder and the downloaded scenes instead of deleting them after success")

	flag.BoolVar(&config.KeepVectorData, "keep-vector-data", config.KeepVectorData,
		"leave the detection geometry in the product; SNAP then cannot render its bands")

	flag.BoolVar(&config.OpenResultFolderWhenDone, "open", config.OpenResultFolderWhenDone,
		"open the result folder when finished")

	flag.Parse()

	flag.Visit(func(givenFlag *flag.Flag) {
		if givenFlag.Name == "output" {
			config.ResultNameGivenExplicitly = true
		}
	})
	return config
}
