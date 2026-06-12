package dockerctl

import (
	"context"
	"errors"
	"testing"
)

type fakeEngine struct {
	state       map[string]bool
	startErr    error
	stopErr     error
	startCalls  []string
	stopCalls   []string
	created     []CreateSpec
	removed     []string
	pulled      []string
	imgsPresent map[string]bool // refs already present locally
	containers  []ContainerInfo // returned by list (filtered in-fake)
	nets        []string        // selfNetworks result
}

func (f *fakeEngine) start(_ context.Context, name string) error {
	f.startCalls = append(f.startCalls, name)
	if f.startErr != nil {
		return f.startErr
	}
	f.state[name] = true
	return nil
}
func (f *fakeEngine) stop(_ context.Context, name string) error {
	f.stopCalls = append(f.stopCalls, name)
	if f.stopErr != nil {
		return f.stopErr
	}
	f.state[name] = false
	return nil
}
func (f *fakeEngine) running(_ context.Context, name string) (bool, error) {
	return f.state[name], nil
}
func (f *fakeEngine) exists(_ context.Context, name string) (bool, error) {
	_, ok := f.state[name]
	return ok, nil
}
func (f *fakeEngine) ensureImage(_ context.Context, ref string) error {
	if f.imgsPresent[ref] {
		return nil
	}
	f.pulled = append(f.pulled, ref)
	return nil
}
func (f *fakeEngine) create(_ context.Context, spec CreateSpec) error {
	f.created = append(f.created, spec)
	f.state[spec.Name] = false // created but not started
	return nil
}
func (f *fakeEngine) remove(_ context.Context, name string) error {
	f.removed = append(f.removed, name)
	delete(f.state, name)
	return nil
}
func (f *fakeEngine) list(_ context.Context, labelFilter map[string]string) ([]ContainerInfo, error) {
	var out []ContainerInfo
	for _, c := range f.containers {
		match := true
		for k, v := range labelFilter {
			if c.Labels[k] != v {
				match = false
				break
			}
		}
		if match {
			out = append(out, c)
		}
	}
	return out, nil
}
func (f *fakeEngine) selfNetworks(_ context.Context) ([]string, error) {
	return f.nets, nil
}

func TestController_StartStopRunning(t *testing.T) {
	f := &fakeEngine{state: map[string]bool{"box": false}}
	c := &Client{eng: f}
	ctx := context.Background()

	if err := c.Start(ctx, "box"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	ok, _ := c.Running(ctx, "box")
	if !ok {
		t.Fatal("want running after Start")
	}
	if err := c.Stop(ctx, "box"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	ok, _ = c.Running(ctx, "box")
	if ok {
		t.Fatal("want stopped after Stop")
	}
}

func TestController_StartErrorPropagates(t *testing.T) {
	f := &fakeEngine{state: map[string]bool{}, startErr: errors.New("boom")}
	c := &Client{eng: f}
	if err := c.Start(context.Background(), "box"); err == nil {
		t.Fatal("want error")
	}
}

func TestController_EnsureImagePullsWhenAbsent(t *testing.T) {
	f := &fakeEngine{state: map[string]bool{}, imgsPresent: map[string]bool{}}
	c := &Client{eng: f}
	if err := c.EnsureImage(context.Background(), "ghcr.io/x:latest"); err != nil {
		t.Fatalf("EnsureImage: %v", err)
	}
	if len(f.pulled) != 1 || f.pulled[0] != "ghcr.io/x:latest" {
		t.Fatalf("want pull of absent image, got %v", f.pulled)
	}
}

func TestController_EnsureImageSkipsWhenPresent(t *testing.T) {
	f := &fakeEngine{state: map[string]bool{}, imgsPresent: map[string]bool{"ghcr.io/x:latest": true}}
	c := &Client{eng: f}
	if err := c.EnsureImage(context.Background(), "ghcr.io/x:latest"); err != nil {
		t.Fatalf("EnsureImage: %v", err)
	}
	if len(f.pulled) != 0 {
		t.Fatalf("present image must not pull, got %v", f.pulled)
	}
}

func TestController_CreateThenRemove(t *testing.T) {
	f := &fakeEngine{state: map[string]bool{}, imgsPresent: map[string]bool{}}
	c := &Client{eng: f}
	ctx := context.Background()
	spec := CreateSpec{Name: "box", Image: "img", Port: 9090, Labels: map[string]string{LabelManaged: "true"}}
	if err := c.Create(ctx, spec); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(f.created) != 1 || f.created[0].Name != "box" {
		t.Fatalf("want 1 create, got %v", f.created)
	}
	ok, _ := c.Exists(ctx, "box")
	if !ok {
		t.Fatal("want box to exist after Create")
	}
	if err := c.Remove(ctx, "box"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(f.removed) != 1 || f.removed[0] != "box" {
		t.Fatalf("want box removed, got %v", f.removed)
	}
}

func TestController_ListFiltersByLabel(t *testing.T) {
	f := &fakeEngine{state: map[string]bool{}, containers: []ContainerInfo{
		{Name: "managed", Labels: map[string]string{LabelManaged: "true"}},
		{Name: "plain", Labels: map[string]string{}},
	}}
	c := &Client{eng: f}
	got, err := c.List(context.Background(), map[string]string{LabelManaged: "true"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].Name != "managed" {
		t.Fatalf("want only managed container, got %v", got)
	}
}
