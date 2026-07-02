// FSD-derived label generation.
//
// Given a host record from a FactorySystemDescriptor plus its positional
// index and the containing FSD, this produces the tenstorrent.com/*
// datacenter-location labels that node-feature-discovery will pick up
// through the local feature-file source.
//
// Label value semantics:
//   - Empty string fields are skipped (no `hall=` with an empty value).
//   - String values are lowercased, since Kubernetes label values are
//     already effectively case-sensitive and we want deterministic output
//     even if the FSD is authored with mixed case.
//   - Any character outside [a-z0-9-._] is replaced with '-'. Runs of
//     invalid characters collapse into a single '-'. Leading/trailing
//     non-alphanumeric characters are trimmed.
//   - Values are truncated to 63 characters (Kubernetes label limit).
//   - Integer fields are converted with strconv.Itoa.

package labels

import (
	"strconv"
	"strings"

	fsdpb "github.com/tenstorrent/tt-feature-discovery/internal/fabric/fsdpb"
)

// Label keys emitted by FromFSDHost. Exported so callers (and tests) can
// reference them without stringly-typed duplication.
const (
	LabelDatacenterHall        = "tenstorrent.com/datacenter.hall"
	LabelDatacenterAisle       = "tenstorrent.com/datacenter.aisle"
	LabelDatacenterRack        = "tenstorrent.com/datacenter.rack"
	LabelDatacenterShelfU      = "tenstorrent.com/datacenter.shelf-u"
	LabelDatacenterMotherboard = "tenstorrent.com/datacenter.motherboard"
	LabelHostID                = "tenstorrent.com/host-id"
	LabelSystemsHostCount      = "tenstorrent.com/systems.host-count"
	LabelSystemsEthConnCount   = "tenstorrent.com/systems.eth-connection-count"
)

// FromFSDHost returns the label set derived from the FSD host record at
// hostIndex within fsd. It never returns an error — malformed / empty
// fields are silently skipped so a partially-populated FSD still produces
// whatever subset of labels is well-formed.
func FromFSDHost(fsd *fsdpb.FactorySystemDescriptor, host *fsdpb.FactorySystemDescriptor_Host, hostIndex int) map[string]string {
	out := map[string]string{}
	if host == nil {
		return out
	}

	putStr := func(key, raw string) {
		v := sanitizeLabelValue(strings.ToLower(raw))
		if v == "" {
			return
		}
		out[key] = v
	}
	putStr(LabelDatacenterHall, host.GetHall())
	putStr(LabelDatacenterAisle, host.GetAisle())
	putStr(LabelDatacenterMotherboard, host.GetMotherboard())

	// Integer fields — zero is a valid rack / shelf-u position, so emit
	// them unconditionally. The FSD would omit the whole host entry
	// rather than leave a rack field literally unset.
	out[LabelDatacenterRack] = strconv.FormatUint(uint64(host.GetRack()), 10)
	out[LabelDatacenterShelfU] = strconv.FormatUint(uint64(host.GetShelfU()), 10)

	if hostIndex >= 0 {
		out[LabelHostID] = strconv.Itoa(hostIndex)
	}

	if fsd != nil {
		out[LabelSystemsHostCount] = strconv.Itoa(len(fsd.GetHosts()))
		if eth := fsd.GetEthConnections(); eth != nil {
			out[LabelSystemsEthConnCount] = strconv.Itoa(len(eth.GetConnection()))
		}
	}
	return out
}

// sanitizeLabelValue normalizes s into something that fits Kubernetes'
// label value grammar: (([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])?, up to
// 63 characters. Input is expected to already be lowercased.
func sanitizeLabelValue(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	prevDash := false
	for _, r := range s {
		if isLabelValueChar(r) {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	v := b.String()
	// Kubernetes label values must start and end with an alphanumeric.
	v = strings.TrimFunc(v, func(r rune) bool { return !isAlphanum(r) })
	if len(v) > 63 {
		v = v[:63]
		v = strings.TrimRightFunc(v, func(r rune) bool { return !isAlphanum(r) })
	}
	return v
}

func isLabelValueChar(r rune) bool {
	return isAlphanum(r) || r == '-' || r == '_' || r == '.'
}

func isAlphanum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z')
}
