package authcheck

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// fakeNotifier records every Notify call.
type fakeNotifier struct {
	mu    sync.Mutex
	calls []map[string]any
}

func (f *fakeNotifier) Notify(_ context.Context, _ string, fields map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	copied := map[string]any{}
	for k, v := range fields {
		copied[k] = v
	}
	f.calls = append(f.calls, copied)
	return nil
}

func (f *fakeNotifier) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func testQueries(t *testing.T) *gen.Queries {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "authcheck.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return gen.New(db)
}

// An hourly sweep against a dead account must notify ONCE, not once per
// sweep. Level-triggering here would send 24 messages a day per broken
// account — the same defect shape as the Kick claim spam fixed in v1.3.11.
func TestPersist_EdgeTriggeredOnly(t *testing.T) {
	ctx := context.Background()
	q := testQueries(t)
	fn := &fakeNotifier{}
	c := New(q, nil, nil).WithNotifier(fn)

	// none -> OK: healthy first observation, no news to report.
	c.persist(ctx, "acc_1", Result{OK: true, Msg: "ok"})
	if got := fn.count(); got != 0 {
		t.Fatalf("none->OK should not notify, got %d sends", got)
	}

	// OK -> fail: the account just died. Notify.
	c.persist(ctx, "acc_1", Result{OK: false, Msg: "401 unauthorized"})
	if got := fn.count(); got != 1 {
		t.Fatalf("OK->fail should notify once, got %d sends", got)
	}

	// fail -> fail: still dead, already told them. Silence.
	c.persist(ctx, "acc_1", Result{OK: false, Msg: "401 unauthorized"})
	c.persist(ctx, "acc_1", Result{OK: false, Msg: "401 unauthorized"})
	if got := fn.count(); got != 1 {
		t.Fatalf("fail->fail must NOT re-fire, got %d sends", got)
	}

	// fail -> OK: recovered. Notify.
	c.persist(ctx, "acc_1", Result{OK: true, Msg: "ok"})
	if got := fn.count(); got != 2 {
		t.Fatalf("fail->OK should notify recovery, got %d sends", got)
	}
}

// A first-ever observation that is already failing must notify — otherwise a
// miner started with an already-expired session stays silent forever.
func TestPersist_FirstEverFailureNotifies(t *testing.T) {
	ctx := context.Background()
	fn := &fakeNotifier{}
	c := New(testQueries(t), nil, nil).WithNotifier(fn)

	c.persist(ctx, "acc_1", Result{OK: false, Msg: "no session"})
	if got := fn.count(); got != 1 {
		t.Fatalf("first-ever fail should notify, got %d sends", got)
	}
}

// The notification must say WHICH account and WHY, and carry the account id
// in the "account" field so notify.AccountRoutedNotifier can route it to a
// per-account webhook.
func TestPersist_FieldsCarryAccountAndReason(t *testing.T) {
	ctx := context.Background()
	fn := &fakeNotifier{}
	c := New(testQueries(t), nil, nil).WithNotifier(fn)

	c.persist(ctx, "acc_abc", Result{OK: false, Msg: "401 unauthorized"})
	if fn.count() != 1 {
		t.Fatalf("expected 1 send, got %d", fn.count())
	}
	got := fn.calls[0]
	if got["account"] != "acc_abc" {
		t.Fatalf("account field = %v, want acc_abc", got["account"])
	}
	if got["ok"] != false {
		t.Fatalf("ok field = %v, want false", got["ok"])
	}
	if got["reason"] != "401 unauthorized" {
		t.Fatalf("reason field = %v, want the Result.Msg", got["reason"])
	}
}

// Notifications are best-effort: a send failure must never stop the health
// result from being persisted, because auth health is the source of truth.
func TestPersist_SendFailureStillPersists(t *testing.T) {
	ctx := context.Background()
	q := testQueries(t)
	c := New(q, nil, nil).WithNotifier(errNotifier{})

	c.persist(ctx, "acc_1", Result{OK: false, Msg: "boom"})
	res, ok := Load(ctx, q, "acc_1")
	if !ok {
		t.Fatal("result was not persisted after a failed send")
	}
	if res.OK {
		t.Fatal("persisted result should be a failure")
	}
}

type errNotifier struct{}

func (errNotifier) Notify(context.Context, string, map[string]any) error {
	return errors.New("webhook down")
}

// A miner with no webhook configured has a nil notifier and must behave
// exactly as before: persist, no panic.
func TestPersist_NilNotifierIsNoop(t *testing.T) {
	ctx := context.Background()
	q := testQueries(t)
	c := New(q, nil, nil)

	c.persist(ctx, "acc_1", Result{OK: false, Msg: "boom"})
	if _, ok := Load(ctx, q, "acc_1"); !ok {
		t.Fatal("result was not persisted with a nil notifier")
	}
}

// The notification should name the account the way the operator sees it in
// the UI, not just by opaque id.
func TestPersist_FieldsCarryPlatformAndName(t *testing.T) {
	ctx := context.Background()
	fn := &fakeNotifier{}
	c := New(testQueries(t), nil, nil).WithNotifier(fn)

	c.persistWithMeta(ctx, "acc_abc", "kick", "MyKickAccount", Result{OK: false, Msg: "401"})
	if fn.count() != 1 {
		t.Fatalf("expected 1 send, got %d", fn.count())
	}
	got := fn.calls[0]
	if got["platform"] != "kick" {
		t.Fatalf("platform field = %v, want kick", got["platform"])
	}
	if got["name"] != "MyKickAccount" {
		t.Fatalf("name field = %v, want MyKickAccount", got["name"])
	}
}
