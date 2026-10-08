// ToSecond: converts a time to UTC and truncates it to the whole second.
// Pass.Validate: checks the pass has a start and slices and that its detection counts add up.
// SavePass: writes the pass inside one transaction, committing only if it was written.
// WritePass: inserts a validated pass with its slices, detections and raster, skipping or replacing a stored one.
// deletePass: deletes a stored pass with its detections, slices and raster layer.
// CheckSchema: returns an error unless the Flyway schema version is at least RequiredSchemaVersion.

package resultsdb

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type Pass struct {
	Start             time.Time
	Orbit             int
	Area              string
	CoveragePercent   *int
	SlicesProcessed   int
	SlicesFailed      int
	RawCFAR           int
	DroppedOutsideAOI int
	SeamDuplicates    int
	Written           int
	AOIVertices       int
	Slices            []Slice
	Raster            *Raster
}

type Slice struct {
	Index       int
	ProductName string
	ProductID   string
	Mission     string
	Start       time.Time
	Detections  []Detection
}

type Detection struct {
	ID               string
	Latitude         string
	Longitude        string
	WidthM           string
	LengthM          string
	PixelX           string
	PixelY           string
	DistanceToShoreM *string
}

type Raster struct {
	Type          string
	File          string
	South         float64
	West          float64
	North         float64
	East          float64
	MaxNativeZoom *int
	MaxZoom       *int
}

type OnExisting int

const (
	SkipExisting OnExisting = iota
	ReplaceExisting
)

func ToSecond(moment time.Time) time.Time {
	return moment.UTC().Truncate(time.Second)
}

func (p Pass) Validate() error {
	if p.Start.IsZero() {
		return errors.New("pass has no start time")
	}
	if len(p.Slices) == 0 {
		return errors.New("pass has no slices")
	}
	total := 0
	for _, s := range p.Slices {
		total += len(s.Detections)
	}
	if total != p.Written {
		return fmt.Errorf("%d detections, but the pass says %d were written", total, p.Written)
	}
	if p.RawCFAR != p.DroppedOutsideAOI+p.SeamDuplicates+p.Written {
		return fmt.Errorf("raw %d is not outside %d + seam %d + written %d",
			p.RawCFAR, p.DroppedOutsideAOI, p.SeamDuplicates, p.Written)
	}
	return nil
}

func SavePass(db *sql.DB, pass Pass, onExisting OnExisting) (bool, error) {
	tx, err := db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	written, err := WritePass(tx, pass, onExisting)
	if err != nil || !written {
		return written, err
	}
	return true, tx.Commit()
}

func WritePass(tx *sql.Tx, pass Pass, onExisting OnExisting) (bool, error) {
	if err := pass.Validate(); err != nil {
		return false, err
	}
	start := ToSecond(pass.Start)

	var existing int64
	err := tx.QueryRow("SELECT id FROM pass_run WHERE area = ? AND pass_start = ? FOR UPDATE",
		pass.Area, start).Scan(&existing)
	switch {
	case err == nil && onExisting == SkipExisting:
		return false, nil
	case err == nil:
		if err := deletePass(tx, existing); err != nil {
			return false, fmt.Errorf("replacing the stored pass: %w", err)
		}
	case !errors.Is(err, sql.ErrNoRows):
		return false, err
	}

	result, err := tx.Exec(`INSERT INTO pass_run (pass_start, absolute_orbit, area, coverage_pct,
			slices_processed, slices_failed, cfar_detections_raw, dropped_outside_aoi,
			dropped_seam_duplicates, detections_written, aoi_vertices)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		start, pass.Orbit, pass.Area, pass.CoveragePercent, pass.SlicesProcessed, pass.SlicesFailed,
		pass.RawCFAR, pass.DroppedOutsideAOI, pass.SeamDuplicates, pass.Written, pass.AOIVertices)
	if err != nil {
		return false, err
	}
	passID, err := result.LastInsertId()
	if err != nil {
		return false, err
	}

	for _, s := range pass.Slices {
		productID := sql.NullString{String: s.ProductID, Valid: s.ProductID != ""}
		result, err := tx.Exec(`INSERT INTO pass_slice (pass_run_id, slice_index, product_name,
				product_id, mission, acquisition_start)
			VALUES (?, ?, ?, ?, ?, ?)`, passID, s.Index, s.ProductName, productID, s.Mission, ToSecond(s.Start))
		if err != nil {
			return false, fmt.Errorf("slice_%d: %w", s.Index, err)
		}
		sliceID, err := result.LastInsertId()
		if err != nil {
			return false, err
		}
		for _, d := range s.Detections {
			if _, err := tx.Exec(`INSERT INTO detection (pass_slice_id, detection_id, latitude, longitude,
					width_m, length_m, pixel_x, pixel_y, distance_to_shore_m)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				sliceID, d.ID, d.Latitude, d.Longitude, d.WidthM, d.LengthM, d.PixelX, d.PixelY,
				d.DistanceToShoreM); err != nil {
				return false, fmt.Errorf("detection %s: %w", d.ID, err)
			}
		}
	}

	if r := pass.Raster; r != nil {
		if _, err := tx.Exec(`INSERT INTO raster_layer (pass_run_id, type, file, bound_south, bound_west,
				bound_north, bound_east, max_native_zoom, max_zoom)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			passID, r.Type, r.File, r.South, r.West, r.North, r.East, r.MaxNativeZoom, r.MaxZoom); err != nil {
			return false, fmt.Errorf("raster: %w", err)
		}
	}
	return true, nil
}

func deletePass(tx *sql.Tx, passID int64) error {
	statements := []string{
		`DELETE d FROM detection d JOIN pass_slice s ON s.id = d.pass_slice_id WHERE s.pass_run_id = ?`,
		`DELETE FROM pass_slice WHERE pass_run_id = ?`,
		`DELETE FROM raster_layer WHERE pass_run_id = ?`,
		`DELETE FROM pass_run WHERE id = ?`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement, passID); err != nil {
			return err
		}
	}
	return nil
}

const RequiredSchemaVersion = 7

func CheckSchema(db *sql.DB) error {
	var version sql.NullInt64
	err := db.QueryRow(`SELECT MAX(CAST(version AS UNSIGNED)) FROM flyway_schema_history WHERE success = 1`).
		Scan(&version)
	if err != nil {
		return fmt.Errorf("reading the schema version: %w (has the viewer ever been started?)", err)
	}
	if !version.Valid || version.Int64 < RequiredSchemaVersion {
		return fmt.Errorf("the database schema is at version %d; this needs %d. "+
			"Start the viewer once so Flyway can migrate it", version.Int64, RequiredSchemaVersion)
	}
	return nil
}
