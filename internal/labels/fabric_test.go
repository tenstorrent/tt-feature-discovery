package labels

import (
	"testing"

	"github.com/tenstorrent/tt-feature-discovery/internal/fabric"
	orchpb "github.com/tenstorrent/tt-feature-discovery/internal/fabric/orchestratorpb"
)

func TestFromFabricSummaryFullRecord(t *testing.T) {
	got := FromFabricSummary(&fabric.Summary{
		Host: &orchpb.HostSummary{
			HostName:       "e01cs01",
			AsicCount:      8,
			Arch:           "wormhole_b0",
			ConnectedHosts: []string{"e01cs02", "e01cs03"},
			Source:         orchpb.TopologySource_COMBINED_PSD_FSD,
			Metadata: &orchpb.HostMetadata{
				Hall: "AUS2", Aisle: "A1", Rack: "12", ShelfU: "34",
			},
		},
		Group: fabric.Grouping{Name: "110-SC36_4x4_C1-10", Size: 3},
		Pod:   fabric.Grouping{Name: "bh_galaxy_sp_0", Size: 2},
	})

	want := map[string]string{
		LabelDatacenterHall:       "aus2",
		LabelDatacenterAisle:      "a1",
		LabelDatacenterRack:       "12",
		LabelDatacenterShelfU:     "34",
		LabelASICCount:            "8",
		LabelASICArch:             "wormhole_b0",
		LabelFabricConnectedHosts: "2",
		LabelFabricGroup:          "110-sc36_4x4_c1-10",
		LabelFabricGroupSize:      "3",
		LabelFabricPod:            "bh_galaxy_sp_0",
		LabelFabricPodSize:        "2",
		LabelFabricSource:         "psd-fsd",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d labels, want %d: %v", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

// A cluster with no FSD loaded gets PSD-only records: no location metadata.
func TestFromFabricSummaryPSDOnly(t *testing.T) {
	got := FromFabricSummary(&fabric.Summary{
		Host: &orchpb.HostSummary{
			HostName:  "n150-01",
			AsicCount: 1,
			Arch:      "wormhole_b0",
			Source:    orchpb.TopologySource_PSD,
		},
	})

	for _, k := range []string{
		LabelDatacenterHall, LabelDatacenterAisle,
		LabelDatacenterRack, LabelDatacenterShelfU,
		LabelFabricConnectedHosts, LabelFabricGroup, LabelFabricGroupSize,
		LabelFabricPod, LabelFabricPodSize,
	} {
		if v, ok := got[k]; ok {
			t.Errorf("%s unexpectedly set to %q", k, v)
		}
	}
	if got[LabelASICCount] != "1" {
		t.Errorf("asic count = %q, want 1", got[LabelASICCount])
	}
	if got[LabelFabricSource] != "psd" {
		t.Errorf("source = %q, want psd", got[LabelFabricSource])
	}
}

func TestFromFabricSummaryNilSafe(t *testing.T) {
	if got := FromFabricSummary(nil); len(got) != 0 {
		t.Errorf("nil summary produced %v", got)
	}
	if got := FromFabricSummary(&fabric.Summary{}); len(got) != 0 {
		t.Errorf("empty summary produced %v", got)
	}
}

func TestSanitizeLabelValue(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"aus2", "aus2"},
		{"wormhole_b0", "wormhole_b0"},
		{"hall a/1", "hall-a-1"},
		{"  spaced  ", "spaced"},
		{"a//////b", "a-b"},
		{"---", ""},
		{"rack.12", "rack.12"},
	}
	for _, c := range cases {
		if got := sanitizeLabelValue(c.in); got != c.want {
			t.Errorf("sanitizeLabelValue(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSanitizeLabelValueTruncates(t *testing.T) {
	long := ""
	for i := 0; i < 100; i++ {
		long += "a"
	}
	got := sanitizeLabelValue(long)
	if len(got) != 63 {
		t.Errorf("len = %d, want 63", len(got))
	}
}

// Truncation must not leave a trailing non-alphanumeric character, which
// Kubernetes rejects.
func TestSanitizeLabelValueTruncatesCleanly(t *testing.T) {
	in := ""
	for i := 0; i < 62; i++ {
		in += "a"
	}
	in += "-tail"
	got := sanitizeLabelValue(in)
	if len(got) != 62 {
		t.Errorf("len = %d, want 62 (trailing dash trimmed): %q", len(got), got)
	}
}

// Group names come out of the FSD cabling hierarchy, so they carry characters
// Kubernetes label values reject: '/' separators and a " (2)" suffix the
// fabric manager adds when two groups derive the same name.
func TestFromFabricSummaryGroupNameSanitized(t *testing.T) {
	got := FromFabricSummary(&fabric.Summary{
		Host:  &orchpb.HostSummary{HostName: "e01cs01"},
		Group: fabric.Grouping{Name: "110-SC36_4x4_C1-10/rack3 (2)", Size: 4},
		Pod:   fabric.Grouping{Name: "bh_galaxy_sp_0/tray1", Size: 2},
	})
	if got[LabelFabricGroup] != "110-sc36_4x4_c1-10-rack3-2" {
		t.Errorf("group = %q", got[LabelFabricGroup])
	}
	if got[LabelFabricPod] != "bh_galaxy_sp_0-tray1" {
		t.Errorf("pod = %q", got[LabelFabricPod])
	}
}

// A group the fabric manager could not name still yields a size.
func TestFromFabricSummaryUnnamedGroup(t *testing.T) {
	got := FromFabricSummary(&fabric.Summary{
		Host:  &orchpb.HostSummary{HostName: "n150-01"},
		Group: fabric.Grouping{Size: 1},
	})
	if _, ok := got[LabelFabricGroup]; ok {
		t.Errorf("unnamed group produced %q", got[LabelFabricGroup])
	}
	if got[LabelFabricGroupSize] != "1" {
		t.Errorf("group size = %q, want 1", got[LabelFabricGroupSize])
	}
}
