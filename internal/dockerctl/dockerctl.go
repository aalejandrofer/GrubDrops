// Package dockerctl controls containers by name over the host Docker socket.
// It can start/stop/inspect existing containers AND auto-create/remove the
// miner's own browser sidecars (pull image, create on the miner's network,
// remove orphans) — no daemon policy beyond that lives here.
package dockerctl

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

// Labels stamped on every miner-created sidecar so the orphan sweep can find
// ONLY containers the miner owns. Hand-defined (compose) sidecars carry none
// of these and are therefore never touched by the sweep.
const (
	LabelManaged = "grubdrops.managed" // "true"
	LabelAccount = "grubdrops.account" // account id
	LabelSlug    = "grubdrops.slug"    // username slug
)

// CreateSpec describes a sidecar to bring into existence.
type CreateSpec struct {
	Name     string            // container name (e.g. grubdrops-browser-ttik3r)
	Image    string            // image ref (ghcr.io/...:latest)
	Port     int               // gRPC port to expose (no host binding)
	Networks []string          // user-defined networks to attach to
	Labels   map[string]string // managed/account/slug labels
	Env      []string          // container environment ("KEY=VALUE" entries)
}

// ContainerInfo is the slice of a listed container the sweep needs.
type ContainerInfo struct {
	Name   string
	Labels map[string]string
}

// Controller is the narrow surface the rest of the app depends on, so callers
// can fake it and a nil Controller can mean "degrade to no container control".
type Controller interface {
	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string) error
	Running(ctx context.Context, name string) (bool, error)
	// Exists reports whether a container with this name exists (any state).
	Exists(ctx context.Context, name string) (bool, error)
	// EnsureImage pulls image if it is not already present locally.
	EnsureImage(ctx context.Context, image string) error
	// Create makes (but does not start) a container from spec.
	Create(ctx context.Context, spec CreateSpec) error
	// Remove force-removes a container (stopping it first).
	Remove(ctx context.Context, name string) error
	// List returns containers (any state) matching every label in labelFilter.
	List(ctx context.Context, labelFilter map[string]string) ([]ContainerInfo, error)
	// SelfNetworks returns the user-defined networks the miner's OWN container
	// is attached to (the default "bridge" is skipped when a user network
	// exists). Empty + nil error means "could not self-detect" (degrade).
	SelfNetworks(ctx context.Context) ([]string, error)
}

// engine is the slice of the Docker SDK we use; the real one wraps *client.Client.
type engine interface {
	start(ctx context.Context, name string) error
	stop(ctx context.Context, name string) error
	running(ctx context.Context, name string) (bool, error)
	exists(ctx context.Context, name string) (bool, error)
	ensureImage(ctx context.Context, ref string) error
	create(ctx context.Context, spec CreateSpec) error
	remove(ctx context.Context, name string) error
	list(ctx context.Context, labelFilter map[string]string) ([]ContainerInfo, error)
	selfNetworks(ctx context.Context) ([]string, error)
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
func (c *Client) Exists(ctx context.Context, name string) (bool, error) {
	return c.eng.exists(ctx, name)
}
func (c *Client) EnsureImage(ctx context.Context, ref string) error {
	return c.eng.ensureImage(ctx, ref)
}
func (c *Client) Create(ctx context.Context, spec CreateSpec) error { return c.eng.create(ctx, spec) }
func (c *Client) Remove(ctx context.Context, name string) error     { return c.eng.remove(ctx, name) }
func (c *Client) List(ctx context.Context, labelFilter map[string]string) ([]ContainerInfo, error) {
	return c.eng.list(ctx, labelFilter)
}
func (c *Client) SelfNetworks(ctx context.Context) ([]string, error) {
	return c.eng.selfNetworks(ctx)
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

func (s *sdkEngine) exists(ctx context.Context, name string) (bool, error) {
	_, err := s.cli.ContainerInspect(ctx, name)
	if err != nil {
		if client.IsErrNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ensureImage pulls ref only when it is not already present locally. ghcr is
// public so we pass no auth. The pull reader MUST be drained to completion or
// the pull does not finish.
func (s *sdkEngine) ensureImage(ctx context.Context, ref string) error {
	if _, _, err := s.cli.ImageInspectWithRaw(ctx, ref); err == nil {
		return nil // already present
	} else if !client.IsErrNotFound(err) {
		return fmt.Errorf("image inspect %q: %w", ref, err)
	}
	rc, err := s.cli.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("image pull %q: %w", ref, err)
	}
	defer rc.Close()
	if _, err := io.Copy(io.Discard, rc); err != nil {
		return fmt.Errorf("image pull %q drain: %w", ref, err)
	}
	return nil
}

func (s *sdkEngine) create(ctx context.Context, spec CreateSpec) error {
	port := nat.Port(fmt.Sprintf("%d/tcp", spec.Port))
	cfg := &container.Config{
		Image:        spec.Image,
		Labels:       spec.Labels,
		Env:          spec.Env,
		ExposedPorts: nat.PortSet{port: struct{}{}},
	}
	// RestartPolicy intentionally empty ("no"): the miner owns the lifecycle
	// (idle-stop reaper). unless-stopped would fight the reaper. No host port
	// binding — reached container-to-container by name on the shared network.
	hostCfg := &container.HostConfig{}
	netCfg := &network.NetworkingConfig{}
	if len(spec.Networks) > 0 {
		eps := make(map[string]*network.EndpointSettings, len(spec.Networks))
		for _, n := range spec.Networks {
			eps[n] = &network.EndpointSettings{}
		}
		netCfg.EndpointsConfig = eps
	}
	if _, err := s.cli.ContainerCreate(ctx, cfg, hostCfg, netCfg, nil, spec.Name); err != nil {
		return fmt.Errorf("container create %q: %w", spec.Name, err)
	}
	return nil
}

func (s *sdkEngine) remove(ctx context.Context, name string) error {
	return s.cli.ContainerRemove(ctx, name, container.RemoveOptions{Force: true})
}

func (s *sdkEngine) list(ctx context.Context, labelFilter map[string]string) ([]ContainerInfo, error) {
	args := filters.NewArgs()
	for k, v := range labelFilter {
		args.Add("label", k+"="+v)
	}
	cs, err := s.cli.ContainerList(ctx, container.ListOptions{All: true, Filters: args})
	if err != nil {
		return nil, fmt.Errorf("container list: %w", err)
	}
	out := make([]ContainerInfo, 0, len(cs))
	for _, c := range cs {
		name := ""
		if len(c.Names) > 0 {
			// Docker prefixes container names with "/".
			name = trimSlash(c.Names[0])
		}
		out = append(out, ContainerInfo{Name: name, Labels: c.Labels})
	}
	return out, nil
}

// selfNetworks inspects the miner's own container (id == hostname) and returns
// its user-defined networks, skipping the default "bridge" when a user network
// exists. Returns (nil, nil) when self-inspect is impossible (not in docker /
// no socket) so the caller degrades gracefully.
func (s *sdkEngine) selfNetworks(ctx context.Context) ([]string, error) {
	self, err := os.Hostname()
	if err != nil || self == "" {
		return nil, nil
	}
	info, err := s.cli.ContainerInspect(ctx, self)
	if err != nil {
		if client.IsErrNotFound(err) {
			return nil, nil // miner not running as a docker container
		}
		return nil, fmt.Errorf("self inspect %q: %w", self, err)
	}
	if info.NetworkSettings == nil {
		return nil, nil
	}
	var all, userDefined []string
	for name := range info.NetworkSettings.Networks {
		all = append(all, name)
		if name != "bridge" {
			userDefined = append(userDefined, name)
		}
	}
	if len(userDefined) > 0 {
		return userDefined, nil
	}
	return all, nil
}

func trimSlash(s string) string {
	if len(s) > 0 && s[0] == '/' {
		return s[1:]
	}
	return s
}
