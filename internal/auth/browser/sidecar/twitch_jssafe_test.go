package sidecar

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// TestJSB64JSON verifies that values embedded into the claim eval script are
// base64-encoded so attacker-influenced game names / drop titles can never
// break out of the surrounding JS literal, and that the payload round-trips
// (including line terminators and non-ASCII / CJK titles).
func TestJSB64JSON(t *testing.T) {
	ls := string(rune(0x2028)) // LINE SEPARATOR
	ps := string(rune(0x2029)) // PARAGRAPH SEPARATOR
	in := []string{"Apex" + ls + "Legends", "Foo" + ps + "Bar", "O'Brien", "</script>", "原神"}

	got := jsB64JSON(in)

	// Output must be pure base64 — no quote, no line terminator, nothing
	// that could escape the enclosing '...' in the script.
	if strings.ContainsAny(got, "'\"`<>"+ls+ps) {
		t.Fatalf("base64 output contains a break-out character: %q", got)
	}

	// Round-trips back to the exact input.
	raw, err := base64.StdEncoding.DecodeString(got)
	if err != nil {
		t.Fatalf("not valid base64: %v", err)
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decoded payload is not valid JSON: %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("round-trip length mismatch: got %v want %v", out, in)
	}
	for i := range in {
		if out[i] != in[i] {
			t.Fatalf("round-trip mismatch at %d: got %q want %q", i, out[i], in[i])
		}
	}

	// Marshal failure falls back to a base64-encoded empty array.
	fb, _ := base64.StdEncoding.DecodeString(jsB64JSON(make(chan int)))
	if string(fb) != "[]" {
		t.Fatalf("expected [] fallback on marshal error, got %q", fb)
	}
}
