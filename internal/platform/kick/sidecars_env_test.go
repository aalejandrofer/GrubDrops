package kick

import "testing"

func TestSidecarEnv(t *testing.T) {
	if sidecarEnv("") != nil {
		t.Fatal("expected nil env when no proxy")
	}
	env := sidecarEnv("http://p:8080")
	if len(env) != 1 || env[0] != "GRUB_SIDECAR_PROXY=http://p:8080" {
		t.Fatalf("got %v", env)
	}
}
