// endOfRun: builds a merged three-slice pass, one slice failed, with awkward detections and a tiles sidecar.
// errFake.Error: returns the fake error's text.
// TestPassRecordCarriesTheRunIntoTheDatabaseShape: checks a finished run maps to a valid pass record with slices, counts and raster.
// TestPassRecordKeepsAMissingDistanceToShoreMissing: checks an unmeasured shore distance is stored as NULL, not zero.
// TestPassRecordWithoutAPictureHasNoRaster: checks a run without a raster sidecar gets no raster layer.
// TestPassRecordLeavesUnknownCoverageNull: checks an unknown coverage percentage is stored as NULL.
// TestPassRecordRefusesWhatItCannotStore: checks a missing orbit, NaN coverage or unknown source slice is refused.
// TestTheRecordIsStoredInOneTransaction: checks SavePass writes the pass, slices, detections and raster in one transaction.
// TestSavingWithoutADatabaseSaysWhy: checks saving or readiness without MySQL credentials fails with a hint, never connecting.
// TestAISWarningNeverStopsTheRun: checks the AIS warning is silent without a time and only notes an unreachable database.

package main

import (
	"database/sql"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"radarpipeline/internal/resultsdb"
)

func endOfRun(t *testing.T) (*PassPlan, mergeOutcome, rasterSidecar) {
	t.Helper()
	plan := &PassPlan{AreaName: "bulgaria", CoveragePercent: 21.4}
	plan.Slices = []*SliceJob{
		{
			Index: 1, ProductID: "2b91b54d-4c19-4c52-af29-4a523ef3043f",
			ProductName: "S1D_IW_GRDH_1SDV_20260926T155130_20260926T155155_004751_008E53_DDA3.SAFE",
			Mission:     "S1D", AbsoluteOrbit: "004751",
			AcquiredAt:           time.Date(2026, 9, 26, 15, 51, 30, 640_000_000, time.UTC),
			DetectionProductPath: "slice_1/ships.dim",
		},
		{
			Index: 2, ProductID: "7f3c2a10-0000-4000-8000-000000000002",
			ProductName: "S1D_IW_GRDH_1SDV_20260926T155155_20260926T155220_004751_008E53_B0B0.SAFE.zip",
			Mission:     "S1D", AbsoluteOrbit: "004751",
			AcquiredAt:           time.Date(2026, 9, 26, 15, 51, 55, 200_000_000, time.UTC),
			DetectionProductPath: "slice_2/ships.dim",
		},
		{
			Index: 3, ProductID: "7f3c2a10-0000-4000-8000-000000000003",
			ProductName: "S1D_IW_GRDH_1SDV_20260926T155220_20260926T155245_004751_008E53_C0C0.SAFE",
			Mission:     "S1D", AbsoluteOrbit: "004751",
			AcquiredAt: time.Date(2026, 9, 26, 15, 52, 20, 0, time.UTC),
			Failure:    errFake("SNAP failed"),
		},
	}

	detection := func(slice *SliceJob, id string, lat, lon float64, shore float64, measured bool) Detection {
		return Detection{
			ID: slice.SliceName() + "_" + id, Latitude: lat, Longitude: lon,
			WidthM: "210.0", LengthM: "290.0", PixelX: "12214", PixelY: "8064",
			SourceSlice: slice.SliceName(), DistanceToShoreM: shore, ShoreMeasured: measured,
		}
	}
	kept := []Detection{
		detection(plan.Slices[0], "target_013", 43.52597123456, 29.92743888, 106327.4, true),
		detection(plan.Slices[0], "target_020", 42.5, 28.0, 0, false),
		detection(plan.Slices[1], "target_000", 42.013, 27.4017, 1.6, true),
	}
	outcome := mergeOutcome{
		RunID:   "2026-09-26T155130Z_004751",
		RawCFAR: 26, DroppedOutsideAOI: 22, SeamDuplicates: 1, Written: 3, Kept: kept,
	}
	sidecar := rasterSidecar{
		Type: "tiles", File: "2026-09-26T155130Z_004751/{z}/{x}/{y}.webp",
		Bounds:        sidecarBounds{South: 42.34, West: 28.83, North: 44.25, East: 32.42},
		MaxNativeZoom: 12, MaxZoom: 16,
	}
	return plan, outcome, sidecar
}

type errFake string

func (e errFake) Error() string { return string(e) }

func TestPassRecordCarriesTheRunIntoTheDatabaseShape(t *testing.T) {
	plan, outcome, sidecar := endOfRun(t)
	pass, err := passRecord(plan, outcome, &sidecar)
	if err != nil {
		t.Fatal(err)
	}

	if !pass.Start.Equal(plan.Slices[0].AcquiredAt) || pass.Orbit != 4751 || pass.Area != "bulgaria" {
		t.Errorf("pass identity = %s, orbit %d, %q", pass.Start, pass.Orbit, pass.Area)
	}
	if pass.CoveragePercent == nil || *pass.CoveragePercent != 21 {
		t.Errorf("coverage = %v, want 21", pass.CoveragePercent)
	}
	if pass.SlicesProcessed != 2 || pass.SlicesFailed != 1 {
		t.Errorf("slices processed %d, failed %d, want 2 and 1", pass.SlicesProcessed, pass.SlicesFailed)
	}
	if pass.RawCFAR != 26 || pass.DroppedOutsideAOI != 22 || pass.SeamDuplicates != 1 || pass.Written != 3 {
		t.Errorf("counts = %+v", pass)
	}
	if pass.AOIVertices != len(BulgarianWatersAOI) {
		t.Errorf("AOI vertices = %d", pass.AOIVertices)
	}

	if len(pass.Slices) != 3 {
		t.Fatalf("%d slices, want all three including the failed one", len(pass.Slices))
	}
	if pass.Slices[2].ProductID == "" || len(pass.Slices[2].Detections) != 0 {
		t.Errorf("failed slice = %+v", pass.Slices[2])
	}
	if strings.HasSuffix(pass.Slices[1].ProductName, ".zip") || strings.HasSuffix(pass.Slices[1].ProductName, ".SAFE") {
		t.Errorf("product name kept its extension: %q", pass.Slices[1].ProductName)
	}

	first := pass.Slices[0].Detections[0]
	if first.Latitude != "43.525971" || first.Longitude != "29.927439" {
		t.Errorf("coordinates = %s, %s, want six places", first.Latitude, first.Longitude)
	}
	if first.DistanceToShoreM == nil || *first.DistanceToShoreM != "106327" {
		t.Errorf("distance to shore = %v, want 106327", first.DistanceToShoreM)
	}
	if len(pass.Slices[1].Detections) != 1 {
		t.Errorf("slice_2 got %d detections, want 1", len(pass.Slices[1].Detections))
	}

	if pass.Raster == nil || pass.Raster.Type != "tiles" || *pass.Raster.MaxNativeZoom != 12 || *pass.Raster.MaxZoom != 16 {
		t.Errorf("raster = %+v", pass.Raster)
	}
	if err := pass.Validate(); err != nil {
		t.Errorf("a real end of run does not validate: %v", err)
	}
}

func TestPassRecordKeepsAMissingDistanceToShoreMissing(t *testing.T) {
	plan, outcome, _ := endOfRun(t)
	pass, err := passRecord(plan, outcome, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range pass.Slices[0].Detections {
		if d.ID == "slice_1_target_020" && d.DistanceToShoreM != nil {
			t.Errorf("unmeasured distance became %q; it must be NULL, not a number", *d.DistanceToShoreM)
		}
	}
}

func TestPassRecordWithoutAPictureHasNoRaster(t *testing.T) {
	plan, outcome, _ := endOfRun(t)
	pass, err := passRecord(plan, outcome, nil)
	if err != nil {
		t.Fatal(err)
	}
	if pass.Raster != nil {
		t.Error("no sidecar should mean no raster_layer row")
	}
}

func TestPassRecordLeavesUnknownCoverageNull(t *testing.T) {
	plan, outcome, _ := endOfRun(t)
	plan.CoveragePercent = coverageUnknown
	pass, err := passRecord(plan, outcome, nil)
	if err != nil {
		t.Fatal(err)
	}
	if pass.CoveragePercent != nil {
		t.Errorf("coverage = %d, want NULL", *pass.CoveragePercent)
	}
}

func TestPassRecordRefusesWhatItCannotStore(t *testing.T) {
	cases := []struct {
		name   string
		change func(*PassPlan, *mergeOutcome)
		want   string
	}{
		{"no orbit", func(plan *PassPlan, _ *mergeOutcome) {
			for _, slice := range plan.Slices {
				slice.AbsoluteOrbit = ""
			}
		}, "no absolute orbit"},
		{"coverage that is not a number", func(plan *PassPlan, _ *mergeOutcome) {
			plan.CoveragePercent = math.NaN()
		}, "not a whole number"},
		{"a detection from a slice the plan never had", func(_ *PassPlan, outcome *mergeOutcome) {
			outcome.Kept[0].SourceSlice = "slice_9"
		}, "not in the plan"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			plan, outcome, _ := endOfRun(t)
			c.change(plan, &outcome)
			_, err := passRecord(plan, outcome, nil)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want one mentioning %q", err, c.want)
			}
		})
	}
}

func TestTheRecordIsStoredInOneTransaction(t *testing.T) {
	plan, outcome, sidecar := endOfRun(t)
	pass, err := passRecord(plan, outcome, &sidecar)
	if err != nil {
		t.Fatal(err)
	}

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id FROM pass_run`).
		WithArgs("bulgaria", time.Date(2026, 9, 26, 15, 51, 30, 0, time.UTC)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(`INSERT INTO pass_run`).
		WithArgs(sqlmock.AnyArg(), 4751, "bulgaria", sqlmock.AnyArg(), 2, 1, 26, 22, 1, 3, len(BulgarianWatersAOI)).
		WillReturnResult(sqlmock.NewResult(41, 1))
	sliceID := int64(100)
	for _, slice := range pass.Slices {
		sliceID++
		mock.ExpectExec(`INSERT INTO pass_slice`).
			WithArgs(int64(41), slice.Index, slice.ProductName, sqlmock.AnyArg(), "S1D", sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(sliceID, 1))
		for _, detection := range slice.Detections {
			mock.ExpectExec(`INSERT INTO detection`).
				WithArgs(sliceID, detection.ID, detection.Latitude, detection.Longitude,
					"210.0", "290.0", "12214", "8064", sqlmock.AnyArg()).
				WillReturnResult(sqlmock.NewResult(1, 1))
		}
	}
	mock.ExpectExec(`INSERT INTO raster_layer`).
		WithArgs(int64(41), "tiles", sidecar.File, 42.34, 28.83, 44.25, 32.42, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	written, err := resultsdb.SavePass(db, pass, resultsdb.ReplaceExisting)
	if err != nil || !written {
		t.Fatalf("SavePass = %v, %v", written, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestSavingWithoutADatabaseSaysWhy(t *testing.T) {
	dir := t.TempDir()
	noPassword := filepath.Join(dir, "mysql.env")
	writeText(t, noPassword, "SOMETHING_ELSE=1\n")

	plan, outcome, _ := endOfRun(t)
	config := newDefaultConfig()

	for _, envFile := range []string{noPassword, filepath.Join(dir, "absent.env")} {
		config.MySQLEnvFile = envFile
		if err := saveRunToDatabase(&config, plan, outcome, nil); err == nil {
			t.Errorf("saving with %s succeeded", filepath.Base(envFile))
		}
		err := checkResultsDatabaseIsReady(&config)
		if err == nil || !strings.Contains(err.Error(), "docker compose up -d") {
			t.Errorf("readiness with %s = %v, want the hint to start MySQL", filepath.Base(envFile), err)
		}
	}

	for _, slice := range plan.Slices {
		slice.AbsoluteOrbit = ""
	}
	if err := saveRunToDatabase(&config, plan, outcome, nil); err == nil ||
		!strings.Contains(err.Error(), "absolute orbit") {
		t.Errorf("err = %v, want the orbit complaint", err)
	}
}

func TestAISWarningNeverStopsTheRun(t *testing.T) {
	config := newDefaultConfig()
	config.MySQLEnvFile = filepath.Join(t.TempDir(), "absent.env")

	quiet := captureStdout(t, func() {
		warnIfAISWillBeMissing(&config, &PassPlan{Slices: []*SliceJob{{Index: 1}}})
	})
	if quiet != "" {
		t.Errorf("a pass with no acquisition time printed %q", quiet)
	}

	plan, _, _ := endOfRun(t)
	said := captureStdout(t, func() { warnIfAISWillBeMissing(&config, plan) })
	if !strings.Contains(said, "cannot ask the database") {
		t.Errorf("printed %q, want a note that the database could not be asked", said)
	}
}
