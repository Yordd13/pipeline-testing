// detectionRunForSlice: picks the detection graph run for a slice, deriving a mask-free graph if it is all water.
// sliceIsAllWater: reports whether a slice's preprocessed extent touches no coastline, returning false when unsure.
// writeMaskFreeGraph: copies a detection graph without its land-mask nodes, rewiring their consumers.
// editableGraph.withoutOperators: returns the graph without the named operators, re-pointing sources past them.
// resolveThrough: follows a chain of removed node IDs to the first node that survives.
// editableGraph.render: writes the graph back out as SNAP graph XML, keeping parameters verbatim.
// escapeXML: returns a string with its XML special characters escaped.

package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const maskFreeGraphFileName = "ShipDetection_nomask.xml"

var maskFreeOperators = map[string]bool{
	"Import-Vector": true,
	"Land-Sea-Mask": true,
}

func detectionRunForSlice(config *Config, slice *SliceJob) (GraphRun, error) {
	if !sliceIsAllWater(config, slice) {
		return detectionRun(config), nil
	}

	derivedPath := filepath.Join(slice.RunSubDir, maskFreeGraphFileName)
	if err := writeMaskFreeGraph(config.DetectionGraphFilePath(), derivedPath); err != nil {
		return GraphRun{}, err
	}

	return GraphRun{
		Description:        "Stage 2 of 2: ship detection, unmasked",
		GraphHostPath:      derivedPath,
		GraphContainerPath: containerOutputDir + "/" + maskFreeGraphFileName,
		InputContainerPath: containerOutputDir + "/" + config.ResultFileName,
		OutputFileName:     config.DetectionResultFileName,
	}, nil
}

func sliceIsAllWater(config *Config, slice *SliceJob) bool {
	shore, err := shorelineForGraph(config)
	if err != nil {
		fmt.Printf("Note: keeping the land mask; the coastline could not be read: %v\n", err)
		return false
	}
	if shore == nil {
		return false
	}

	band, err := openRasterBand(slice.PreprocessedPath, rasterBandName)
	if err != nil {
		fmt.Printf("Note: keeping the land mask; %s's extent is unreadable: %v\n",
			slice.SliceName(), err)
		return false
	}

	box := band.bounds()
	if shore.touchesBounds(box) {
		return false
	}

	fmt.Printf("\nNo coast in %s: lat %.3f..%.3f, lon %.3f..%.3f is open sea.\n",
		slice.SliceName(), box.South, box.North, box.West, box.East)
	fmt.Println("Running the detection graph without its land mask, which would" +
		" otherwise fail\nlooking for a coastline SNAP had nothing to import.")
	return true
}

func writeMaskFreeGraph(sourcePath, destinationPath string) error {
	contents, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", filepath.Base(sourcePath), err)
	}

	var graph editableGraph
	if err := xml.Unmarshal(contents, &graph); err != nil {
		return fmt.Errorf("%s is not readable as XML: %w", filepath.Base(sourcePath), err)
	}

	kept, err := graph.withoutOperators(maskFreeOperators)
	if err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(sourcePath), err)
	}

	if err := os.WriteFile(destinationPath, []byte(kept.render()), 0o644); err != nil {
		return fmt.Errorf("cannot write %s: %w", filepath.Base(destinationPath), err)
	}
	return nil
}

type editableGraph struct {
	XMLName xml.Name       `xml:"graph"`
	ID      string         `xml:"id,attr"`
	Version string         `xml:"version"`
	Nodes   []editableNode `xml:"node"`
}

type editableNode struct {
	ID       string `xml:"id,attr"`
	Operator string `xml:"operator"`
	Sources  struct {
		Products []editableSource `xml:"sourceProduct"`
	} `xml:"sources"`
	Parameters *editableParameters `xml:"parameters"`
}

type editableSource struct {
	RefID string `xml:"refid,attr"`
}

type editableParameters struct {
	Class string `xml:"class,attr"`
	Inner string `xml:",innerxml"`
}

func (g editableGraph) withoutOperators(unwanted map[string]bool) (editableGraph, error) {
	replacement := map[string]string{}
	for _, node := range g.Nodes {
		if !unwanted[node.Operator] {
			continue
		}
		if len(node.Sources.Products) != 1 {
			return editableGraph{}, fmt.Errorf(
				"node %q has %d sources, and only a node with exactly one can be "+
					"taken out without changing what the graph computes",
				node.ID, len(node.Sources.Products))
		}
		replacement[node.ID] = node.Sources.Products[0].RefID
	}
	if len(replacement) == 0 {
		return editableGraph{}, fmt.Errorf("holds none of the nodes to remove")
	}

	kept := editableGraph{XMLName: g.XMLName, ID: g.ID, Version: g.Version}
	for _, node := range g.Nodes {
		if unwanted[node.Operator] {
			continue
		}
		for at, source := range node.Sources.Products {
			node.Sources.Products[at].RefID = resolveThrough(replacement, source.RefID)
		}
		kept.Nodes = append(kept.Nodes, node)
	}
	return kept, nil
}

func resolveThrough(replacement map[string]string, refID string) string {
	for step := 0; step < len(replacement)+1; step++ {
		next, removed := replacement[refID]
		if !removed {
			return refID
		}
		refID = next
	}
	return refID
}

func (g editableGraph) render() string {
	out := &strings.Builder{}
	out.WriteString("<!-- Derived from the detection graph by the pipeline: this slice\n" +
		"     holds no coast, so the land mask has been taken out. Not kept;\n" +
		"     rewritten on every run that needs it. -->\n")
	fmt.Fprintf(out, "<graph id=\"%s\">\n", escapeXML(g.ID))
	fmt.Fprintf(out, "  <version>%s</version>\n", escapeXML(g.Version))

	for _, node := range g.Nodes {
		fmt.Fprintf(out, "  <node id=\"%s\">\n", escapeXML(node.ID))
		fmt.Fprintf(out, "    <operator>%s</operator>\n", escapeXML(node.Operator))

		if len(node.Sources.Products) == 0 {
			out.WriteString("    <sources/>\n")
		} else {
			out.WriteString("    <sources>\n")
			for _, source := range node.Sources.Products {
				fmt.Fprintf(out, "      <sourceProduct refid=\"%s\"/>\n",
					escapeXML(source.RefID))
			}
			out.WriteString("    </sources>\n")
		}

		if node.Parameters != nil {
			fmt.Fprintf(out, "    <parameters class=\"%s\">%s</parameters>\n",
				escapeXML(node.Parameters.Class), node.Parameters.Inner)
		}
		out.WriteString("  </node>\n")
	}

	out.WriteString("</graph>\n")
	return out.String()
}

func escapeXML(value string) string {
	escaped := &bytes.Buffer{}
	xml.EscapeText(escaped, []byte(value))
	return escaped.String()
}
