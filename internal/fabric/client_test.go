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
	"google.golang.org/protobuf/proto"

	fsdpb "github.com/tenstorrent/tt-feature-discovery/internal/fabric/fsdpb"
	orchpb "github.com/tenstorrent/tt-feature-discovery/internal/fabric/orchestratorpb"
)

// fakeServer implements just enough of FabricManagerService to satisfy
// tests: it returns a canned FSD on every QueryFactorySystemDescriptor
// call and counts invocations for cache-behavior assertions.
type fakeServer struct {
	orchpb.UnimplementedFabricManagerServiceServer

	fsd   *fsdpb.FactorySystemDescriptor
	calls int
	err   error
}

func (f *fakeServer) QueryFactorySystemDescriptor(
	_ context.Context,
	_ *orchpb.QueryFactorySystemDescriptorRequest,
) (*orchpb.QueryFactorySystemDescriptorResponse, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	raw, err := proto.Marshal(f.fsd)
	if err != nil {
		return nil, err
	}
	return &orchpb.QueryFactorySystemDescriptorResponse{
		FsdFile: []*orchpb.FactorySystemDescriptorFile{{
			Name:           "test.textproto",
			TextprotoBytes: raw,
		}},
	}, nil
}

func newBufconnClient(t *testing.T, srv *fakeServer, ttl time.Duration) *Client {
	t.Helper()
	lis := bufconn.Listen(1 << 16)
	s := grpc.NewServer()
	orchpb.RegisterFabricManagerServiceServer(s, srv)
	go func() { _ = s.Serve(lis) }()
	t.Cleanup(func() { s.Stop() })

	dial := func(_ string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
		opts = append(opts,
			grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) {
				return lis.DialContext(context.Background())
			}),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		return grpc.NewClient("passthrough:///bufnet", opts...)
	}
	return NewClient("bufnet", WithCacheTTL(ttl), withDialer(dial))
}

func sampleFSD() *fsdpb.FactorySystemDescriptor {
	return &fsdpb.FactorySystemDescriptor{
		Hosts: []*fsdpb.FactorySystemDescriptor_Host{
			{Hostname: "e01cs01", Hall: "h1", Rack: 1, ShelfU: 10},
			{Hostname: "e01cs02", Hall: "h1", Rack: 1, ShelfU: 12},
			{Hostname: "e01cs03", Hall: "h1", Rack: 1, ShelfU: 14},
		},
	}
}

func TestClientSnapshot(t *testing.T) {
	srv := &fakeServer{fsd: sampleFSD()}
	c := newBufconnClient(t, srv, 1*time.Hour)

	fsd, err := c.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if got := len(fsd.GetHosts()); got != 3 {
		t.Errorf("hosts = %d, want 3", got)
	}
	if srv.calls != 1 {
		t.Errorf("server calls = %d, want 1", srv.calls)
	}

	// Second Snapshot within TTL must be served from cache.
	if _, err := c.Snapshot(context.Background()); err != nil {
		t.Fatalf("second Snapshot: %v", err)
	}
	if srv.calls != 1 {
		t.Errorf("expected cached read, server calls = %d, want 1", srv.calls)
	}
}

func TestClientSnapshotRefetchesAfterTTL(t *testing.T) {
	srv := &fakeServer{fsd: sampleFSD()}
	c := newBufconnClient(t, srv, 1*time.Nanosecond)

	if _, err := c.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := c.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if srv.calls != 2 {
		t.Errorf("server calls = %d, want 2 (TTL elapsed)", srv.calls)
	}
}

func TestFindHost(t *testing.T) {
	fsd := sampleFSD()

	h, idx, err := FindHost(fsd, "e01cs02")
	if err != nil {
		t.Fatalf("FindHost: %v", err)
	}
	if h.GetHostname() != "e01cs02" || idx != 1 {
		t.Errorf("got host=%q idx=%d, want e01cs02, 1", h.GetHostname(), idx)
	}

	// Case-insensitive
	_, idx, err = FindHost(fsd, "E01CS03")
	if err != nil || idx != 2 {
		t.Errorf("case-insensitive lookup failed: idx=%d err=%v", idx, err)
	}

	// Missing
	if _, _, err := FindHost(fsd, "nope"); !errors.Is(err, ErrHostNotFound) {
		t.Errorf("missing host err = %v, want ErrHostNotFound", err)
	}

	// Nil FSD
	if _, _, err := FindHost(nil, "any"); !errors.Is(err, ErrHostNotFound) {
		t.Errorf("nil FSD err = %v, want ErrHostNotFound", err)
	}
}
