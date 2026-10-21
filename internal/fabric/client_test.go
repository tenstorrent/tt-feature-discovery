// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 Tenstorrent USA, Inc.

package fabric

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	orchpb "github.com/tenstorrent/tt-feature-discovery/internal/fabric/orchestratorpb"
)

// fakeService is an in-memory FabricManagerService.
type fakeService struct {
	orchpb.UnimplementedFabricManagerServiceServer
	resp  *orchpb.QueryTopologySummaryResponse
	err   error
	calls int
	// lastSource records the topo_source the client asked for.
	lastSource orchpb.TopologySource
}

func (f *fakeService) QueryTopologySummary(_ context.Context, req *orchpb.QueryTopologySummaryRequest) (*orchpb.QueryTopologySummaryResponse, error) {
	f.calls++
	f.lastSource = req.GetTopoSource()
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

// newTestClient wires a Client to an in-process server over bufconn.
func newTestClient(t *testing.T, svc *fakeService, opts ...Option) *Client {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	orchpb.RegisterFabricManagerServiceServer(srv, svc)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	dial := func(target string, _ ...grpc.DialOption) (*grpc.ClientConn, error) {
		return grpc.NewClient("passthrough:///bufnet",
			grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
				return lis.DialContext(ctx)
			}),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
	}
	return NewClient("bufnet", append([]Option{withDialer(dial)}, opts...)...)
}

func sampleResponse() *orchpb.QueryTopologySummaryResponse {
	return &orchpb.QueryTopologySummaryResponse{
		Hosts: []*orchpb.HostSummary{
			{
				HostName:       "e01cs01",
				AsicCount:      8,
				Arch:           "wormhole_b0",
				ConnectedHosts: []string{"e01cs02", "e01cs03"},
				Source:         orchpb.TopologySource_COMBINED_PSD_FSD,
				Metadata: &orchpb.HostMetadata{
					Hall: "AUS2", Aisle: "A1", Rack: "12", ShelfU: "34",
				},
			},
			{HostName: "e01cs02", AsicCount: 8},
		},
		HostGroups: []*orchpb.HostGrouping{
			{
				HostNames: []string{"e01cs01", "e01cs02", "e01cs03"},
				Name:      "110-SC36_4x4_C1-10",
				Pods: []*orchpb.HostGrouping{
					{Name: "bh_galaxy_sp_0", HostNames: []string{"e01cs01", "e01cs02"}},
					{Name: "bh_galaxy_sp_1", HostNames: []string{"e01cs03"}},
				},
			},
		},
	}
}

func TestHostSummaryFound(t *testing.T) {
	svc := &fakeService{resp: sampleResponse()}
	c := newTestClient(t, svc)

	got, err := c.HostSummary(context.Background(), "e01cs01")
	if err != nil {
		t.Fatalf("HostSummary: %v", err)
	}
	if got.Host.GetAsicCount() != 8 {
		t.Errorf("asic count = %d, want 8", got.Host.GetAsicCount())
	}
	if got.Group.Name != "110-SC36_4x4_C1-10" || got.Group.Size != 3 {
		t.Errorf("group = %+v, want {110-SC36_4x4_C1-10 3}", got.Group)
	}
	if got.Pod.Name != "bh_galaxy_sp_0" || got.Pod.Size != 2 {
		t.Errorf("pod = %+v, want {bh_galaxy_sp_0 2}", got.Pod)
	}
	if got.Stale {
		t.Error("fresh response marked stale")
	}
	if svc.lastSource != orchpb.TopologySource_COMBINED_PSD_FSD {
		t.Errorf("topo_source = %v, want COMBINED_PSD_FSD", svc.lastSource)
	}
}

// The Kubernetes node name may be FQDN-qualified or cased differently from the
// agent's gethostname().
func TestHostSummaryMatchesShortName(t *testing.T) {
	c := newTestClient(t, &fakeService{resp: sampleResponse()})

	for _, name := range []string{"E01CS01", "e01cs01.aus2.tenstorrent.com", "e01cs01."} {
		got, err := c.HostSummary(context.Background(), name)
		if err != nil {
			t.Fatalf("HostSummary(%q): %v", name, err)
		}
		if got.Host.GetHostName() != "e01cs01" {
			t.Errorf("HostSummary(%q) matched %q", name, got.Host.GetHostName())
		}
	}
}

func TestHostSummaryNotFound(t *testing.T) {
	c := newTestClient(t, &fakeService{resp: sampleResponse()})

	_, err := c.HostSummary(context.Background(), "not-a-host")
	if !errors.Is(err, ErrHostNotFound) {
		t.Fatalf("err = %v, want ErrHostNotFound", err)
	}
}

func TestHostSummaryUsesCache(t *testing.T) {
	svc := &fakeService{resp: sampleResponse()}
	c := newTestClient(t, svc, WithCacheTTL(time.Hour))

	for i := 0; i < 3; i++ {
		if _, err := c.HostSummary(context.Background(), "e01cs01"); err != nil {
			t.Fatalf("HostSummary: %v", err)
		}
	}
	if svc.calls != 1 {
		t.Errorf("server calls = %d, want 1 (cached)", svc.calls)
	}
}

// A fabric manager that goes away should not immediately strip labels.
func TestHostSummaryServesStaleWithinWindow(t *testing.T) {
	svc := &fakeService{resp: sampleResponse()}
	now := time.Unix(0, 0)
	c := newTestClient(t, svc,
		WithCacheTTL(time.Minute),
		WithMaxStale(time.Hour),
		withClock(func() time.Time { return now }),
	)

	if _, err := c.HostSummary(context.Background(), "e01cs01"); err != nil {
		t.Fatalf("priming call: %v", err)
	}

	svc.err = errors.New("controller restarting")
	now = now.Add(10 * time.Minute) // past the TTL, inside the stale window

	got, err := c.HostSummary(context.Background(), "e01cs01")
	if err != nil {
		t.Fatalf("HostSummary: %v", err)
	}
	if !got.Stale {
		t.Error("cached response not marked stale")
	}
	if got.Host.GetAsicCount() != 8 {
		t.Errorf("asic count = %d, want 8", got.Host.GetAsicCount())
	}
}

func TestHostSummaryFailsPastStaleWindow(t *testing.T) {
	svc := &fakeService{resp: sampleResponse()}
	now := time.Unix(0, 0)
	c := newTestClient(t, svc,
		WithCacheTTL(time.Minute),
		WithMaxStale(time.Hour),
		withClock(func() time.Time { return now }),
	)

	if _, err := c.HostSummary(context.Background(), "e01cs01"); err != nil {
		t.Fatalf("priming call: %v", err)
	}

	svc.err = errors.New("gone")
	now = now.Add(2 * time.Hour)

	if _, err := c.HostSummary(context.Background(), "e01cs01"); err == nil {
		t.Fatal("expected error once the stale window elapsed")
	}
}

func TestHostSummaryNoCacheReturnsError(t *testing.T) {
	svc := &fakeService{err: errors.New("unavailable")}
	c := newTestClient(t, svc)

	if _, err := c.HostSummary(context.Background(), "e01cs01"); err == nil {
		t.Fatal("expected error with no cached response")
	}
}

// Pods are optional: the fabric manager omits them when the hierarchy bottoms
// out at the group, and a host can belong to a group but to no pod.
func TestHostSummaryGroupWithoutPod(t *testing.T) {
	resp := &orchpb.QueryTopologySummaryResponse{
		Hosts: []*orchpb.HostSummary{{HostName: "e01cs09"}},
		HostGroups: []*orchpb.HostGrouping{{
			HostNames: []string{"e01cs09", "e01cs10"},
			Name:      "110-SC36_4x4_C1-10",
			Pods:      []*orchpb.HostGrouping{{Name: "pod-a", HostNames: []string{"e01cs10"}}},
		}},
	}
	c := newTestClient(t, &fakeService{resp: resp})

	got, err := c.HostSummary(context.Background(), "e01cs09")
	if err != nil {
		t.Fatalf("HostSummary: %v", err)
	}
	if got.Group.Size != 2 {
		t.Errorf("group size = %d, want 2", got.Group.Size)
	}
	if got.Pod != (Grouping{}) {
		t.Errorf("pod = %+v, want zero", got.Pod)
	}
}

// Groups are unnamed when no member is described by an FSD.
func TestHostSummaryUnnamedGroup(t *testing.T) {
	resp := &orchpb.QueryTopologySummaryResponse{
		Hosts:      []*orchpb.HostSummary{{HostName: "n150-01"}},
		HostGroups: []*orchpb.HostGrouping{{HostNames: []string{"n150-01"}}},
	}
	c := newTestClient(t, &fakeService{resp: resp})

	got, err := c.HostSummary(context.Background(), "n150-01")
	if err != nil {
		t.Fatalf("HostSummary: %v", err)
	}
	if got.Group.Name != "" || got.Group.Size != 1 {
		t.Errorf("group = %+v, want {\"\" 1}", got.Group)
	}
}

func TestHostSummaryNoGroups(t *testing.T) {
	resp := &orchpb.QueryTopologySummaryResponse{
		Hosts: []*orchpb.HostSummary{{HostName: "n150-01"}},
	}
	c := newTestClient(t, &fakeService{resp: resp})

	got, err := c.HostSummary(context.Background(), "n150-01")
	if err != nil {
		t.Fatalf("HostSummary: %v", err)
	}
	if got.Group != (Grouping{}) || got.Pod != (Grouping{}) {
		t.Errorf("group=%+v pod=%+v, want both zero", got.Group, got.Pod)
	}
}
