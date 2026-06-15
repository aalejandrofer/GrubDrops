package kick

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/dockerctl"
)

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"TTik3r":       "ttik3r",
		"Phluses":      "phluses",
		"Cool_Name 99": "cool-name-99",
		"--weird--":    "weird",
		"":             "",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q)=%q want %q", in, got, want)
		}
	}
}

func TestSidecarName(t *testing.T) {
	tmpl := "grubdrops-browser-{slug}"
	if got := sidecarName(tmpl, "TTik3r"); got != "grubdrops-browser-ttik3r" {
		t.Fatalf("got %q", got)
	}
	// empty slug → empty name (caller treats as "no controllable sidecar")
	if got := sidecarName(tmpl, ""); got != "" {
		t.Fatalf("empty username should yield empty name, got %q", got)
	}
}

// fakeCtl implements dockerctl.Controller for tests. run holds containers that
// EXIST (value = running state). containers feeds List (orphan sweep).
type fakeCtl struct {
	mu         sync.Mutex
	run        map[string]bool
	starts     []string
	stops      []string
	created    []dockerctl.CreateSpec
	removed    []string
	pulled     []string
	containers []dockerctl.ContainerInfo
	nets       []string
}

func newFakeCtl() *fakeCtl { return &fakeCtl{run: map[string]bool{}} }
func (f *fakeCtl) Start(_ context.Context, n string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.run[n] = true
	f.starts = append(f.starts, n)
	return nil
}
func (f *fakeCtl) Stop(_ context.Context, n string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.run[n] = false
	f.stops = append(f.stops, n)
	return nil
}
func (f *fakeCtl) Running(_ context.Context, n string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.run[n], nil
}
func (f *fakeCtl) Exists(_ context.Context, n string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.run[n]
	return ok, nil
}
func (f *fakeCtl) EnsureImage(_ context.Context, ref string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pulled = append(f.pulled, ref)
	return nil
}
func (f *fakeCtl) Create(_ context.Context, spec dockerctl.CreateSpec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, spec)
	f.run[spec.Name] = false // created, not started
	return nil
}
func (f *fakeCtl) Remove(_ context.Context, n string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, n)
	delete(f.run, n)
	return nil
}
func (f *fakeCtl) List(_ context.Context, labelFilter map[string]string) ([]dockerctl.ContainerInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []dockerctl.ContainerInfo
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
func (f *fakeCtl) SelfNetworks(_ context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.nets, nil
}
func (f *fakeCtl) stopCount() int   { f.mu.Lock(); defer f.mu.Unlock(); return len(f.stops) }
func (f *fakeCtl) createCount() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.created) }
func (f *fakeCtl) startCount() int  { f.mu.Lock(); defer f.mu.Unlock(); return len(f.starts) }
func (f *fakeCtl) removedNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.removed...)
}

// TestRegistry_RunningNamesFiltersStopped verifies the status list shows only
// sidecars that are actually running — registered-but-stopped (e.g. on-demand
// containers idle/never-started) must NOT appear. names() returns all
// registered; runningNames() must filter.
func TestRegistry_RunningNamesFiltersStopped(t *testing.T) {
	ctl := newFakeCtl()
	ctl.run["grubdrops-browser-ttik3r"] = true   // running
	ctl.run["grubdrops-browser-phluses"] = false // registered but stopped
	reg := newSidecarRegistry(ctl, "grubdrops-browser-{slug}", 9090, time.Minute)
	reg.register("acc1", "TTik3r")
	reg.register("acc2", "Phluses")

	got := reg.runningNames(context.Background())
	want := []string{"grubdrops-browser-ttik3r:9090"}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("runningNames = %v, want %v (only the running sidecar)", got, want)
	}
}

// TestRegistry_RunningNamesNilCtl: with no docker controller, report nothing
// running rather than the full registered list.
func TestRegistry_RunningNamesNilCtl(t *testing.T) {
	reg := newSidecarRegistry(nil, "grubdrops-browser-{slug}", 9090, time.Minute)
	reg.register("acc1", "TTik3r")
	if got := reg.runningNames(context.Background()); len(got) != 0 {
		t.Fatalf("runningNames with nil ctl = %v, want empty", got)
	}
}

func TestRegistry_ReaperStopsIdleRunning(t *testing.T) {
	ctl := newFakeCtl()
	ctl.run["grubdrops-browser-ttik3r"] = true
	reg := newSidecarRegistry(ctl, "grubdrops-browser-{slug}", 9090, 50*time.Millisecond)
	reg.register("acc1", "TTik3r")
	// lastActive far in the past → reaper should stop it.
	reg.touchAt("acc1", time.Now().Add(-time.Hour))

	reg.reapOnce(context.Background())
	if ctl.stopCount() != 1 {
		t.Fatalf("want 1 stop, got %d", ctl.stopCount())
	}
}

func TestRegistry_ReaperKeepsFreshAndStopped(t *testing.T) {
	ctl := newFakeCtl()
	ctl.run["grubdrops-browser-ttik3r"] = true   // fresh, running
	ctl.run["grubdrops-browser-phluses"] = false // idle but already stopped
	reg := newSidecarRegistry(ctl, "grubdrops-browser-{slug}", 9090, 50*time.Millisecond)
	reg.register("acc1", "TTik3r")
	reg.register("acc2", "Phluses")
	reg.touchAt("acc1", time.Now())                 // fresh
	reg.touchAt("acc2", time.Now().Add(-time.Hour)) // idle but stopped

	reg.reapOnce(context.Background())
	if ctl.stopCount() != 0 {
		t.Fatalf("want 0 stops, got %d", ctl.stopCount())
	}
}

func TestRegistry_EnsureUpCreatesWhenAbsent(t *testing.T) {
	ctl := newFakeCtl() // no container exists yet
	reg := newSidecarRegistry(ctl, "grubdrops-browser-{slug}", 9090, time.Minute).
		withCreate("ghcr.io/x:latest", "")
	reg.register("acc1", "TTik3r")

	if err := reg.ensureUp(context.Background(), "acc1", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("ensureUp: %v", err)
	}
	if ctl.createCount() != 1 {
		t.Fatalf("want 1 create when absent, got %d", ctl.createCount())
	}
	spec := ctl.created[0]
	if spec.Name != "grubdrops-browser-ttik3r" || spec.Image != "ghcr.io/x:latest" {
		t.Fatalf("bad create spec: %+v", spec)
	}
	if spec.Labels[dockerctl.LabelManaged] != "true" || spec.Labels[dockerctl.LabelAccount] != "acc1" || spec.Labels[dockerctl.LabelSlug] != "ttik3r" {
		t.Fatalf("bad labels: %v", spec.Labels)
	}
	if ctl.startCount() != 1 {
		t.Fatalf("want start after create, got %d", ctl.startCount())
	}
}

func TestRegistry_EnsureUpStartOnlyWhenPresent(t *testing.T) {
	ctl := newFakeCtl()
	ctl.run["grubdrops-browser-ttik3r"] = false // exists, stopped (hand-defined)
	reg := newSidecarRegistry(ctl, "grubdrops-browser-{slug}", 9090, time.Minute).
		withCreate("ghcr.io/x:latest", "")
	reg.register("acc1", "TTik3r")

	if err := reg.ensureUp(context.Background(), "acc1", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("ensureUp: %v", err)
	}
	if ctl.createCount() != 0 {
		t.Fatalf("existing container must NOT be recreated, got %d creates", ctl.createCount())
	}
	if ctl.startCount() != 1 {
		t.Fatalf("want 1 start, got %d", ctl.startCount())
	}
}

func TestRegistry_SweepRemovesOrphanSparesActiveAndUnlabeled(t *testing.T) {
	ctl := newFakeCtl()
	ctl.containers = []dockerctl.ContainerInfo{
		// managed, account still active → spare
		{Name: "grubdrops-browser-ttik3r", Labels: map[string]string{
			dockerctl.LabelManaged: "true", dockerctl.LabelAccount: "acc1", dockerctl.LabelSlug: "ttik3r"}},
		// managed, account gone → REMOVE
		{Name: "grubdrops-browser-gone", Labels: map[string]string{
			dockerctl.LabelManaged: "true", dockerctl.LabelAccount: "acc_dead", dockerctl.LabelSlug: "gone"}},
		// unlabeled hand-defined sidecar → must never even be listed → spare
		{Name: "grubdrops-browser-phluses", Labels: map[string]string{}},
	}
	reg := newSidecarRegistry(ctl, "grubdrops-browser-{slug}", 9090, time.Minute).
		withCreate("ghcr.io/x:latest", "")
	reg.register("acc1", "TTik3r") // only acc1 is live

	reg.sweepOrphans(context.Background())

	got := ctl.removedNames()
	if len(got) != 1 || got[0] != "grubdrops-browser-gone" {
		t.Fatalf("want only orphan removed, got %v", got)
	}
}

func TestRegistry_SweepNeverRemovesUnlabeled(t *testing.T) {
	ctl := newFakeCtl()
	// Even if an unlabeled container slips into the (unfiltered) list, the
	// guard must skip it. Simulate a List that returns it despite the filter.
	ctl.containers = []dockerctl.ContainerInfo{
		{Name: "grubdrops-browser-phluses", Labels: map[string]string{}},
		{Name: "random-unrelated", Labels: nil},
	}
	reg := newSidecarRegistry(ctl, "grubdrops-browser-{slug}", 9090, time.Minute).
		withCreate("ghcr.io/x:latest", "")
	// no active accounts at all
	reg.sweepOrphans(context.Background())
	if got := ctl.removedNames(); len(got) != 0 {
		t.Fatalf("unlabeled containers must never be removed, got %v", got)
	}
}

func TestRegistry_NilControllerDegrades(t *testing.T) {
	reg := newSidecarRegistry(nil, "grubdrops-browser-{slug}", 9090, time.Minute)
	reg.register("acc1", "TTik3r")
	// must not panic / must be a no-op
	if err := reg.ensureUp(context.Background(), "acc1", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("nil controller ensureUp: %v", err)
	}
	reg.reapOnce(context.Background())
}
