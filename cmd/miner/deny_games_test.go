package main

import "testing"

// With no game whitelist a points-only/force-watch account must mine nothing.
// The raw nil closures would make the watcher mine every live drop.
func TestDenyAllIfNoGames(t *testing.T) {
	allow, rank := denyAllIfNoGames(nil, nil)
	if allow("Minecraft") || allow("") {
		t.Fatal("empty whitelist must not allow any game")
	}
	if rank("Minecraft") != 1<<30 {
		t.Fatal("unlisted games rank last")
	}

	keep := func(g string) bool { return g == "Minecraft" }
	got, _ := denyAllIfNoGames(keep, func(string) int { return 0 })
	if !got("Minecraft") || got("Other") {
		t.Fatal("a real whitelist must pass through untouched")
	}
}
