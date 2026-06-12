// Package dockerctl controls containers by name over the host Docker socket.
// Sole responsibility: start/stop/inspect — no daemon policy lives here.
package dockerctl

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// Controller is the narrow surface the rest of the app depends on, so callers
// can fake it and a nil Controller can mean "degrade to no container control".
type Controller interface {
	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string) error
	Running(ctx context.Context, name string) (bool, error)
}

// engine is the slice of the Docker SDK we use; the real one wraps *client.Client.
type engine interface {
	start(ctx context.Context, name string) error
	stop(ctx context.Context, name string) error
	running(ctx context.Context, name string) (bool, error)
}

type Client struct{ eng engine }

// New connects to the Docker daemon via the ambient environment
// (DOCKER_HOST or /var/run/docker.sock). Returns an error if unreachable so
// the caller can fall back to always-on sidecars.
func New() (*Client, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	if _, err := cli.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("docker ping: %w", err)
	}
	return &Client{eng: &sdkEngine{cli: cli}}, nil
}

func (c *Client) Start(ctx context.Context, name string) error { return c.eng.start(ctx, name) }
func (c *Client) Stop(ctx context.Context, name string) error  { return c.eng.stop(ctx, name) }
func (c *Client) Running(ctx context.Context, name string) (bool, error) {
	return c.eng.running(ctx, name)
}

type sdkEngine struct{ cli *client.Client }

func (s *sdkEngine) start(ctx context.Context, name string) error {
	return s.cli.ContainerStart(ctx, name, container.StartOptions{})
}
func (s *sdkEngine) stop(ctx context.Context, name string) error {
	t := 15 // seconds
	return s.cli.ContainerStop(ctx, name, container.StopOptions{Timeout: &t})
}
func (s *sdkEngine) running(ctx context.Context, name string) (bool, error) {
	info, err := s.cli.ContainerInspect(ctx, name)
	if err != nil {
		return false, err
	}
	return info.State != nil && info.State.Running, nil
}
