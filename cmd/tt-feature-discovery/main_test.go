package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tenstorrent/tt-feature-discovery/internal/fabric"
	orchpb "github.com/tenstorrent/tt-feature-discovery/internal/fabric/orchestratorpb"
)

type stubFabric struct {
	summary *fabric.Summary
	err     error
}

func (s *stubFabric) HostSummary(context.Context, string) (*fabric.Summary, error) {
	return s.summary, s.err
}

// testConfig builds a config over a temp sysfs containing one Tenstorrent
// device and a temp feature file.
func testConfig(t *testing.T) *config {
	t.Helper()
	root := t.TempDir()
	pci := filepath.Join(root, "pci")
	dev := filepath.Join(pci, "0000:01:00.0")
	// Owner-only: the test reads these fixtures back itself, so nothing needs
	// group or world access.
	if err := os.MkdirAll(dev, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dev, "vendor"), []byte("0x1e52\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &config{
		sysBusPCIDir: pci,
		featureFile:  filepath.Join(root, "features.d", "tt-fd"),
		nodeName:     "e01cs01",
	}
}

func readFeatureFile(t *testing.T, path string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read feature file: %v", err)
	}
	out := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line == "" {
			continue
		}
		k, v, _ := strings.Cut(line, "=")
		out[k] = v
	}
	return out
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRunOnceWithoutFabricClient(t *testing.T) {
	cfg := testConfig(t)
	if err := runOnce(context.Background(), quietLogger(), cfg, nil); err != nil {
		t.Fatalf("runOnce: %v", err)
	}
	got := readFeatureFile(t, cfg.featureFile)
	if len(got) != 1 || got[labelDevicePresent] != "true" {
		t.Errorf("labels = %v, want only device.present", got)
	}
}

func TestRunOnceMergesFabricLabels(t *testing.T) {
	cfg := testConfig(t)
	fc := &stubFabric{summary: &fabric.Summary{
		Host: &orchpb.HostSummary{
			HostName:  "e01cs01",
			AsicCount: 8,
			Arch:      "wormhole_b0",
			Source:    orchpb.TopologySource_COMBINED_PSD_FSD,
			Metadata:  &orchpb.HostMetadata{Hall: "AUS2", Rack: "12"},
		},
		Group: fabric.Grouping{Name: "110-SC36_4x4_C1-10", Size: 3},
		Pod:   fabric.Grouping{Name: "bh_galaxy_sp_0", Size: 2},
	}}
	if err := runOnce(context.Background(), quietLogger(), cfg, fc); err != nil {
		t.Fatalf("runOnce: %v", err)
	}
	got := readFeatureFile(t, cfg.featureFile)
	for k, want := range map[string]string{
		labelDevicePresent:                  "true",
		"tenstorrent.com/asic.count":        "8",
		"tenstorrent.com/asic.arch":         "wormhole_b0",
		"tenstorrent.com/datacenter.hall":   "aus2",
		"tenstorrent.com/datacenter.rack":   "12",
		"tenstorrent.com/fabric.group":      "110-sc36_4x4_c1-10",
		"tenstorrent.com/fabric.group-size": "3",
		"tenstorrent.com/fabric.pod":        "bh_galaxy_sp_0",
		"tenstorrent.com/fabric.pod-size":   "2",
		"tenstorrent.com/fabric.source":     "psd-fsd",
	} {
		if got[k] != want {
			t.Errorf("%s = %q, want %q", k, got[k], want)
		}
	}
}

// A fabric-manager outage must not fail the pass or drop device.present.
func TestRunOnceSurvivesFabricError(t *testing.T) {
	cfg := testConfig(t)
	fc := &stubFabric{err: errors.New("connection refused")}
	if err := runOnce(context.Background(), quietLogger(), cfg, fc); err != nil {
		t.Fatalf("runOnce: %v", err)
	}
	got := readFeatureFile(t, cfg.featureFile)
	if got[labelDevicePresent] != "true" {
		t.Errorf("device.present missing: %v", got)
	}
	if len(got) != 1 {
		t.Errorf("unexpected fabric labels on error: %v", got)
	}
}

func TestRunOnceHostNotKnownToFabric(t *testing.T) {
	cfg := testConfig(t)
	fc := &stubFabric{err: fabric.ErrHostNotFound}
	if err := runOnce(context.Background(), quietLogger(), cfg, fc); err != nil {
		t.Fatalf("runOnce: %v", err)
	}
	got := readFeatureFile(t, cfg.featureFile)
	if len(got) != 1 || got[labelDevicePresent] != "true" {
		t.Errorf("labels = %v, want only device.present", got)
	}
}

func TestParseFlagsDefaultsNodeNameToHostname(t *testing.T) {
	t.Setenv("NODE_NAME", "")
	cfg, err := parseFlags(nil, os.Stderr)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	hostname, _ := os.Hostname()
	if cfg.nodeName != hostname {
		t.Errorf("node name = %q, want %q", cfg.nodeName, hostname)
	}
}

func TestParseFlagsReadsEnv(t *testing.T) {
	t.Setenv("NODE_NAME", "e01cs07")
	t.Setenv("TT_FABRIC_MANAGER_ADDRESS", "fm.tt.svc:50051")
	cfg, err := parseFlags(nil, os.Stderr)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if cfg.nodeName != "e01cs07" {
		t.Errorf("node name = %q", cfg.nodeName)
	}
	if cfg.fabricAddress != "fm.tt.svc:50051" {
		t.Errorf("fabric address = %q", cfg.fabricAddress)
	}
}

// Explicit flags win over the environment.
func TestParseFlagsFlagOverridesEnv(t *testing.T) {
	t.Setenv("NODE_NAME", "from-env")
	cfg, err := parseFlags([]string{"--node-name=from-flag"}, os.Stderr)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if cfg.nodeName != "from-flag" {
		t.Errorf("node name = %q, want from-flag", cfg.nodeName)
	}
}
