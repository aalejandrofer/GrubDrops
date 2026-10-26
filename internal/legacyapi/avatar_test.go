package legacyapi

import "testing"

func TestAvatarSrc(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		url      string
		want     string
	}{
		{"empty falls back to letter circle", "twitch", "", ""},
		{"whitespace-only treated as empty", "kick", "   ", ""},
		{
			"twitch direct CDN url",
			"twitch",
			"https://static-cdn.jtvnw.net/jtv_user_pictures/abc-profile_image-300x300.png",
			"https://static-cdn.jtvnw.net/jtv_user_pictures/abc-profile_image-300x300.png",
		},
		{
			"kick proxied through /img/kick",
			"kick",
			"https://files.kick.com/images/user/42/profile_image/x.webp",
			"/img/kick?u=https%3A%2F%2Ffiles.kick.com%2Fimages%2Fuser%2F42%2Fprofile_image%2Fx.webp",
		},
		{
			"kick relative path proxied",
			"kick",
			"images/user/42/profile_image/x.webp",
			"/img/kick?u=images%2Fuser%2F42%2Fprofile_image%2Fx.webp",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := avatarSrc(tt.platform, tt.url); got != tt.want {
				t.Fatalf("avatarSrc(%q,%q) = %q, want %q", tt.platform, tt.url, got, tt.want)
			}
		})
	}
}
