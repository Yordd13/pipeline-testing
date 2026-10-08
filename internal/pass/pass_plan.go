// SliceJob.SliceName: returns the slice's folder name, such as "slice_1".
// SliceJob.Succeeded: reports whether the slice has no failure and produced a detection product.
// PassPlan.AbsoluteOrbit: returns the first non-empty absolute orbit among the plan's slices.
// PassPlan.PassStart: returns the earliest acquisition time among the plan's slices.
// PassPlan.SucceededSlices: returns the slices that succeeded.
// PassPlan.FailedSlices: returns the slices that did not succeed.
// PassPlan.AllSucceeded: reports whether the plan has slices and none of them failed.
// PassPlan.CoverageText: formats the coverage percentage as a whole number, or "" when unknown.
// newSliceJobFromCatalogue: builds a SliceJob from a catalogue scene plus its local scene and archive paths.
// newSliceJobFromFile: builds a SliceJob for a scene on disk, deriving its provenance from the file name.
// AcquisitionTimeFromProductName: parses the acquisition start time from a Sentinel-1 product name, or zero.
// TrimProductExtensions: strips a trailing .zip and then .SAFE from a product name.

package pass

import (
	"fmt"
	"strings"
	"time"

	"radarpipeline/internal/cdse"
)

const CoverageUnknown = -1.0

type SliceJob struct {
	Index int

	SceneFileName string
	ArchivePath   string

	ProductID     string
	ProductName   string
	Mission       string
	AbsoluteOrbit string
	AcquiredAt    time.Time

	RunSubDir            string
	PreprocessedPath     string
	DetectionProductPath string
	Failure              error
}

func (s *SliceJob) SliceName() string {
	return fmt.Sprintf("slice_%d", s.Index)
}

func (s *SliceJob) Succeeded() bool {
	return s.Failure == nil && s.DetectionProductPath != ""
}

type PassPlan struct {
	AreaName        string
	Slices          []*SliceJob
	CoveragePercent float64
	RunDir          string
}

func (p *PassPlan) AbsoluteOrbit() string {
	for _, slice := range p.Slices {
		if slice.AbsoluteOrbit != "" {
			return slice.AbsoluteOrbit
		}
	}
	return ""
}

func (p *PassPlan) PassStart() time.Time {
	earliest := time.Time{}
	for _, slice := range p.Slices {
		if slice.AcquiredAt.IsZero() {
			continue
		}
		if earliest.IsZero() || slice.AcquiredAt.Before(earliest) {
			earliest = slice.AcquiredAt
		}
	}
	return earliest
}

func (p *PassPlan) SucceededSlices() []*SliceJob {
	done := make([]*SliceJob, 0, len(p.Slices))
	for _, slice := range p.Slices {
		if slice.Succeeded() {
			done = append(done, slice)
		}
	}
	return done
}

func (p *PassPlan) FailedSlices() []*SliceJob {
	failed := make([]*SliceJob, 0)
	for _, slice := range p.Slices {
		if !slice.Succeeded() {
			failed = append(failed, slice)
		}
	}
	return failed
}

func (p *PassPlan) AllSucceeded() bool {
	return len(p.FailedSlices()) == 0 && len(p.Slices) > 0
}

func (p *PassPlan) CoverageText() string {
	if p.CoveragePercent < 0 {
		return ""
	}
	return fmt.Sprintf("%.0f", p.CoveragePercent)
}

func newSliceJobFromCatalogue(index int, scene cdse.CatalogueScene, sceneFileName, archivePath string) *SliceJob {
	return &SliceJob{
		Index:         index,
		SceneFileName: sceneFileName,
		ArchivePath:   archivePath,
		ProductID:     scene.ProductID,
		ProductName:   scene.ProductName,
		Mission:       scene.MissionPrefix,
		AbsoluteOrbit: scene.AbsoluteOrbit,
		AcquiredAt:    scene.AcquiredAt,
	}
}

func newSliceJobFromFile(index int, sceneFileName string) *SliceJob {
	return &SliceJob{
		Index:         index,
		SceneFileName: sceneFileName,
		ProductName:   sceneFileName,
		Mission:       cdse.MissionPrefixFromProductName(sceneFileName),
		AbsoluteOrbit: cdse.AbsoluteOrbitFromProductName(sceneFileName),
		AcquiredAt:    AcquisitionTimeFromProductName(sceneFileName),
	}
}

func AcquisitionTimeFromProductName(productName string) time.Time {
	fields := strings.Split(TrimProductExtensions(productName), "_")
	const startField = 4
	if len(fields) <= startField {
		return time.Time{}
	}
	acquiredAt, err := time.Parse("20060102T150405", fields[startField])
	if err != nil {
		return time.Time{}
	}
	return acquiredAt.UTC()
}

func TrimProductExtensions(name string) string {
	name = strings.TrimSuffix(name, ".zip")
	return strings.TrimSuffix(name, ".SAFE")
}
