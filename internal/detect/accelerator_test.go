package detect

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// writeCard lays out one PCI device directory the way sysfs presents it.
// Empty attribute values are skipped, so a caller can model a card whose
// subsystem_device is missing.
func writeCard(t *testing.T, root, bdf string, attrs map[string]string) {
	t.Helper()
	dir := filepath.Join(root, bdf)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, value := range attrs {
		if value == "" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func n150(t *testing.T, root, bdf string) {
	writeCard(t, root, bdf, map[string]string{
		"vendor": TenstorrentVendorID, "device": "0x401e", "subsystem_device": "0x0018",
	})
}

// Eight N150s, matching a node on the aus2-dev2 dev cluster.
func TestScanHomogeneousHost(t *testing.T) {
	root := t.TempDir()
	for _, bdf := range []string{
		"0000:01:00.0", "0000:25:00.0", "0000:41:00.0", "0000:61:00.0",
		"0000:81:00.0", "0000:a1:00.0", "0000:c1:00.0", "0000:e1:00.0",
	} {
		n150(t, root, bdf)
	}
	// A non-Tenstorrent device must not be counted.
	writeCard(t, root, "0000:00:1f.0", map[string]string{"vendor": "0x8086", "device": "0x1234"})

	got, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if got.Count != 8 {
		t.Errorf("count = %d, want 8", got.Count)
	}
	if !reflect.DeepEqual(got.Families, []string{"wormhole"}) {
		t.Errorf("families = %v", got.Families)
	}
	if !reflect.DeepEqual(got.Products, []string{"n150"}) {
		t.Errorf("products = %v", got.Products)
	}
	if got.Mixed() {
		t.Error("homogeneous host reported as mixed")
	}
}

func TestScanBlackhole(t *testing.T) {
	root := t.TempDir()
	writeCard(t, root, "0000:01:00.0", map[string]string{
		"vendor": TenstorrentVendorID, "device": "0xb140", "subsystem_device": "0x0040",
	})
	got, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !reflect.DeepEqual(got.Families, []string{"blackhole"}) || !reflect.DeepEqual(got.Products, []string{"p150"}) {
		t.Errorf("families=%v products=%v", got.Families, got.Products)
	}
}

// sysfs prints these values unpadded on some kernels ("0x18", not "0x0018").
func TestScanAcceptsUnpaddedHex(t *testing.T) {
	root := t.TempDir()
	writeCard(t, root, "0000:01:00.0", map[string]string{
		"vendor": TenstorrentVendorID, "device": "0x401e", "subsystem_device": "0x18",
	})
	got, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !reflect.DeepEqual(got.Products, []string{"n150"}) {
		t.Errorf("products = %v, want n150", got.Products)
	}
}

func TestScanMixedHost(t *testing.T) {
	root := t.TempDir()
	n150(t, root, "0000:01:00.0")
	writeCard(t, root, "0000:02:00.0", map[string]string{
		"vendor": TenstorrentVendorID, "device": "0xb140", "subsystem_device": "0x0040",
	})

	got, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if got.Count != 2 {
		t.Errorf("count = %d, want 2", got.Count)
	}
	if !reflect.DeepEqual(got.Families, []string{"blackhole", "wormhole"}) {
		t.Errorf("families = %v, want sorted [blackhole wormhole]", got.Families)
	}
	if !got.Mixed() {
		t.Error("mixed host not reported as mixed")
	}
}

// Same family, different boards, is still mixed: a product label cannot
// describe the host.
func TestScanMixedProductsSameFamily(t *testing.T) {
	root := t.TempDir()
	n150(t, root, "0000:01:00.0")
	writeCard(t, root, "0000:02:00.0", map[string]string{
		"vendor": TenstorrentVendorID, "device": "0x401e", "subsystem_device": "0x0014",
	})
	got, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !reflect.DeepEqual(got.Families, []string{"wormhole"}) {
		t.Errorf("families = %v", got.Families)
	}
	if !reflect.DeepEqual(got.Products, []string{"n150", "n300"}) {
		t.Errorf("products = %v", got.Products)
	}
	if !got.Mixed() {
		t.Error("differing products should be mixed")
	}
}

// A Tenstorrent card this build does not know is still counted, and reported
// as unclassified rather than silently dropped or mislabelled.
func TestScanUnknownIDs(t *testing.T) {
	root := t.TempDir()
	writeCard(t, root, "0000:01:00.0", map[string]string{
		"vendor": TenstorrentVendorID, "device": "0xffff", "subsystem_device": "0xeeee",
	})
	got, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if got.Count != 1 {
		t.Errorf("count = %d, want 1", got.Count)
	}
	if !reflect.DeepEqual(got.Families, []string{Unclassified}) {
		t.Errorf("families = %v, want [%s]", got.Families, Unclassified)
	}
}

// A card with no subsystem_device at all still classifies by family.
func TestScanMissingSubsystemAttr(t *testing.T) {
	root := t.TempDir()
	writeCard(t, root, "0000:01:00.0", map[string]string{
		"vendor": TenstorrentVendorID, "device": "0x401e",
	})
	got, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if !reflect.DeepEqual(got.Families, []string{"wormhole"}) {
		t.Errorf("families = %v", got.Families)
	}
	if !reflect.DeepEqual(got.Products, []string{Unclassified}) {
		t.Errorf("products = %v", got.Products)
	}
}

func TestScanNoDevices(t *testing.T) {
	got, err := Scan(t.TempDir())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if got.Count != 0 || got.Families != nil || got.Mixed() {
		t.Errorf("empty host produced %+v", got)
	}
}

// Consistent with DevicePresent: no PCI sysfs is "no devices", not an error.
func TestScanMissingSysfs(t *testing.T) {
	got, err := Scan(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if got.Count != 0 {
		t.Errorf("count = %d, want 0", got.Count)
	}
}
