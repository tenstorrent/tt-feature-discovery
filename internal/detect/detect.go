// Package detect reports the presence of Tenstorrent NPU devices on the host
// by scanning a PCI sysfs directory for the Tenstorrent vendor ID.
package detect

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// TenstorrentVendorID is the PCI vendor ID assigned to Tenstorrent.
// See https://pci-ids.ucw.cz/read/PC/1e52.
const TenstorrentVendorID = "0x1e52"

// DefaultSysBusPCIDir is the canonical Linux PCI sysfs root.
const DefaultSysBusPCIDir = "/sys/bus/pci/devices"

// NPUPresent returns true if at least one PCI device under sysBusPCIDir
// advertises the Tenstorrent vendor ID.
//
// A non-existent sysBusPCIDir is treated as "no devices" rather than an error,
// so callers running off a Linux host (e.g. macOS dev, or a node without PCI)
// simply report not-present.
func NPUPresent(sysBusPCIDir string) (bool, error) {
	entries, err := os.ReadDir(sysBusPCIDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("read pci sysfs %q: %w", sysBusPCIDir, err)
	}
	for _, e := range entries {
		vendor, err := readVendor(filepath.Join(sysBusPCIDir, e.Name()))
		if err != nil {
			// A single unreadable device shouldn't fail the whole scan; PCI
			// sysfs entries occasionally disappear during rescans.
			continue
		}
		if vendor == TenstorrentVendorID {
			return true, nil
		}
	}
	return false, nil
}

func readVendor(deviceDir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(deviceDir, "vendor"))
	if err != nil {
		return "", err
	}
	return strings.ToLower(strings.TrimSpace(string(b))), nil
}
