package kick

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/aalejandrofer/dropsminer/internal/auth/browser"
	pb "github.com/aalejandrofer/dropsminer/internal/auth/browser/gen/browser/v1"
	"github.com/aalejandrofer/dropsminer/internal/platform"
)

type stubServer struct {
	pb.UnimplementedBrowserServer
	drops     []*pb.DropProgress
	campaigns []*pb.KickCampaign
	handle    string
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

func (s *stubServer) KickScrapeActiveDrops(_ context.Context, _ *pb.KickScrapeActiveDropsRequest) (*pb.KickScrapeActiveDropsResponse, error) {
	return &pb.KickScrapeActiveDropsResponse{Campaigns: s.campaigns}, nil
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

// ListActiveCampaigns must no-op when the backend has no browser
// client wired (e.g. MINER_BROWSER_URL empty) — discovery should
// gracefully skip Kick rather than panic.
func TestKickBackend_ListActiveCampaigns_NilClient(t *testing.T) {
	b := New(nil)
	sess := platform.Session{Cookies: map[string]string{"kick": `{}`}}
	camps, err := b.ListActiveCampaigns(context.Background(), sess)
	require.NoError(t, err)
	assert.Empty(t, camps)
}

// ListActiveCampaigns is game-agnostic: it surfaces every active
// campaign the sidecar scrapes, and the per-account GameFilter prunes
// them down. Verifies (1) multiple games surface, (2) filter rejects
// non-whitelisted ones, (3) Game / Name / Benefits round-trip.
func TestKickBackend_ListActiveCampaigns_GameAgnosticFilter(t *testing.T) {
	stub := &stubServer{campaigns: []*pb.KickCampaign{
		{
			Id: "c-rust", Game: "Rust", Name: "Rust Twitch Drops", Status: "active",
			Benefits: []*pb.KickBenefit{{Id: "ben1", Name: "Crate", RequiredMinutes: 60}},
		},
		{
			Id: "c-cs", Game: "Counter-Strike", Name: "CS Drops", Status: "active",
			Benefits: []*pb.KickBenefit{{Id: "ben2", Name: "Sticker", RequiredMinutes: 30}},
		},
	}}
	addr := startStub(t, stub)
	c, err := browser.Dial(addr)
	require.NoError(t, err)
	defer c.Close()

	b := New(c)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// No filter -> both surface.
	all, err := b.ListActiveCampaigns(ctx, platform.Session{Cookies: map[string]string{"kick": `{}`}})
	require.NoError(t, err)
	require.Len(t, all, 2)
	games := map[string]bool{all[0].Game: true, all[1].Game: true}
	assert.True(t, games["Rust"] && games["Counter-Strike"])
	for _, camp := range all {
		assert.Equal(t, "kick", camp.Platform)
		require.NotEmpty(t, camp.Benefits)
		assert.Equal(t, camp.ID, camp.Benefits[0].CampaignID)
	}

	// Filter only Rust -> CS dropped.
	sess := platform.Session{
		Cookies:    map[string]string{"kick": `{}`},
		GameFilter: func(game string) bool { return game == "Rust" },
	}
	only, err := b.ListActiveCampaigns(ctx, sess)
	require.NoError(t, err)
	require.Len(t, only, 1)
	assert.Equal(t, "Rust", only[0].Game)
}

// Benefits with required_minutes==0 should default to 120 (2h) to
// match Kick's typical drop threshold — the scraper can't always
// recover the per-drop requirement from the page state.
func TestKickBackend_ListActiveCampaigns_DefaultRequiredMinutes(t *testing.T) {
	stub := &stubServer{campaigns: []*pb.KickCampaign{
		{
			Id: "c1", Game: "Rust", Name: "Drops", Status: "active",
			Benefits: []*pb.KickBenefit{{Id: "b1", Name: "Reward"}},
		},
	}}
	addr := startStub(t, stub)
	c, err := browser.Dial(addr)
	require.NoError(t, err)
	defer c.Close()

	b := New(c)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	camps, err := b.ListActiveCampaigns(ctx, platform.Session{Cookies: map[string]string{"kick": `{}`}})
	require.NoError(t, err)
	require.Len(t, camps, 1)
	require.Len(t, camps[0].Benefits, 1)
	assert.Equal(t, 120, camps[0].Benefits[0].RequiredMinutes)
}

func TestKickBackend_RegisterChannelExposesInList(t *testing.T) {
	b := New(nil)
	b.RegisterChannel("acc1", "fakestreamer")

	// Session must carry AccountID so the backend can scope channels
	// to the owning account (mining cross-account channels would fail
	// the heartbeat since cookies belong to a different user).
	out, err := b.ListEligibleChannels(context.Background(), platform.Session{AccountID: "acc1"}, platform.Campaign{ID: "x"})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "fakestreamer", out[0].Channel)
}

// TestKickBackend_RegisterChannelsMulti: an account may register N
// channels and ListEligibleChannels must return all of them (F6).
func TestKickBackend_RegisterChannelsMulti(t *testing.T) {
	b := New(nil)
	b.RegisterChannels("acc1", []string{"alice", "bob", "carol"})

	out, err := b.ListEligibleChannels(context.Background(), platform.Session{AccountID: "acc1"}, platform.Campaign{ID: "x"})
	require.NoError(t, err)
	require.Len(t, out, 3)
	names := map[string]bool{}
	for _, s := range out {
		names[s.Channel] = true
	}
	assert.True(t, names["alice"])
	assert.True(t, names["bob"])
	assert.True(t, names["carol"])
}

// TestKickBackend_ListEligibleChannelsScopedToAccount: channels
// registered for one account must NOT leak into another account's
// session result.
func TestKickBackend_ListEligibleChannelsScopedToAccount(t *testing.T) {
	b := New(nil)
	b.RegisterChannels("acc1", []string{"alice", "bob"})
	b.RegisterChannels("acc2", []string{"carol"})

	out1, err := b.ListEligibleChannels(context.Background(), platform.Session{AccountID: "acc1"}, platform.Campaign{})
	require.NoError(t, err)
	require.Len(t, out1, 2)

	out2, err := b.ListEligibleChannels(context.Background(), platform.Session{AccountID: "acc2"}, platform.Campaign{})
	require.NoError(t, err)
	require.Len(t, out2, 1)
	assert.Equal(t, "carol", out2[0].Channel)

	outNone, err := b.ListEligibleChannels(context.Background(), platform.Session{AccountID: "acc3"}, platform.Campaign{})
	require.NoError(t, err)
	assert.Empty(t, outNone, "unknown account must see zero channels")
}

// TestKickBackend_RegisterChannels_DedupesAndTrims: ensures the
// dedupe/trim contract is honored so callers can paste sloppy input.
func TestKickBackend_RegisterChannels_DedupesAndTrims(t *testing.T) {
	b := New(nil)
	b.RegisterChannels("acc1", []string{" alice ", "Alice", "bob", "", " bob ", "carol"})
	got := b.Channels("acc1")
	assert.Equal(t, []string{"alice", "bob", "carol"}, got)
}

// TestKickBackend_AllowedChannelCountDistinct mirrors the contract the
// dashboard relies on: Kick doesn't surface a per-campaign allow-list
// (each account picks a single channel), so the count is the number
// of distinct channels across accounts. Two accounts pointing at the
// same channel collapse to a single eligible stream.
func TestKickBackend_AllowedChannelCountDistinct(t *testing.T) {
	b := New(nil)
	assert.Equal(t, 0, b.AllowedChannelCount("anything"), "no accounts -> zero")

	b.RegisterChannel("acc1", "alice")
	b.RegisterChannel("acc2", "bob")
	b.RegisterChannel("acc3", "alice") // duplicate channel
	assert.Equal(t, 2, b.AllowedChannelCount("kick-inventory"))
}
