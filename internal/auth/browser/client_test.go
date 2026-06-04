package browser

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	pb "github.com/aalejandrofer/rust-drops-miner/internal/auth/browser/gen/browser/v1"
)

type stubServer struct {
	pb.UnimplementedBrowserServer
	authUsername string
	watchHandle  string
	drops        []*pb.DropProgress
}

func (s *stubServer) Authenticate(_ context.Context, req *pb.AuthenticateRequest) (*pb.AuthenticateResponse, error) {
	return &pb.AuthenticateResponse{Session: req.Session, Username: s.authUsername}, nil
}

func (s *stubServer) StartWatch(_ context.Context, _ *pb.StartWatchRequest) (*pb.StartWatchResponse, error) {
	return &pb.StartWatchResponse{WatchHandle: s.watchHandle}, nil
}

func (s *stubServer) Inventory(_ context.Context, _ *pb.InventoryRequest) (*pb.InventoryResponse, error) {
	return &pb.InventoryResponse{Drops: s.drops}, nil
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

func TestClient_RoundTrips(t *testing.T) {
	stub := &stubServer{
		authUsername: "demo",
		watchHandle:  "tab_42",
		drops: []*pb.DropProgress{
			{BenefitId: "d1", MinutesWatched: 10, Claimed: false},
		},
	}
	addr := startStub(t, stub)

	c, err := Dial(addr)
	require.NoError(t, err)
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	authResp, err := c.Authenticate(ctx, &pb.KickSession{XsrfToken: "tok"})
	require.NoError(t, err)
	assert.Equal(t, "demo", authResp.Username)

	handle, err := c.StartWatch(ctx, &pb.KickSession{}, "fakestreamer")
	require.NoError(t, err)
	assert.Equal(t, "tab_42", handle)

	drops, err := c.Inventory(ctx, &pb.KickSession{})
	require.NoError(t, err)
	require.Len(t, drops, 1)
	assert.Equal(t, "d1", drops[0].BenefitId)
}
