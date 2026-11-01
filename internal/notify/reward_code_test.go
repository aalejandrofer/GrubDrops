package notify

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testCode = "AB12-CD34-EF56"

func rewardFields() map[string]any {
	return map[string]any{
		"title":         "New reward code",
		"reward":        "Blue Creeper Cape",
		"reward_game":   "Minecraft",
		"account_label": "Alice",
		"code":          testCode,
		"redeem_url":    "https://www.minecraft.net/redeem",
		"expires":       "2026-12-01 12:00 UTC · 30 days left",
	}
}

func TestBuildEmbed_RewardCode(t *testing.T) {
	e := buildEmbed(EventRewardCode, rewardFields())
	assert.Equal(t, map[string]any{"name": "New reward code"}, e["author"])
	assert.Equal(t, "Blue Creeper Cape", e["title"])
	assert.Equal(t, "https://www.minecraft.net/redeem", e["url"])
	fields := e["fields"].([]map[string]any)
	got := map[string]string{}
	for _, f := range fields {
		got[f["name"].(string)] = f["value"].(string)
	}
	assert.Equal(t, map[string]string{
		"Account": "Alice",
		"Game":    "Minecraft",
		"Code":    "`" + testCode + "`",
		"Redeem":  "[Open](https://www.minecraft.net/redeem)",
		"Expires": "2026-12-01 12:00 UTC · 30 days left",
	}, got)
}

func TestBuildEmbed_RewardCodeBadLink(t *testing.T) {
	f := rewardFields()
	f["redeem_url"] = "javascript:alert(1)"
	delete(f, "expires")
	e := buildEmbed(EventRewardCode, f)
	assert.Nil(t, e["url"])
	for _, fl := range e["fields"].([]map[string]any) {
		assert.NotEqual(t, "Redeem", fl["name"])
		assert.NotEqual(t, "Expires", fl["name"])
	}
}

func TestValidLinkURL(t *testing.T) {
	assert.True(t, validLinkURL("https://www.twitch.tv/drops/inventory"))
	assert.True(t, validLinkURL("http://example.com/x?a=1"))
	for _, bad := range []string{"", "ftp://x", "javascript:alert(1)", "https://x/a b", "https://x/(y)", "https://x/`"} {
		assert.False(t, validLinkURL(bad), bad)
	}
}

func TestTelegramText_RewardCode(t *testing.T) {
	got := buildTelegramText(EventRewardCode, rewardFields())
	for _, want := range []string{
		"New reward code", "Blue Creeper Cape", "Account: Alice", "Game: Minecraft",
		"Code: <code>" + testCode + "</code>",
		`<a href="https://www.minecraft.net/redeem">Redeem</a>`,
		"Expires: 2026-12-01 12:00 UTC · 30 days left",
	} {
		assert.Contains(t, got, want)
	}
	f := rewardFields()
	f["reward"] = "<b>Cape</b>"
	f["redeem_url"] = "javascript:x"
	got = buildTelegramText(EventRewardCode, f)
	assert.Contains(t, got, "&lt;b&gt;Cape&lt;/b&gt;")
	assert.NotContains(t, got, "href")
}

func TestNoop_RedactsCode(t *testing.T) {
	var buf bytes.Buffer
	n := &NoopNotifier{Logger: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))}
	require.NoError(t, n.Notify(context.Background(), EventRewardCode, rewardFields()))
	assert.NotContains(t, buf.String(), testCode)
	assert.True(t, strings.Contains(buf.String(), "[redacted]"))
	assert.Contains(t, buf.String(), "Blue Creeper Cape")
}

func TestRewardCode_NotAnAlert(t *testing.T) {
	assert.False(t, IsAlert(EventRewardCode))
}
