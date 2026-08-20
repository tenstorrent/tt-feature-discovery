package labels

import (
	"testing"

	"github.com/tenstorrent/tt-feature-discovery/internal/detect"
)

func TestFromAcceleratorsHomogeneous(t *testing.T) {
	got := FromAccelerators(detect.Accelerators{
		Count:    8,
		Families: []string{"wormhole"},
		Products: []string{"n150"},
	})
	want := map[string]string{
		LabelAcceleratorCount:   "8",
		LabelAcceleratorFamily:  "wormhole",
		LabelAcceleratorProduct: "n150",
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

// A mixed host gets "mixed" plus the full set, so a selector for one family
// does not match it but an operator can still see what is installed.
func TestFromAcceleratorsMixedFamilies(t *testing.T) {
	got := FromAccelerators(detect.Accelerators{
		Count:    2,
		Families: []string{"blackhole", "wormhole"},
		Products: []string{"n150", "p150"},
	})
	if got[LabelAcceleratorFamily] != Mixed {
		t.Errorf("family = %q, want %q", got[LabelAcceleratorFamily], Mixed)
	}
	if got[LabelAcceleratorFamilies] != "blackhole.wormhole" {
		t.Errorf("families = %q", got[LabelAcceleratorFamilies])
	}
	if got[LabelAcceleratorProduct] != Mixed {
		t.Errorf("product = %q, want %q", got[LabelAcceleratorProduct], Mixed)
	}
	if got[LabelAcceleratorProducts] != "n150.p150" {
		t.Errorf("products = %q", got[LabelAcceleratorProducts])
	}
	if got[LabelAcceleratorCount] != "2" {
		t.Errorf("count = %q, want 2", got[LabelAcceleratorCount])
	}
}

// One family, two boards: family stays scalar, only product goes mixed.
func TestFromAcceleratorsMixedProductsOnly(t *testing.T) {
	got := FromAccelerators(detect.Accelerators{
		Count:    2,
		Families: []string{"wormhole"},
		Products: []string{"n150", "n300"},
	})
	if got[LabelAcceleratorFamily] != "wormhole" {
		t.Errorf("family = %q, want wormhole", got[LabelAcceleratorFamily])
	}
	if _, ok := got[LabelAcceleratorFamilies]; ok {
		t.Error("families set for a single-family host")
	}
	if got[LabelAcceleratorProduct] != Mixed {
		t.Errorf("product = %q, want mixed", got[LabelAcceleratorProduct])
	}
	if got[LabelAcceleratorProducts] != "n150.n300" {
		t.Errorf("products = %q", got[LabelAcceleratorProducts])
	}
}

// The set labels only appear when there is more than one value.
func TestFromAcceleratorsNoSetLabelsWhenHomogeneous(t *testing.T) {
	got := FromAccelerators(detect.Accelerators{
		Count: 1, Families: []string{"wormhole"}, Products: []string{"n150"},
	})
	for _, k := range []string{LabelAcceleratorFamilies, LabelAcceleratorProducts} {
		if v, ok := got[k]; ok {
			t.Errorf("%s unexpectedly set to %q", k, v)
		}
	}
}

func TestFromAcceleratorsNoDevices(t *testing.T) {
	if got := FromAccelerators(detect.Accelerators{}); len(got) != 0 {
		t.Errorf("no devices produced %v", got)
	}
}

// "unknown" is published rather than dropped: a node with hardware we cannot
// name is worth surfacing, and it must not match a family selector.
func TestFromAcceleratorsUnclassified(t *testing.T) {
	got := FromAccelerators(detect.Accelerators{
		Count:    1,
		Families: []string{detect.Unclassified},
		Products: []string{detect.Unclassified},
	})
	if got[LabelAcceleratorFamily] != "unknown" {
		t.Errorf("family = %q, want unknown", got[LabelAcceleratorFamily])
	}
	if got[LabelAcceleratorCount] != "1" {
		t.Errorf("count = %q, want 1", got[LabelAcceleratorCount])
	}
}
