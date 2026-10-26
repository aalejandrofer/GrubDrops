package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const tgToken = "123456:ABCdefGHIjklMNOpqrSTUvwxYZ0123456789"

func TestSettings_TelegramSaveAndMask(t *testing.T) {
	e := newSettingsEnv(t)
	code, b := e.do("PUT", "/settings", map[string]any{
		"telegram_bot_token": tgToken, "telegram_chat_id": "-1001234567890",
	})
	require.Equal(t, 200, code)
	assert.NotContains(t, string(b), "ABCdefGHI")
	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, map[string]any{
		"configured": true, "token_hint": "123456:••••", "chat_id": "-1001234567890",
	}, got["telegram"])
	stored, _ := e.st.TelegramBotToken(context.Background())
	assert.Equal(t, tgToken, stored)
	assert.Equal(t, 1, e.updates)

	// GET also never returns the token.
	code, b = e.do("GET", "/settings", nil)
	require.Equal(t, 200, code)
	assert.NotContains(t, string(b), "ABCdefGHI")

	// Sending the GET body back with only a new chat ID keeps the token.
	got["telegram_chat_id"] = "@grubdrops_alerts"
	code, _ = e.do("PUT", "/settings", got)
	require.Equal(t, 200, code)
	stored, _ = e.st.TelegramBotToken(context.Background())
	assert.Equal(t, tgToken, stored)
	chat, _ := e.st.TelegramChatID(context.Background())
	assert.Equal(t, "@grubdrops_alerts", chat)

	// Empty values clear it.
	code, b = e.do("PUT", "/settings", map[string]any{"telegram_bot_token": "", "telegram_chat_id": ""})
	require.Equal(t, 200, code)
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, false, got["telegram"].(map[string]any)["configured"])
}

func TestSettings_TelegramInvalid(t *testing.T) {
	e := newSettingsEnv(t)
	for _, body := range []map[string]any{
		{"telegram_bot_token": "not-a-token"},
		{"telegram_bot_token": "123:abc/../x"},
		{"telegram_chat_id": "abc"},
		{"telegram_chat_id": "@x"},
	} {
		code, _ := e.do("PUT", "/settings", body)
		assert.Equal(t, 400, code, body)
	}
	tok, _ := e.st.TelegramBotToken(context.Background())
	assert.Empty(t, tok)
	assert.Equal(t, 0, e.updates)
}

func TestSettings_TelegramEnvFallback(t *testing.T) {
	e := newSettingsEnv(t)
	e.h = func() http.Handler {
		s := &server{d: Deps{Settings: e.st, Notifier: e.n,
			TelegramTokenFallback: tgToken, TelegramChatFallback: "42"}}
		r := chi.NewRouter()
		s.routesSettings(r)
		return r
	}()
	code, b := e.do("GET", "/settings", nil)
	require.Equal(t, 200, code)
	var got settingsBody
	require.NoError(t, json.Unmarshal(b, &got))
	assert.True(t, got.Telegram.Configured)
	assert.Equal(t, "", got.Telegram.TokenHint)
	assert.Equal(t, "42", got.Telegram.ChatID)
	assert.NotContains(t, string(b), "ABCdefGHI")
	code, _ = e.do("POST", "/settings/notify-test?target=telegram", nil)
	assert.Equal(t, 204, code)
	assert.Equal(t, []string{"telegram"}, e.n.targets)
}

func TestNotifyTest_Target(t *testing.T) {
	e := newSettingsEnv(t)
	ctx := context.Background()
	require.NoError(t, e.st.SetGlobalDiscordWebhook(ctx, "https://discord.com/api/webhooks/1/x"))

	// Telegram not configured yet.
	code, _ := e.do("POST", "/settings/notify-test?target=telegram", nil)
	assert.Equal(t, 400, code)
	code, _ = e.do("POST", "/settings/notify-test?target=slack", nil)
	assert.Equal(t, 400, code)

	code, _ = e.do("POST", "/settings/notify-test?target=discord", nil)
	assert.Equal(t, 204, code)

	require.NoError(t, e.st.SetTelegramBotToken(ctx, tgToken))
	require.NoError(t, e.st.SetTelegramChatID(ctx, "42"))
	code, _ = e.do("POST", "/settings/notify-test?target=telegram", nil)
	assert.Equal(t, 204, code)
	code, _ = e.do("POST", "/settings/notify-test", nil)
	assert.Equal(t, 204, code)
	assert.Equal(t, []string{"discord", "telegram", ""}, e.n.targets)
}

func TestNotifyTest_TelegramOnly(t *testing.T) {
	e := newSettingsEnv(t)
	ctx := context.Background()
	require.NoError(t, e.st.SetTelegramBotToken(ctx, tgToken))
	require.NoError(t, e.st.SetTelegramChatID(ctx, "42"))
	code, _ := e.do("POST", "/settings/notify-test", nil)
	assert.Equal(t, 204, code)
	code, _ = e.do("POST", "/settings/notify-test?target=discord", nil)
	assert.Equal(t, 400, code)
}
