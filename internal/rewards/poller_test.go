package rewards

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/i18n"
	"github.com/aalejandrofer/grubdrops/internal/notify"
	"github.com/aalejandrofer/grubdrops/internal/platform"
	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

const testAgeKey = "AGE-SECRET-KEY-1DZCAXYWJM6M42NSX5GR4QWZZ2JXEYKJ9ZKWYFYSNU997775JJ6XSY85FK9"

const fakeCode = "AB12-CD34-EF56"

var t0 = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func newStore(t *testing.T) *store.RewardCodes {
	t.Helper()
	require.NoError(t, i18n.Load())
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "r.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)
	for _, id := range []string{"a1", "a2"} {
		_, err = q.CreateAccount(ctx, gen.CreateAccountParams{
			ID: id, Platform: "twitch", DisplayName: id, Status: "idle",
			FingerprintJson: "{}", Enabled: 1, CreatedAt: t0.Unix(), UpdatedAt: t0.Unix(),
		})
		require.NoError(t, err)
	}
	c, err := store.NewCryptor(testAgeKey)
	require.NoError(t, err)
	return store.NewRewardCodes(q, c)
}

type sent struct {
	event  string
	fields map[string]any
}

type fakeNotifier struct {
	mu   sync.Mutex
	sent []sent
	err  error
}

func (f *fakeNotifier) Notify(_ context.Context, event string, fields map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, sent{event, fields})
	return nil
}

func (f *fakeNotifier) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func poller(st Store, n Notifier, c *clock, targets ...Target) *Poller {
	return &Poller{
		Store:    st,
		Notifier: n,
		Targets:  func(context.Context) ([]Target, error) { return targets, nil },
		Now:      c.Now,
		Sleep:    func(context.Context, time.Duration) bool { return true },
		Zone:     func() *time.Location { return time.UTC },
	}
}

func staticReader(calls *int, list ...platform.RewardCode) Reader {
	return func(context.Context, func(string) (bool, bool)) ([]platform.RewardCode, error) {
		*calls++
		return list, nil
	}
}

func reward(expires time.Time) platform.RewardCode {
	return platform.RewardCode{
		Key: "r1", Name: "Blue Creeper Cape", Game: "Minecraft", Code: fakeCode,
		RedeemURL: "https://www.minecraft.net/redeem", ExpiresAt: expires,
	}
}

func TestPass_NotifiesNewCodeOnce(t *testing.T) {
	st := newStore(t)
	n := &fakeNotifier{}
	c := &clock{t0}
	calls := 0
	p := poller(st, n, c, Target{AccountID: "a1", Read: staticReader(&calls, reward(t0.Add(30*24*time.Hour)))})

	require.Equal(t, 1, p.Pass(context.Background()))
	require.Equal(t, 1, n.count())
	f := n.sent[0].fields
	require.Equal(t, notify.EventRewardCode, n.sent[0].event)
	require.Equal(t, "Blue Creeper Cape", f["reward"])
	require.Equal(t, fakeCode, f["code"])
	require.Equal(t, "https://www.minecraft.net/redeem", f["redeem_url"])
	require.Equal(t, "Minecraft", f["reward_game"])
	require.Equal(t, i18n.T("en", "rewards.notify_new"), f["title"])
	require.Contains(t, f["expires"], "2026-12-01 12:00 UTC")
	require.Contains(t, f["expires"], "30")

	// Not due again until Every passes: no read, no second send.
	c.now = t0.Add(time.Hour)
	require.Equal(t, 0, p.Pass(context.Background()))
	require.Equal(t, 1, calls)
	require.Equal(t, 1, n.count())

	// Due again: reads, still no second send.
	c.now = t0.Add(DefaultEvery)
	require.Equal(t, 1, p.Pass(context.Background()))
	require.Equal(t, 2, calls)
	require.Equal(t, 1, n.count())
}

func TestPass_DedupeAcrossRestart(t *testing.T) {
	st := newStore(t)
	n := &fakeNotifier{}
	c := &clock{t0}
	calls := 0
	tg := Target{AccountID: "a1", Read: staticReader(&calls, reward(t0.Add(30*24*time.Hour)))}
	poller(st, n, c, tg).Pass(context.Background())
	require.Equal(t, 1, n.count())

	// A fresh poller on the same store, as after a restart, reads at once
	// but sends nothing new.
	c.now = t0.Add(time.Minute)
	p2 := poller(st, n, c, tg)
	require.Equal(t, 1, p2.Pass(context.Background()))
	require.Equal(t, 2, calls)
	require.Equal(t, 1, n.count())
}

func TestSweep_ReminderWindow(t *testing.T) {
	st := newStore(t)
	n := &fakeNotifier{}
	c := &clock{t0}
	calls := 0
	exp := t0.Add(5 * 24 * time.Hour)
	p := poller(st, n, c, Target{AccountID: "a1", Read: staticReader(&calls, reward(exp))})
	p.Pass(context.Background())
	require.Equal(t, 1, n.count())

	// Just outside the window: nothing.
	c.now = exp.Add(-DefaultRemindBefore - time.Minute)
	p.Sweep(context.Background())
	require.Equal(t, 1, n.count())

	// Inside the window: one reminder.
	c.now = exp.Add(-DefaultRemindBefore + time.Minute)
	p.Sweep(context.Background())
	require.Equal(t, 2, n.count())
	require.Equal(t, i18n.T("en", "rewards.notify_reminder"), n.sent[1].fields["title"])
	require.Equal(t, fakeCode, n.sent[1].fields["code"])

	// Never twice, even after a restart.
	c.now = exp.Add(-time.Hour)
	p.Sweep(context.Background())
	poller(st, n, c).Sweep(context.Background())
	require.Equal(t, 2, n.count())
}

func TestSweep_NoReminderWhenDismissedOrRedeemed(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	n := &fakeNotifier{}
	c := &clock{t0}
	exp := t0.Add(5 * 24 * time.Hour)
	calls := 0
	p := poller(st, n, c, Target{AccountID: "a1", Read: staticReader(&calls, reward(exp))})
	p.Pass(ctx)
	list, err := st.List(ctx, true)
	require.NoError(t, err)
	require.Len(t, list, 1)
	ok, err := st.SetDismissed(ctx, list[0].ID, t0)
	require.NoError(t, err)
	require.True(t, ok)

	c.now = exp.Add(-time.Hour)
	p.Sweep(ctx)
	require.Equal(t, 1, n.count())
}

func TestSweep_FirstSeenInsideWindowSendsOnce(t *testing.T) {
	st := newStore(t)
	n := &fakeNotifier{}
	c := &clock{t0}
	calls := 0
	p := poller(st, n, c, Target{AccountID: "a1", Read: staticReader(&calls, reward(t0.Add(10*time.Hour)))})
	p.Pass(context.Background())
	require.Equal(t, 1, n.count())
	require.Equal(t, i18n.T("en", "rewards.notify_new"), n.sent[0].fields["title"])
	c.now = t0.Add(5 * time.Hour)
	p.Sweep(context.Background())
	require.Equal(t, 1, n.count())
}

func TestSweep_ExpiredAtFirstSightIsSilent(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	n := &fakeNotifier{}
	c := &clock{t0}
	calls := 0
	p := poller(st, n, c, Target{AccountID: "a1", Read: staticReader(&calls, reward(t0.Add(-time.Hour)))})
	p.Pass(ctx)
	require.Equal(t, 0, n.count())
	list, err := st.List(ctx, false)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.True(t, list[0].NotifiedAt.IsZero(), "nothing went out")
	require.True(t, t0.Equal(list[0].NotifySkippedAt), "marked skipped so it never sends")
}

// gatedNotifier is a fakeNotifier whose reward code event can be turned off,
// like a notifier with no target or with the event toggled off.
type gatedNotifier struct {
	fakeNotifier
	off bool
}

func (g *gatedNotifier) Enabled(_ context.Context, event string) bool {
	return !g.off || event != notify.EventRewardCode
}

func (g *gatedNotifier) Notify(ctx context.Context, event string, fields map[string]any) error {
	if !g.Enabled(ctx, event) {
		return nil
	}
	return g.fakeNotifier.Notify(ctx, event, fields)
}

func TestSweep_DeliveredMarksNotified(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	n := &gatedNotifier{}
	c := &clock{t0}
	calls := 0
	poller(st, n, c, Target{AccountID: "a1", Read: staticReader(&calls, reward(t0.Add(30*24*time.Hour)))}).Pass(ctx)
	require.Equal(t, 1, n.count())
	list, err := st.List(ctx, false)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.True(t, t0.Equal(list[0].NotifiedAt))
	require.True(t, list[0].NotifySkippedAt.IsZero())
}

func TestSweep_NotificationsOffMarksSkipped(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	n := &gatedNotifier{off: true}
	c := &clock{t0}
	calls := 0
	exp := t0.Add(30 * 24 * time.Hour)
	p := poller(st, n, c, Target{AccountID: "a1", Read: staticReader(&calls, reward(exp))})
	p.Pass(ctx)
	require.Equal(t, 0, n.count())
	list, err := st.List(ctx, false)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.True(t, list[0].NotifiedAt.IsZero(), "nothing went out")
	require.True(t, t0.Equal(list[0].NotifySkippedAt))

	// Notifications back on: the skipped code is not sent late,
	// and gets no reminder before it expires.
	n.off = false
	c.now = t0.Add(time.Hour)
	p.Sweep(ctx)
	c.now = exp.Add(-time.Hour)
	p.Sweep(ctx)
	require.Equal(t, 0, n.count())
	list, err = st.List(ctx, false)
	require.NoError(t, err)
	require.True(t, list[0].NotifiedAt.IsZero())
	require.True(t, list[0].RemindedAt.IsZero())
}

func TestSweep_NoTargetMarksSkipped(t *testing.T) {
	ctx := context.Background()
	for name, n := range map[string]Notifier{
		"noop": &notify.NoopNotifier{},
		"event off": &notify.Multi{Targets: []notify.Named{{Name: notify.TargetDiscord, Notifier: notify.NewDiscordWebhook("http://127.0.0.1:1/x",
			&notify.VerbosityFilter{Allow: map[string]bool{notify.EventRewardCode: false}})}}},
	} {
		st := newStore(t)
		c := &clock{t0}
		calls := 0
		poller(st, n, c, Target{AccountID: "a1", Read: staticReader(&calls, reward(t0.Add(30*24*time.Hour)))}).Pass(ctx)
		list, err := st.List(ctx, false)
		require.NoError(t, err, name)
		require.Len(t, list, 1, name)
		require.True(t, list[0].NotifiedAt.IsZero(), name)
		require.True(t, t0.Equal(list[0].NotifySkippedAt), name)
	}
}

func TestNeeds_SkippedIsHandled(t *testing.T) {
	r := store.RewardCode{Code: fakeCode, ExpiresAt: t0.Add(time.Hour), NotifySkippedAt: t0.Add(-time.Hour)}
	require.Equal(t, DueNone, Needs(r, t0, DefaultRemindBefore))
	r.ExpiresAt = t0.Add(-time.Minute)
	require.Equal(t, DueNone, Needs(r, t0, DefaultRemindBefore))
}

func TestSweep_NoCodeNoNotice(t *testing.T) {
	st := newStore(t)
	n := &fakeNotifier{}
	c := &clock{t0}
	calls := 0
	rc := reward(time.Time{})
	rc.Code = ""
	poller(st, n, c, Target{AccountID: "a1", Read: staticReader(&calls, rc)}).Pass(context.Background())
	require.Equal(t, 0, n.count())
}

func TestSweep_FailedSendRetriesLater(t *testing.T) {
	st := newStore(t)
	n := &fakeNotifier{err: errors.New("discord down")}
	c := &clock{t0}
	calls := 0
	p := poller(st, n, c, Target{AccountID: "a1", Read: staticReader(&calls, reward(t0.Add(30*24*time.Hour)))})
	p.Pass(context.Background())
	require.Equal(t, 0, n.count())

	n.err = nil
	c.now = t0.Add(time.Minute)
	p.Sweep(context.Background())
	require.Equal(t, 0, n.count(), "waits after a failure")

	c.now = t0.Add(DefaultNotifyRetry)
	p.Sweep(context.Background())
	require.Equal(t, 1, n.count())
}

func TestPass_UnavailableQuiet(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	st := newStore(t)
	c := &clock{t0}
	calls := 0
	read := func(context.Context, func(string) (bool, bool)) ([]platform.RewardCode, error) {
		calls++
		return nil, platform.ErrRewardCodesUnavailable
	}
	p := poller(st, &fakeNotifier{}, c, Target{AccountID: "a1", Read: read})
	p.Pass(context.Background())
	require.True(t, p.Unavailable("a1"))
	require.NotContains(t, buf.String(), "level=WARN")
	require.NotContains(t, buf.String(), "level=ERROR")

	// No retry for a day.
	c.now = t0.Add(23 * time.Hour)
	p.Pass(context.Background())
	require.Equal(t, 1, calls)
	c.now = t0.Add(DefaultUnavailableRetry)
	p.Pass(context.Background())
	require.Equal(t, 2, calls)
	require.Equal(t, 1, strings.Count(buf.String(), "cannot read reward codes"), "logs once")
}

func TestPass_FailureBacksOff(t *testing.T) {
	st := newStore(t)
	c := &clock{t0}
	calls := 0
	read := func(context.Context, func(string) (bool, bool)) ([]platform.RewardCode, error) {
		calls++
		return nil, errors.New("timeout")
	}
	p := poller(st, &fakeNotifier{}, c, Target{AccountID: "a1", Read: read})
	p.Pass(context.Background())
	c.now = t0.Add(DefaultRetry - time.Second)
	p.Pass(context.Background())
	require.Equal(t, 1, calls)
	c.now = t0.Add(DefaultRetry)
	p.Pass(context.Background())
	require.Equal(t, 2, calls)
	// Second failure doubles the wait.
	c.now = c.now.Add(DefaultRetry)
	p.Pass(context.Background())
	require.Equal(t, 2, calls)
	c.now = c.now.Add(DefaultRetry)
	p.Pass(context.Background())
	require.Equal(t, 3, calls)
}

func TestPass_KnownAndSpacing(t *testing.T) {
	st := newStore(t)
	c := &clock{t0}
	var seen []bool
	var sleeps []time.Duration
	read := func(_ context.Context, known func(string) (bool, bool)) ([]platform.RewardCode, error) {
		code, exp := known("r1")
		seen = append(seen, code && exp)
		return []platform.RewardCode{reward(t0.Add(30 * 24 * time.Hour))}, nil
	}
	p := poller(st, &fakeNotifier{}, c,
		Target{AccountID: "a1", Read: read},
		Target{AccountID: "a2", Read: read},
	)
	p.Sleep = func(_ context.Context, d time.Duration) bool { sleeps = append(sleeps, d); return true }
	require.Equal(t, 2, p.Pass(context.Background()))
	require.Equal(t, []time.Duration{DefaultSpacing}, sleeps, "one gap between two reads")
	c.now = t0.Add(DefaultEvery)
	p.Pass(context.Background())
	require.Equal(t, []bool{false, false, true, true}, seen, "known is per account")
}

func TestPass_NeverLogsCode(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	st := newStore(t)
	c := &clock{t0}
	calls := 0
	n := &fakeNotifier{err: errors.New("down")}
	p := poller(st, n, c, Target{AccountID: "a1", Read: staticReader(&calls, reward(t0.Add(time.Hour)))})
	p.Pass(context.Background())
	n.err = nil
	c.now = t0.Add(DefaultNotifyRetry)
	p.Pass(context.Background())
	require.NotEmpty(t, buf.String())
	require.NotContains(t, buf.String(), fakeCode)
}

func TestExpiryText(t *testing.T) {
	require.NoError(t, i18n.Load())
	at := time.Date(2026, 11, 10, 18, 30, 0, 0, time.UTC)
	got := ExpiryText("en", at, at.Add(-3*24*time.Hour-time.Hour), time.UTC)
	require.Equal(t, "2026-11-10 18:30 UTC · "+strings.ReplaceAll(i18n.T("en", "rewards.left_days"), "{n}", "3"), got)
	got = ExpiryText("en", at, at.Add(-5*time.Hour), time.UTC)
	require.Contains(t, got, strings.ReplaceAll(i18n.T("en", "rewards.left_hours"), "{n}", "5"))
	got = ExpiryText("en", at, at.Add(-10*time.Second), time.UTC)
	require.Contains(t, got, strings.ReplaceAll(i18n.T("en", "rewards.left_minutes"), "{n}", "1"))
}
