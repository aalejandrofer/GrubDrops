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
