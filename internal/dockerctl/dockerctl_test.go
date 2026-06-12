package dockerctl

import (
	"context"
	"errors"
	"testing"
)

type fakeEngine struct {
	state     map[string]bool
	startErr  error
	stopErr   error
	startCalls []string
	stopCalls  []string
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
