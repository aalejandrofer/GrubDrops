// cmd/browser-sidecar/main.go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"

	pb "github.com/aalejandrofer/dropsminer/internal/auth/browser/gen/browser/v1"
	"github.com/aalejandrofer/dropsminer/internal/auth/browser/sidecar"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	addr := os.Getenv("BROWSER_ADDR")
	if addr == "" {
		addr = "0.0.0.0:9090"
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	browser := sidecar.New(ctx)
	defer browser.Close()
	srv := sidecar.NewServer(browser)

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	gs := grpc.NewServer()
	pb.RegisterBrowserServer(gs, srv)

	go func() {
		logger.Info("browser sidecar listening", "addr", addr)
		if err := gs.Serve(lis); err != nil {
			logger.Error("grpc serve", "err", err)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down sidecar")
	stopped := make(chan struct{})
	go func() {
		gs.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		gs.Stop()
	}
	return nil
}
