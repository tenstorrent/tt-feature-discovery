// Fabric-manager-derived label generation.
//
// Everything here is a direct projection of the structured HostSummary that
// tt-fabric-manager returns from QueryTopologySummary. The fabric manager owns
// FSD interpretation; this file only renames fields and makes the values legal
// Kubernetes label values.
//
// Label value handling:
//   - Empty string fields are skipped, so a partially-populated record still
//     yields whatever subset is well formed.
//   - Values are lowercased, then any character outside [a-z0-9-._] becomes
//     '-', with runs collapsed and non-alphanumeric ends trimmed.
//   - Values are truncated to the 63-character Kubernetes limit.

package labels

import (
	"strconv"
	"strings"

	"github.com/tenstorrent/tt-feature-discovery/internal/fabric"
	orchpb "github.com/tenstorrent/tt-feature-discovery/internal/fabric/orchestratorpb"
)

// Label keys derived from the fabric manager's host summary.
const (
	LabelDatacenterHall   = "tenstorrent.com/datacenter.hall"
	LabelDatacenterAisle  = "tenstorrent.com/datacenter.aisle"
	LabelDatacenterRack   = "tenstorrent.com/datacenter.rack"
	LabelDatacenterShelfU = "tenstorrent.com/datacenter.shelf-u"

	LabelASICCount = "tenstorrent.com/asic.count"
	LabelASICArch  = "tenstorrent.com/asic.arch"

	LabelFabricConnectedHosts = "tenstorrent.com/fabric.connected-hosts"
	LabelFabricGroup          = "tenstorrent.com/fabric.group"
	LabelFabricGroupSize      = "tenstorrent.com/fabric.group-size"
	LabelFabricPod            = "tenstorrent.com/fabric.pod"
	LabelFabricPodSize        = "tenstorrent.com/fabric.pod-size"
	LabelFabricSource         = "tenstorrent.com/fabric.source"
)

// FromFabricSummary projects a fabric-manager host summary onto node labels.
// It never returns an error — unset or malformed fields are skipped.
func FromFabricSummary(s *fabric.Summary) map[string]string {
	out := map[string]string{}
	if s == nil || s.Host == nil {
		return out
	}

	put := func(key, raw string) {
		if v := sanitizeLabelValue(strings.ToLower(raw)); v != "" {
			out[key] = v
		}
	}

	// Location metadata is only populated when the fabric manager has an FSD
	// loaded, so these drop out cleanly on clusters without one.
	md := s.Host.GetMetadata()
	put(LabelDatacenterHall, md.GetHall())
	put(LabelDatacenterAisle, md.GetAisle())
	put(LabelDatacenterRack, md.GetRack())
	put(LabelDatacenterShelfU, md.GetShelfU())

	if n := s.Host.GetAsicCount(); n > 0 {
		out[LabelASICCount] = strconv.Itoa(int(n))
	}
	put(LabelASICArch, s.Host.GetArch())

	if hosts := s.Host.GetConnectedHosts(); len(hosts) > 0 {
		out[LabelFabricConnectedHosts] = strconv.Itoa(len(hosts))
	}
	// Group and pod names come from the FSD cabling hierarchy, so they carry
	// '/' separators and occasionally a " (2)" disambiguating suffix; the
	// sanitizer folds both into '-'. A host in a group the fabric manager
	// could not name still gets the size.
	put(LabelFabricGroup, s.Group.Name)
	if s.Group.Size > 0 {
		out[LabelFabricGroupSize] = strconv.Itoa(s.Group.Size)
	}
	put(LabelFabricPod, s.Pod.Name)
	if s.Pod.Size > 0 {
		out[LabelFabricPodSize] = strconv.Itoa(s.Pod.Size)
	}

	put(LabelFabricSource, sourceName(s.Host.GetSource()))

	return out
}

// sourceName renders which view the fabric manager actually had for this host:
// "psd" (live discovery only), "fsd" (factory descriptor only), or both.
func sourceName(src orchpb.TopologySource) string {
	switch src {
	case orchpb.TopologySource_PSD:
		return "psd"
	case orchpb.TopologySource_FSD:
		return "fsd"
	case orchpb.TopologySource_COMBINED_PSD_FSD:
		return "psd-fsd"
	default:
		return ""
	}
}

// sanitizeLabelValue normalizes s into Kubernetes' label value grammar:
// (([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])?, up to 63 characters.
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
	v := strings.TrimFunc(b.String(), func(r rune) bool { return !isAlphanum(r) })
	if len(v) > 63 {
		v = strings.TrimRightFunc(v[:63], func(r rune) bool { return !isAlphanum(r) })
	}
	return v
}

func isLabelValueChar(r rune) bool {
	return isAlphanum(r) || r == '-' || r == '_' || r == '.'
}

func isAlphanum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z')
}
