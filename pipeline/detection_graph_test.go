// rewriteToMaskFree: writes the given graph XML to disk, derives its mask-free version and parses the result.
// TestMaskFreeGraphDropsTheMaskingNodes: checks the Import-Vector and Land-Sea-Mask nodes are removed and the rest kept.
// TestMaskFreeGraphRewiresPastTheRemovedNodes: checks a node that read the mask now reads Read, through both removed nodes.
// TestMaskFreeGraphCarriesParametersAcrossVerbatim: checks the thresholding parameters and their class survive unchanged.
// TestMaskFreeGraphRefusesAGraphWithNoMask: checks deriving a mask-free graph from one with no masking nodes is an error.
// TestTheRealDetectionGraphCanLoseItsMask: checks the repo's ShipDetection.xml loses its mask with every source still defined.
// TestWriteMaskFreeGraphReportsWhatStoppedIt: checks a missing source, invalid XML or unwritable destination is named in the error.
// TestResolveThroughStopsOnACycle: checks resolving a source through removed nodes terminates on a cycle.
// TestRenderEscapesWhatItWrites: checks rendering a graph escapes XML special characters in ids and version.

package main

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const graphWithAMask = `<graph id="Graph">
  <version>1.0</version>
  <node id="Read">
    <operator>Read</operator>
    <sources/>
    <parameters class="com.bc.ceres.binding.dom.XppDomElement">
      <file>${input}</file>
    </parameters>
  </node>
  <node id="Import-Vector">
    <operator>Import-Vector</operator>
    <sources>
      <sourceProduct refid="Read"/>
    </sources>
    <parameters class="com.bc.ceres.binding.dom.XppDomElement">
      <vectorFile>/graphs/bg_coast.shp</vectorFile>
    </parameters>
  </node>
  <node id="Land-Sea-Mask">
    <operator>Land-Sea-Mask</operator>
    <sources>
      <sourceProduct refid="Import-Vector"/>
    </sources>
    <parameters class="com.bc.ceres.binding.dom.XppDomElement">
      <geometry>bg_coast</geometry>
    </parameters>
  </node>
  <node id="AdaptiveThresholding">
    <operator>AdaptiveThresholding</operator>
    <sources>
      <sourceProduct refid="Land-Sea-Mask"/>
    </sources>
    <parameters class="com.bc.ceres.binding.dom.XppDomElement">
      <pfa>12.5</pfa>
      <estimateBackground>false</estimateBackground>
    </parameters>
  </node>
  <applicationData id="Presentation">
    <node id="Land-Sea-Mask">
      <displayPosition x="107.0" y="93.0"/>
    </node>
  </applicationData>
</graph>`

func rewriteToMaskFree(t *testing.T, contents string) editableGraph {
	t.Helper()

	source := filepath.Join(t.TempDir(), "detect.xml")
	if err := os.WriteFile(source, []byte(contents), 0o644); err != nil {
		t.Fatalf("cannot write the graph to rewrite: %v", err)
	}
	destination := filepath.Join(t.TempDir(), maskFreeGraphFileName)
	if err := writeMaskFreeGraph(source, destination); err != nil {
		t.Fatalf("writeMaskFreeGraph: %v", err)
	}

	written, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("cannot read what was written: %v", err)
	}

	var graph editableGraph
	if err := xml.Unmarshal(written, &graph); err != nil {
		t.Fatalf("the derived graph is not valid XML: %v\n%s", err, written)
	}
	return graph
}

func TestMaskFreeGraphDropsTheMaskingNodes(t *testing.T) {
	graph := rewriteToMaskFree(t, graphWithAMask)

	for _, node := range graph.Nodes {
		if maskFreeOperators[node.Operator] {
			t.Errorf("%s survived the rewrite", node.Operator)
		}
	}
	if len(graph.Nodes) != 2 {
		t.Fatalf("kept %d nodes, want the 2 that are not masking", len(graph.Nodes))
	}
}

func TestMaskFreeGraphRewiresPastTheRemovedNodes(t *testing.T) {
	graph := rewriteToMaskFree(t, graphWithAMask)

	for _, node := range graph.Nodes {
		if node.ID != "AdaptiveThresholding" {
			continue
		}
		if len(node.Sources.Products) != 1 {
			t.Fatalf("AdaptiveThresholding has %d sources, want 1",
				len(node.Sources.Products))
		}
		if got := node.Sources.Products[0].RefID; got != "Read" {
			t.Errorf("AdaptiveThresholding now reads %q, want Read — the chain "+
				"through both removed nodes was not followed", got)
		}
		return
	}
	t.Fatal("AdaptiveThresholding is missing from the derived graph")
}

func TestMaskFreeGraphCarriesParametersAcrossVerbatim(t *testing.T) {
	graph := rewriteToMaskFree(t, graphWithAMask)

	for _, node := range graph.Nodes {
		if node.ID != "AdaptiveThresholding" {
			continue
		}
		if node.Parameters == nil {
			t.Fatal("AdaptiveThresholding lost its parameters")
		}
		for _, want := range []string{"<pfa>12.5</pfa>", "<estimateBackground>false</estimateBackground>"} {
			if !strings.Contains(node.Parameters.Inner, want) {
				t.Errorf("parameters lost %s; got %q", want, node.Parameters.Inner)
			}
		}
		if node.Parameters.Class != "com.bc.ceres.binding.dom.XppDomElement" {
			t.Errorf("parameters class is %q, want the one it came with",
				node.Parameters.Class)
		}
		return
	}
	t.Fatal("AdaptiveThresholding is missing from the derived graph")
}

func TestMaskFreeGraphRefusesAGraphWithNoMask(t *testing.T) {
	unmasked := strings.NewReplacer(
		"<operator>Import-Vector</operator>", "<operator>Speckle-Filter</operator>",
		"<operator>Land-Sea-Mask</operator>", "<operator>Multilook</operator>",
	).Replace(graphWithAMask)

	source := filepath.Join(t.TempDir(), "detect.xml")
	if err := os.WriteFile(source, []byte(unmasked), 0o644); err != nil {
		t.Fatalf("cannot write the graph: %v", err)
	}
	err := writeMaskFreeGraph(source, filepath.Join(t.TempDir(), maskFreeGraphFileName))
	if err == nil {
		t.Fatal("rewriting a graph with no masking nodes succeeded, want a refusal")
	}
}

func TestTheRealDetectionGraphCanLoseItsMask(t *testing.T) {
	real := filepath.Join("..", "graphs", "ShipDetection.xml")
	contents, err := os.ReadFile(real)
	if err != nil {
		t.Skipf("no detection graph to check: %v", err)
	}

	graph := rewriteToMaskFree(t, string(contents))

	defined := map[string]bool{}
	for _, node := range graph.Nodes {
		if maskFreeOperators[node.Operator] {
			t.Errorf("%s survived the rewrite", node.Operator)
		}
		defined[node.ID] = true
	}

	for _, node := range graph.Nodes {
		for _, source := range node.Sources.Products {
			if !defined[source.RefID] {
				t.Errorf("%s reads %q, which is not a node in the derived graph",
					node.ID, source.RefID)
			}
		}
	}

	for _, needed := range []string{"Read", "AdaptiveThresholding", "Object-Discrimination", "Write"} {
		if !defined[needed] {
			t.Errorf("the derived graph has no %s node", needed)
		}
	}
}

func TestWriteMaskFreeGraphReportsWhatStoppedIt(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "detect.xml")
	writeText(t, good, graphWithAMask)
	notXML := filepath.Join(dir, "broken.xml")
	writeText(t, notXML, "<graph><node>")

	cases := []struct {
		name, source, destination, want string
	}{
		{"missing source", filepath.Join(dir, "absent.xml"), filepath.Join(dir, "out.xml"), "cannot read"},
		{"not XML", notXML, filepath.Join(dir, "out.xml"), "not readable as XML"},
		{"nowhere to write", good, filepath.Join(dir, "absent", "out.xml"), "cannot write"},
	}
	for _, c := range cases {
		if err := writeMaskFreeGraph(c.source, c.destination); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
}

func TestResolveThroughStopsOnACycle(t *testing.T) {
	cycle := map[string]string{"a": "b", "b": "a"}
	if got := resolveThrough(cycle, "a"); got != "a" && got != "b" {
		t.Errorf("resolveThrough = %q", got)
	}
}

func TestRenderEscapesWhatItWrites(t *testing.T) {
	graph := editableGraph{ID: `a"b`, Version: "1&2", Nodes: []editableNode{{ID: "<x>", Operator: "Read"}}}
	rendered := graph.render()
	for _, want := range []string{`<graph id="a&#34;b">`, "<version>1&amp;2</version>", `<node id="&lt;x&gt;">`, "<sources/>"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("render lacks %s:\n%s", want, rendered)
		}
	}
}
