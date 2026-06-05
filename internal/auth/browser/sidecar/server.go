package sidecar

import (
	"context"

	pb "github.com/aalejandrofer/dropsminer/internal/auth/browser/gen/browser/v1"
)

// Server implements the gRPC service. Methods translate proto types
// into Browser/Kick/Twitch calls.
type Server struct {
	pb.UnimplementedBrowserServer
	b      *Browser
	kick   *Kick
	twitch *Twitch
}

func NewServer(b *Browser) *Server {
	return &Server{b: b, kick: NewKick(b), twitch: NewTwitch(b)}
}

func (s *Server) Authenticate(ctx context.Context, req *pb.AuthenticateRequest) (*pb.AuthenticateResponse, error) {
	handle, tabCtx, err := s.b.OpenTab()
	if err != nil {
		return nil, err
	}
	defer s.b.CloseTab(handle)

	username, err := s.kick.VerifyAuth(tabCtx, req.Session)
	if err != nil {
		return nil, err
	}
	return &pb.AuthenticateResponse{Session: req.Session, Username: username}, nil
}

func (s *Server) StartWatch(ctx context.Context, req *pb.StartWatchRequest) (*pb.StartWatchResponse, error) {
	handle, err := s.kick.OpenStream(req.Channel, req.Session)
	if err != nil {
		return nil, err
	}
	return &pb.StartWatchResponse{WatchHandle: handle}, nil
}

func (s *Server) Heartbeat(ctx context.Context, req *pb.HeartbeatRequest) (*pb.HeartbeatResponse, error) {
	_, ok := s.b.Tab(req.WatchHandle)
	return &pb.HeartbeatResponse{Alive: ok}, nil
}

func (s *Server) StopWatch(ctx context.Context, req *pb.StopWatchRequest) (*pb.StopWatchResponse, error) {
	s.b.CloseTab(req.WatchHandle)
	return &pb.StopWatchResponse{}, nil
}

func (s *Server) Inventory(ctx context.Context, req *pb.InventoryRequest) (*pb.InventoryResponse, error) {
	drops, err := s.kick.Inventory(ctx, req.Session)
	if err != nil {
		return nil, err
	}
	return &pb.InventoryResponse{Drops: drops}, nil
}

func (s *Server) Claim(ctx context.Context, req *pb.ClaimRequest) (*pb.ClaimResponse, error) {
	already, err := s.kick.Claim(ctx, req.Session, req.BenefitId)
	if err != nil {
		return nil, err
	}
	return &pb.ClaimResponse{AlreadyClaimed: already}, nil
}

// --- Twitch ---

func (s *Server) TwitchAuthenticate(ctx context.Context, req *pb.TwitchAuthenticateRequest) (*pb.TwitchAuthenticateResponse, error) {
	username, userID, err := s.twitch.Authenticate(ctx, req.AccountId, req.Session)
	if err != nil {
		return nil, err
	}
	return &pb.TwitchAuthenticateResponse{Username: username, UserId: userID, Session: req.Session}, nil
}

func (s *Server) TwitchGQL(ctx context.Context, req *pb.TwitchGQLRequest) (*pb.TwitchGQLResponse, error) {
	body, status, err := s.twitch.GQL(ctx, req.AccountId, req.Body)
	if err != nil {
		return nil, err
	}
	return &pb.TwitchGQLResponse{Body: body, Status: int32(status)}, nil
}

func (s *Server) TwitchOpenStream(ctx context.Context, req *pb.TwitchOpenStreamRequest) (*pb.TwitchOpenStreamResponse, error) {
	handle, err := s.twitch.OpenStream(req.Channel)
	if err != nil {
		return nil, err
	}
	return &pb.TwitchOpenStreamResponse{WatchHandle: handle}, nil
}

func (s *Server) TwitchHeartbeat(ctx context.Context, req *pb.TwitchHeartbeatRequest) (*pb.TwitchHeartbeatResponse, error) {
	_, ok := s.b.Tab(req.WatchHandle)
	return &pb.TwitchHeartbeatResponse{Alive: ok}, nil
}

func (s *Server) TwitchStopWatch(ctx context.Context, req *pb.TwitchStopWatchRequest) (*pb.TwitchStopWatchResponse, error) {
	s.b.CloseTab(req.WatchHandle)
	return &pb.TwitchStopWatchResponse{}, nil
}
