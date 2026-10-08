// readLandSeaMaskSettings: parses a SNAP graph file and returns how its Land-Sea-Mask node is configured, if present.
// isXMLTrue: reports whether an XML parameter value is "true", ignoring case and surrounding space.

package main

import (
	"encoding/xml"
	"fmt"
	"os"
	"strings"
)

type graphDocument struct {
	Nodes []graphNode `xml:"node"`
}

type graphNode struct {
	ID         string          `xml:"id,attr"`
	Operator   string          `xml:"operator"`
	Parameters graphParameters `xml:"parameters"`
}

type graphParameters struct {
	UseSRTM        string `xml:"useSRTM"`
	Geometry       string `xml:"geometry"`
	InvertGeometry string `xml:"invertGeometry"`
}

type landSeaMaskSettings struct {
	Present        bool
	UseSRTM        bool
	Geometry       string
	InvertGeometry bool
}

func readLandSeaMaskSettings(graphFilePath string) (landSeaMaskSettings, error) {
	contents, err := os.ReadFile(graphFilePath)
	if err != nil {
		return landSeaMaskSettings{}, fmt.Errorf("cannot read %s: %w", graphFilePath, err)
	}

	var document graphDocument
	if err := xml.Unmarshal(contents, &document); err != nil {
		return landSeaMaskSettings{}, fmt.Errorf("%s is not readable as XML: %w", graphFilePath, err)
	}

	for _, node := range document.Nodes {
		if node.Operator != "Land-Sea-Mask" {
			continue
		}
		return landSeaMaskSettings{
			Present:        true,
			UseSRTM:        isXMLTrue(node.Parameters.UseSRTM),
			Geometry:       strings.TrimSpace(node.Parameters.Geometry),
			InvertGeometry: isXMLTrue(node.Parameters.InvertGeometry),
		}, nil
	}
	return landSeaMaskSettings{}, nil
}

func isXMLTrue(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "true")
}
