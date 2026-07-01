package timeutil

import (
	"testing"
	"time"
)

func TestResolve_SettingWins(t *testing.T) {
	loc := Resolve("Asia/Shanghai", "America/New_York")
	if loc.String() != "Asia/Shanghai" {
		t.Fatalf("setting should win, got %q", loc.String())
	}
}

func TestResolve_FallsBackToEnv(t *testing.T) {
	loc := Resolve("", "America/New_York")
	if loc.String() != "America/New_York" {
		t.Fatalf("expected env fallback, got %q", loc.String())
	}
}

func TestResolve_InvalidSettingFallsBackToEnv(t *testing.T) {
	loc := Resolve("Not/AZone", "America/New_York")
	if loc.String() != "America/New_York" {
		t.Fatalf("invalid setting should fall back to env, got %q", loc.String())
	}
}

func TestResolve_DefaultsToUTC(t *testing.T) {
	if loc := Resolve("", ""); loc.String() != "UTC" {
		t.Fatalf("expected UTC default, got %q", loc.String())
	}
	if loc := Resolve("garbage", "also-garbage"); loc.String() != "UTC" {
		t.Fatalf("expected UTC when both invalid, got %q", loc.String())
	}
}

func TestValid(t *testing.T) {
	if !Valid("Europe/Madrid") {
		t.Error("Europe/Madrid should be valid")
	}
	if !Valid("UTC") {
		t.Error("UTC should be valid")
	}
	if Valid("Nope/Nope") {
		t.Error("Nope/Nope should be invalid")
	}
	// Empty is treated as valid (means "unset → use env/UTC").
	if !Valid("") {
		t.Error("empty should be valid (unset)")
	}
}

func TestZone_DefaultsUTCAndNeverNil(t *testing.T) {
	var z *Zone = NewZone(nil)
	if z.Location() != time.UTC {
		t.Fatalf("nil zone should default to UTC, got %v", z.Location())
	}
	if z.Name() != "UTC" {
		t.Fatalf("expected UTC name, got %q", z.Name())
	}
}

func TestZone_SetIsLive(t *testing.T) {
	z := NewZone(time.UTC)
	shanghai, _ := time.LoadLocation("Asia/Shanghai")
	z.Set(shanghai)
	if z.Location().String() != "Asia/Shanghai" {
		t.Fatalf("Set did not take effect, got %q", z.Location().String())
	}
	if z.Name() != "Asia/Shanghai" {
		t.Fatalf("Name after Set = %q", z.Name())
	}
	// Setting nil is ignored (stays on last good zone), never nil-panics.
	z.Set(nil)
	if z.Location().String() != "Asia/Shanghai" {
		t.Fatalf("Set(nil) should be ignored, got %q", z.Location().String())
	}
}
