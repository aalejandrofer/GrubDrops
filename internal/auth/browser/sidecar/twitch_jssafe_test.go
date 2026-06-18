package sidecar

import (
	"strings"
	"testing"
)

// TestJSSafeJSON verifies that values embedded into the eval script can't
// break out of the surrounding JS literal. encoding/json escapes <, >, &
// but leaves U+2028/U+2029 raw — those are JS line terminators, so a game
// name or drop title carrying one must come back escaped.
func TestJSSafeJSON(t *testing.T) {
	const ls = "\u2028" // LINE SEPARATOR
	const ps = "\u2029" // PARAGRAPH SEPARATOR

	got := jsSafeJSON([]string{"Apex" + ls + "Legends", "Foo" + ps + "Bar"})

	if strings.Contains(got, ls) || strings.Contains(got, ps) {
		t.Fatalf("raw line separator survived: %q", got)
	}
	if !strings.Contains(got, `\u2028`) || !strings.Contains(got, `\u2029`) {
		t.Fatalf("expected escaped separators in %q", got)
	}
	// HTML-significant runes stay escaped by encoding/json's default.
	if strings.Contains(jsSafeJSON([]string{"</script>"}), "<") {
		t.Fatalf("raw < survived HTML escaping")
	}
	// Marshal failure falls back to a valid empty array literal.
	if jsSafeJSON(make(chan int)) != "[]" {
		t.Fatalf("expected [] fallback on marshal error")
	}
}
