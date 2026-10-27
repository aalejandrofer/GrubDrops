package netutil

import "testing"

func TestMaskProxyURL(t *testing.T) {
	for in, want := range map[string]string{
		"http://bob:s3cret@h:1": "http://bob:%2A%2A%2A@h:1",
		"http://bob@h:1":        "http://bob@h:1",
		"socks5://h:1":          "socks5://h:1",
		"":                      "",
	} {
		if got := MaskProxyURL(in); got != want {
			t.Errorf("MaskProxyURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRestoreProxyPassword(t *testing.T) {
	saved := "http://bob:s3cret@old:1"
	for _, c := range []struct{ in, want string }{
		{MaskProxyURL(saved), saved},
		{"http://bob:***@new:2", "http://bob:s3cret@new:2"},
		{"http://bob:other@new:2", "http://bob:other@new:2"},
		{"http://new:2", "http://new:2"},
		{"", ""},
	} {
		if got := RestoreProxyPassword(c.in, saved); got != c.want {
			t.Errorf("RestoreProxyPassword(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := RestoreProxyPassword("http://bob:***@new:2", "http://new:2"); got != "http://bob:%2A%2A%2A@new:2" && got != "http://bob:***@new:2" {
		t.Errorf("no saved password: got %q", got)
	}
}
