package browser

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/aalejandrofer/rust-drops-miner/internal/auth/browser/gen/browser/v1"
)

// Client wraps the generated gRPC client with a friendlier surface.
type Client struct {
	conn *grpc.ClientConn
	api  pb.BrowserClient
}

// Dial connects to the sidecar's gRPC endpoint (e.g. "browser:9090").
// Insecure because the sidecar lives on a compose-internal network.
func Dial(target string) (*Client, error) {
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial sidecar %q: %w", target, err)
	}
	return &Client{conn: conn, api: pb.NewBrowserClient(conn)}, nil
}

func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) Authenticate(ctx context.Context, s *pb.KickSession) (*pb.AuthenticateResponse, error) {
	return c.api.Authenticate(ctx, &pb.AuthenticateRequest{Session: s})
}

func (c *Client) StartWatch(ctx context.Context, s *pb.KickSession, channel string) (string, error) {
	resp, err := c.api.StartWatch(ctx, &pb.StartWatchRequest{Session: s, Channel: channel})
	if err != nil {
		return "", err
	}
	return resp.WatchHandle, nil
}

func (c *Client) Heartbeat(ctx context.Context, handle string) (bool, error) {
	resp, err := c.api.Heartbeat(ctx, &pb.HeartbeatRequest{WatchHandle: handle})
	if err != nil {
		return false, err
	}
	return resp.Alive, nil
}

func (c *Client) StopWatch(ctx context.Context, handle string) error {
	_, err := c.api.StopWatch(ctx, &pb.StopWatchRequest{WatchHandle: handle})
	return err
}

func (c *Client) Inventory(ctx context.Context, s *pb.KickSession) ([]*pb.DropProgress, error) {
	resp, err := c.api.Inventory(ctx, &pb.InventoryRequest{Session: s})
	if err != nil {
		return nil, err
	}
	return resp.Drops, nil
}

func (c *Client) Claim(ctx context.Context, s *pb.KickSession, benefitID string) (bool, error) {
	resp, err := c.api.Claim(ctx, &pb.ClaimRequest{Session: s, BenefitId: benefitID})
	if err != nil {
		return false, err
	}
	return resp.AlreadyClaimed, nil
}

// --- Twitch ---

func (c *Client) TwitchAuthenticate(ctx context.Context, accountID string, s *pb.TwitchSession) (*pb.TwitchAuthenticateResponse, error) {
	return c.api.TwitchAuthenticate(ctx, &pb.TwitchAuthenticateRequest{AccountId: accountID, Session: s})
}

func (c *Client) TwitchGQL(ctx context.Context, accountID, opName string, body []byte) ([]byte, int, error) {
	resp, err := c.api.TwitchGQL(ctx, &pb.TwitchGQLRequest{AccountId: accountID, OperationName: opName, Body: body})
	if err != nil {
		return nil, 0, err
	}
	return resp.Body, int(resp.Status), nil
}

func (c *Client) TwitchOpenStream(ctx context.Context, accountID, channel string) (string, error) {
	resp, err := c.api.TwitchOpenStream(ctx, &pb.TwitchOpenStreamRequest{AccountId: accountID, Channel: channel})
	if err != nil {
		return "", err
	}
	return resp.WatchHandle, nil
}

func (c *Client) TwitchHeartbeat(ctx context.Context, handle string) (bool, error) {
	resp, err := c.api.TwitchHeartbeat(ctx, &pb.TwitchHeartbeatRequest{WatchHandle: handle})
	if err != nil {
		return false, err
	}
	return resp.Alive, nil
}

func (c *Client) TwitchStopWatch(ctx context.Context, handle string) error {
	_, err := c.api.TwitchStopWatch(ctx, &pb.TwitchStopWatchRequest{WatchHandle: handle})
	return err
}
