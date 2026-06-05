package kick

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/aalejandrofer/rust-drops-miner/internal/auth/browser"
	pb "github.com/aalejandrofer/rust-drops-miner/internal/auth/browser/gen/browser/v1"
	"github.com/aalejandrofer/rust-drops-miner/internal/platform"
)

type stubServer struct {
	pb.UnimplementedBrowserServer
	drops  []*pb.DropProgress
	handle string
}

func (s *stubServer) Inventory(_ context.Context, _ *pb.InventoryRequest) (*pb.InventoryResponse, error) {
	return &pb.InventoryResponse{Drops: s.drops}, nil
}

func (s *stubServer) StartWatch(_ context.Context, _ *pb.StartWatchRequest) (*pb.StartWatchResponse, error) {
	return &pb.StartWatchResponse{WatchHandle: s.handle}, nil
}

func (s *stubServer) Heartbeat(_ context.Context, req *pb.HeartbeatRequest) (*pb.HeartbeatResponse, error) {
	return &pb.HeartbeatResponse{Alive: req.WatchHandle == "tab_42"}, nil
}

func startStub(t *testing.T, srv *stubServer) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	gs := grpc.NewServer()
	pb.RegisterBrowserServer(gs, srv)
	go gs.Serve(lis)
	t.Cleanup(func() { gs.GracefulStop() })
	return lis.Addr().String()
}

func TestKickBackend_InventoryProgress(t *testing.T) {
	stub := &stubServer{drops: []*pb.DropProgress{
		{BenefitId: "d1", MinutesWatched: 45, Claimed: false},
	}}
	addr := startStub(t, stub)

	c, err := browser.Dial(addr)
	require.NoError(t, err)
	defer c.Close()

	b := New(c)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pr, err := b.InventoryProgress(ctx, platform.Session{Cookies: map[string]string{"kick": `{}`}})
	require.NoError(t, err)
	require.Len(t, pr, 1)
	assert.Equal(t, "d1", pr[0].BenefitID)
	assert.Equal(t, 45, pr[0].MinutesWatched)
}

func TestKickBackend_StartHeartbeatStop(t *testing.T) {
	stub := &stubServer{handle: "tab_42"}
	addr := startStub(t, stub)

	c, err := browser.Dial(addr)
	require.NoError(t, err)
	defer c.Close()

	b := New(c)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	h, err := b.StartWatch(ctx, platform.Session{Cookies: map[string]string{"kick": `{}`}}, platform.Stream{Channel: "fakestreamer"})
	require.NoError(t, err)
	assert.Equal(t, "tab_42", h.Internal)

	require.NoError(t, b.Heartbeat(ctx, h))
}

func TestKickBackend_DeviceLoginRejected(t *testing.T) {
	b := New(nil)
	_, err := b.StartDeviceLogin(context.Background())
	require.Error(t, err)
}

// When the per-account whitelist excludes "Rust", Kick's synthetic
// campaign must be suppressed at the backend layer — the only game it
// can mine is Rust, so a rejecting filter means "this account opted
// out of Kick entirely". Crucially the backend must NOT issue a
// sidecar Inventory call when the filter rejects the game.
func TestKickBackend_ListActiveCampaigns_GameFilterShortCircuits(t *testing.T) {
	// nil browser client is fine because the short-circuit must happen
	// before any sidecar call. If the filter check is missing the test
	// will panic on b.c.Inventory.
	b := New(nil)
	sess := platform.Session{
		Cookies:    map[string]string{"kick": `{}`},
		GameFilter: func(game string) bool { return false },
	}
	camps, err := b.ListActiveCampaigns(context.Background(), sess)
	require.NoError(t, err)
	assert.Empty(t, camps)
}

func TestKickBackend_RegisterChannelExposesInList(t *testing.T) {
	b := New(nil)
	b.RegisterChannel("acc1", "fakestreamer")

	out, err := b.ListEligibleChannels(context.Background(), platform.Session{}, platform.Campaign{ID: "x"})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "fakestreamer", out[0].Channel)
}
