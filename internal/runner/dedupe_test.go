package runner

import "testing"

func TestOnceLog(t *testing.T) {
	var o onceLog
	if !o.Changed("a") {
		t.Fatal("first key must log")
	}
	if o.Changed("a") || o.Changed("a") {
		t.Fatal("repeat must be suppressed")
	}
	if !o.Changed("b") {
		t.Fatal("changed key must log")
	}
	if o.Changed("b") {
		t.Fatal("repeat of b must be suppressed")
	}
	if n := o.Reset(); n != 1 {
		t.Fatalf("suppressed count = %d, want 1", n)
	}
	if !o.Changed("b") {
		t.Fatal("after reset the same key logs again")
	}
	if n := o.Reset(); n != 0 {
		t.Fatalf("count = %d, want 0", n)
	}
}
