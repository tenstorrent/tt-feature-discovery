// Accelerator classification from PCI sysfs.
//
// Two sysfs attributes are enough to say what a Tenstorrent card is:
//
//   - `device` is the PCI device ID and identifies the ASIC generation
//     (0x401e Wormhole, 0xb140 Blackhole), matching UMD's own mapping in
//     device/pcie/pci_device.cpp.
//   - `subsystem_device` carries the board's UPI code, the same value UMD
//     looks up in board_upi_map to name a board (0x0018 N150, 0x0035 the
//     Wormhole Galaxy UBB, and so on). UMD reads it straight off PCI to
//     recognise a Wormhole UBB, so it is a first-class identifier rather
//     than a coincidence.
//
// Reading these two files keeps classification local: it needs no ioctl, no
// UMD, no driver and no fabric manager, so a node is classifiable before
// anything Tenstorrent-specific is installed on it.

package detect

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Accelerator families, keyed by PCI device ID.
var familyByDeviceID = map[string]string{
	"0x401e": "wormhole",
	"0xb140": "blackhole",
}

// Board products, keyed by the UPI code in subsystem_device. Mirrors
// board_upi_map in tt-umd (device/api/umd/device/types/cluster_descriptor_types.hpp).
var productBySubsystemID = map[string]string{
	"0x0018": "n150",
	"0x0014": "n300",
	"0x000b": "galaxy",
	"0x0035": "ubb",
	"0x0036": "p100",
	"0x0043": "p100",
	"0x0040": "p150",
	"0x0041": "p150",
	"0x0042": "p150",
	"0x0044": "p300",
	"0x0045": "p300",
	"0x0046": "p300",
	"0x0047": "ubb_blackhole",
}

// Unclassified is the value used when a device is Tenstorrent's but its IDs
// are not recognised — a newer card than this build knows about. It is
// deliberately not left empty: "there is hardware here we cannot name" is
// worth surfacing on the node, and it keeps a `family=wormhole` selector from
// matching something that might not be one.
const Unclassified = "unknown"

// Device is one Tenstorrent PCI endpoint.
type Device struct {
	BDF     string
	Family  string
	Product string
}

// Accelerators summarizes the Tenstorrent PCI endpoints on a host.
type Accelerators struct {
	// Count is the number of Tenstorrent PCI endpoints. This counts what is
	// addressable on the bus, which is not always the ASIC count: on boards
	// where only one ASIC is PCIe-attached (N300) or where many sit behind
	// fewer endpoints (Galaxy/UBB), the fabric manager's asic.count is
	// higher.
	Count int
	// Families and Products hold the distinct values seen, sorted. A
	// single-element slice means a homogeneous host.
	Families []string
	Products []string
	Devices  []Device
}

// Mixed reports whether the host has more than one family or product, i.e.
// whether a single node-level family/product label can describe it.
func (a Accelerators) Mixed() bool {
	return len(a.Families) > 1 || len(a.Products) > 1
}

// Scan enumerates the Tenstorrent PCI endpoints under sysBusPCIDir.
//
// As with DevicePresent, a missing sysBusPCIDir means "no devices" rather than
// an error, and an individual unreadable device is skipped: PCI sysfs entries
// come and go during a bus rescan.
func Scan(sysBusPCIDir string) (Accelerators, error) {
	var out Accelerators

	entries, err := os.ReadDir(sysBusPCIDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return out, nil
		}
		return out, fmt.Errorf("read pci sysfs %q: %w", sysBusPCIDir, err)
	}

	families := map[string]struct{}{}
	products := map[string]struct{}{}
	for _, e := range entries {
		dir := filepath.Join(sysBusPCIDir, e.Name())
		vendor, err := readVendor(dir)
		if err != nil || vendor != TenstorrentVendorID {
			continue
		}
		d := Device{
			BDF:     e.Name(),
			Family:  classify(dir, "device", familyByDeviceID),
			Product: classify(dir, "subsystem_device", productBySubsystemID),
		}
		out.Count++
		out.Devices = append(out.Devices, d)
		families[d.Family] = struct{}{}
		products[d.Product] = struct{}{}
	}

	out.Families = sortedKeys(families)
	out.Products = sortedKeys(products)
	return out, nil
}

// classify reads a hex-valued sysfs attribute and maps it through table,
// yielding Unclassified for a missing file or an unrecognised value.
func classify(deviceDir, attr string, table map[string]string) string {
	raw, err := readHexAttr(deviceDir, attr)
	if err != nil {
		return Unclassified
	}
	if v, ok := table[raw]; ok {
		return v
	}
	return Unclassified
}

// readHexAttr normalizes a sysfs hex attribute to a lowercase 0x-prefixed
// value zero-padded to four digits, so "0x18" and "0x0018" compare equal.
func readHexAttr(deviceDir, attr string) (string, error) {
	b, err := os.ReadFile(filepath.Join(deviceDir, attr))
	if err != nil {
		return "", err
	}
	s := strings.ToLower(strings.TrimSpace(string(b)))
	s = strings.TrimPrefix(s, "0x")
	if s == "" {
		return "", fmt.Errorf("empty %s", attr)
	}
	for len(s) < 4 {
		s = "0" + s
	}
	return "0x" + s, nil
}

func sortedKeys(m map[string]struct{}) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
