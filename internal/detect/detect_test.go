// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 Tenstorrent USA, Inc.

package detect

import (
	"os"
	"path/filepath"
	"testing"
)

func writeDevice(t *testing.T, root, addr, vendor string) {
	t.Helper()
	dir := filepath.Join(root, addr)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vendor"), []byte(vendor+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDevicePresent(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(t *testing.T, root string)
		want    bool
		wantErr bool
	}{
		{
			name:  "missing sysfs returns false without error",
			setup: func(*testing.T, string) {},
			want:  false,
		},
		{
			name: "no tenstorrent device",
			setup: func(t *testing.T, root string) {
				writeDevice(t, root, "0000:00:1f.0", "0x8086") // Intel
				writeDevice(t, root, "0000:01:00.0", "0x10de") // NVIDIA
			},
			want: false,
		},
		{
			name: "single tenstorrent device",
			setup: func(t *testing.T, root string) {
				writeDevice(t, root, "0000:01:00.0", "0x1e52")
			},
			want: true,
		},
		{
			name: "mixed devices",
			setup: func(t *testing.T, root string) {
				writeDevice(t, root, "0000:00:1f.0", "0x8086")
				writeDevice(t, root, "0000:01:00.0", "0x1e52")
				writeDevice(t, root, "0000:02:00.0", "0x10de")
			},
			want: true,
		},
		{
			name: "vendor file with whitespace and uppercase",
			setup: func(t *testing.T, root string) {
				dir := filepath.Join(root, "0000:01:00.0")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "vendor"), []byte("  0x1E52  \n"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			want: true,
		},
		{
			name: "directory entry without vendor file is ignored",
			setup: func(t *testing.T, root string) {
				if err := os.MkdirAll(filepath.Join(root, "0000:01:00.0"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			// For "missing sysfs", point at a nonexistent path under tmp.
			scanRoot := root
			if tc.name == "missing sysfs returns false without error" {
				scanRoot = filepath.Join(root, "does-not-exist")
			} else {
				tc.setup(t, root)
			}

			got, err := DevicePresent(scanRoot)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("DevicePresent = %v, want %v", got, tc.want)
			}
		})
	}
}
