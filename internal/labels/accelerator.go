// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 Tenstorrent USA, Inc.

// Accelerator label generation.
//
// Mixed hosts: Tenstorrent systems are homogeneous in practice, but a node
// with two card types cannot be described by one family or product value.
// Rather than pick a winner — which would let a `family=wormhole` selector
// place a Wormhole-only workload on a node that is half Blackhole — the
// scalar label becomes "mixed" and the full sorted set is published
// alongside it in `accelerator.families` / `accelerator.products`.
//
// That makes the safe case the default: a selector for a specific family
// simply does not match a mixed node, and anything wanting per-device
// placement should be going through DRA, which resolves devices
// individually, rather than through node labels.

package labels

import (
	"strconv"
	"strings"

	"github.com/tenstorrent/tt-feature-discovery/internal/detect"
)

// Label keys derived from the host's PCI accelerators.
const (
	LabelAcceleratorCount    = "tenstorrent.com/accelerator.count"
	LabelAcceleratorFamily   = "tenstorrent.com/accelerator.family"
	LabelAcceleratorProduct  = "tenstorrent.com/accelerator.product"
	LabelAcceleratorFamilies = "tenstorrent.com/accelerator.families"
	LabelAcceleratorProducts = "tenstorrent.com/accelerator.products"
)

// Mixed is the family/product value for a host holding more than one kind.
const Mixed = "mixed"

// FromAccelerators projects a PCI scan onto node labels. A host with no
// Tenstorrent devices yields no labels.
func FromAccelerators(a detect.Accelerators) map[string]string {
	out := map[string]string{}
	if a.Count == 0 {
		return out
	}

	out[LabelAcceleratorCount] = strconv.Itoa(a.Count)
	putSet(out, LabelAcceleratorFamily, LabelAcceleratorFamilies, a.Families)
	putSet(out, LabelAcceleratorProduct, LabelAcceleratorProducts, a.Products)
	return out
}

// putSet publishes a set of values as a scalar label, degrading to "mixed"
// plus a joined list label when the host holds more than one value.
func putSet(out map[string]string, scalarKey, setKey string, values []string) {
	switch len(values) {
	case 0:
		return
	case 1:
		out[scalarKey] = sanitizeLabelValue(values[0])
	default:
		out[scalarKey] = Mixed
		// '.' rather than ',' — a comma is not a legal label value
		// character. Values arrive sorted, so this is stable.
		out[setKey] = sanitizeLabelValue(strings.Join(values, "."))
	}
}
