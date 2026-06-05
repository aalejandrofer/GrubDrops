package log

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_WritesJSONToWriter(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, "info")
	l.Info("hello", "account", "acc1")

	out := buf.String()
	assert.Contains(t, out, `"msg":"hello"`)
	assert.Contains(t, out, `"account":"acc1"`)
	assert.Contains(t, out, `"level":"INFO"`)
}

func TestRingBuffer_KeepsLastN(t *testing.T) {
	rb := NewRing(3)
	for i := 0; i < 5; i++ {
		rb.Push(LogLine{Msg: "m"})
	}
	require.Equal(t, 3, len(rb.Snapshot()))
}

func TestNewWithRing_WritesToBoth(t *testing.T) {
	var buf bytes.Buffer
	rb := NewRing(10)
	l := NewWithRing(&buf, "debug", rb)
	l.Info("ping")

	assert.True(t, strings.Contains(buf.String(), "ping"))
	assert.Equal(t, 1, len(rb.Snapshot()))
	assert.Equal(t, "ping", rb.Snapshot()[0].Msg)
}

// Regression: structured emitters (e.g. the watcher) attach a "kind"
// attribute; the ringHandler must capture it on LogLine.Kind so the
// dashboard can color-code without falling back to substring guesswork.
func TestNewWithRing_ExtractsKindAttr(t *testing.T) {
	var buf bytes.Buffer
	rb := NewRing(10)
	l := NewWithRing(&buf, "debug", rb)
	l.Info("watcher claim recorded", "kind", "claim", "account", "acc1", "benefit", "b1")

	snap := rb.Snapshot()
	require.Equal(t, 1, len(snap))
	assert.Equal(t, "claim", snap[0].Kind)
	assert.Equal(t, "acc1", snap[0].Fields["account"])
}

func TestPushEvent_AppendsAndCopiesFields(t *testing.T) {
	rb := NewRing(4)
	fields := map[string]any{"account": "acc1"}
	rb.PushEvent(KindClaim, "", "claim recorded", fields)
	// Mutating the caller's map must not corrupt the ring entry.
	fields["account"] = "MUTATED"

	snap := rb.Snapshot()
	require.Equal(t, 1, len(snap))
	assert.Equal(t, KindClaim, snap[0].Kind)
	assert.Equal(t, "INFO", snap[0].Level)
	assert.Equal(t, "acc1", snap[0].Fields["account"])
}

// A nil receiver must be a no-op — callers shouldn't have to guard
// each emission.
func TestPushEvent_NilReceiverIsNoop(t *testing.T) {
	var rb *Ring
	assert.NotPanics(t, func() {
		rb.PushEvent(KindClaim, "", "msg", nil)
	})
}

func TestNewRingFromEnv_DefaultWhenUnset(t *testing.T) {
	t.Setenv("MINER_LOG_RING", "")
	rb := NewRingFromEnv(7)
	assert.Equal(t, 7, rb.size)
}

func TestNewRingFromEnv_HonoursValidEnv(t *testing.T) {
	t.Setenv("MINER_LOG_RING", "42")
	rb := NewRingFromEnv(7)
	assert.Equal(t, 42, rb.size)
}

func TestNewRingFromEnv_FallsBackOnGarbage(t *testing.T) {
	t.Setenv("MINER_LOG_RING", "not-a-number")
	rb := NewRingFromEnv(7)
	assert.Equal(t, 7, rb.size)
}
