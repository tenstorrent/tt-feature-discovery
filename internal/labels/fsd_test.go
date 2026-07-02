package labels

import (
	"reflect"
	"strings"
	"testing"

	fsdpb "github.com/tenstorrent/tt-feature-discovery/internal/fabric/fsdpb"
)

func TestFromFSDHost_FullyPopulated(t *testing.T) {
	fsd := &fsdpb.FactorySystemDescriptor{
		Hosts: []*fsdpb.FactorySystemDescriptor_Host{
			{Hostname: "e01cs01", Hall: "H1", Aisle: "A2", Rack: 3, ShelfU: 42, Motherboard: "Supermicro-H12DSG"},
			{Hostname: "e01cs02", Hall: "H1", Aisle: "A2", Rack: 3, ShelfU: 44, Motherboard: "Supermicro-H12DSG"},
		},
		EthConnections: &fsdpb.FactorySystemDescriptor_EthConnections{
			Connection: make([]*fsdpb.FactorySystemDescriptor_Connection, 7),
		},
	}
	got := FromFSDHost(fsd, fsd.Hosts[1], 1)
	want := map[string]string{
		LabelDatacenterHall:        "h1",
		LabelDatacenterAisle:       "a2",
		LabelDatacenterRack:        "3",
		LabelDatacenterShelfU:      "44",
		LabelDatacenterMotherboard: "supermicro-h12dsg",
		LabelHostID:                "1",
		LabelSystemsHostCount:      "2",
		LabelSystemsEthConnCount:   "7",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("labels mismatch\n got:  %v\n want: %v", got, want)
	}
}

func TestFromFSDHost_EmptyStringFieldsSkipped(t *testing.T) {
	fsd := &fsdpb.FactorySystemDescriptor{
		Hosts: []*fsdpb.FactorySystemDescriptor_Host{{
			Hostname:    "n1",
			Hall:        "", // skipped
			Aisle:       "A1",
			Motherboard: "", // skipped
			Rack:        1,
			ShelfU:      2,
		}},
	}
	got := FromFSDHost(fsd, fsd.Hosts[0], 0)
	if _, ok := got[LabelDatacenterHall]; ok {
		t.Errorf("expected hall label to be absent, got %q", got[LabelDatacenterHall])
	}
	if _, ok := got[LabelDatacenterMotherboard]; ok {
		t.Errorf("expected motherboard label to be absent")
	}
	if got[LabelDatacenterAisle] != "a1" {
		t.Errorf("aisle = %q, want a1", got[LabelDatacenterAisle])
	}
	if got[LabelDatacenterRack] != "1" {
		t.Errorf("rack = %q, want 1", got[LabelDatacenterRack])
	}
	if got[LabelHostID] != "0" {
		t.Errorf("host-id = %q, want 0", got[LabelHostID])
	}
}

func TestFromFSDHost_Lowercases(t *testing.T) {
	h := &fsdpb.FactorySystemDescriptor_Host{
		Hostname: "N1", Hall: "OM", Aisle: "B", Motherboard: "SupermicroH12",
	}
	got := FromFSDHost(nil, h, 0)
	if got[LabelDatacenterHall] != "om" {
		t.Errorf("hall = %q, want om", got[LabelDatacenterHall])
	}
	if got[LabelDatacenterMotherboard] != "supermicroh12" {
		t.Errorf("motherboard = %q, want supermicroh12", got[LabelDatacenterMotherboard])
	}
}

func TestFromFSDHost_NilHost(t *testing.T) {
	got := FromFSDHost(nil, nil, -1)
	if len(got) != 0 {
		t.Errorf("nil host produced labels: %v", got)
	}
}

func TestFromFSDHost_NoFSDStillEmitsHostLabels(t *testing.T) {
	h := &fsdpb.FactorySystemDescriptor_Host{Hostname: "n1", Hall: "h1", Aisle: "a", Rack: 5, ShelfU: 10}
	got := FromFSDHost(nil, h, 3)
	if got[LabelHostID] != "3" {
		t.Errorf("host-id = %q, want 3", got[LabelHostID])
	}
	if _, ok := got[LabelSystemsHostCount]; ok {
		t.Errorf("systems.host-count should be absent when fsd is nil")
	}
	if _, ok := got[LabelSystemsEthConnCount]; ok {
		t.Errorf("systems.eth-connection-count should be absent when fsd is nil")
	}
}

func TestSanitizeLabelValue(t *testing.T) {
	// sanitizeLabelValue only enforces Kubernetes' character grammar and
	// length; lowercasing is done at the caller (FromFSDHost). So these
	// inputs are all already lowercased.
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"om", "om"},
		{"aisle a", "aisle-a"},
		{"  spaces  ", "spaces"},
		{"multi   space", "multi-space"},
		{"under_score.dot-dash", "under_score.dot-dash"},
		{"!!!leading", "leading"},
		{"x!!!trailing.", "x-trailing"},
		{"!!!!", ""},
		// 70-char run of 'a' followed by junk — output must be <=63 chars
		// and must not end in a non-alphanumeric.
		{strings.Repeat("a", 70), strings.Repeat("a", 63)},
	}
	for _, tc := range cases {
		got := sanitizeLabelValue(tc.in)
		if got != tc.want {
			t.Errorf("sanitize(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if len(got) > 63 {
			t.Errorf("sanitize(%q) produced %d chars, over 63-char limit", tc.in, len(got))
		}
	}
}
