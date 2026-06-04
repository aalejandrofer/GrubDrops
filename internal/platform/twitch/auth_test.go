package twitch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chano-fernandez/rust-drops-miner/internal/platform"
)

func TestAuth_StartDeviceLogin_ParsesResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		assert.Equal(t, clientID, r.Form.Get("client_id"))
		assert.NotEmpty(t, r.Form.Get("scopes"))
		_, _ = w.Write([]byte(`{
			"device_code":"DEVABC123",
			"user_code":"AAAABBBB",
			"verification_uri":"https://www.twitch.tv/activate",
			"interval":5,
			"expires_in":1800
		}`))
	}))
	defer srv.Close()

	a := &authFlow{deviceURL: srv.URL, tokenURL: "", http: &http.Client{Timeout: 5 * time.Second}}
	ch, err := a.start(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "AAAABBBB", ch.UserCode)
	assert.Equal(t, "https://www.twitch.tv/activate", ch.VerificationURL)
	assert.Equal(t, 5*time.Second, ch.Interval)
	internal := ch.Internal.(deviceInternal)
	assert.Equal(t, "DEVABC123", internal.DeviceCode)
}

func TestAuth_PollDeviceLogin_ReturnsSessionOnAccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "urn:ietf:params:oauth:grant-type:device_code", r.Form.Get("grant_type"))
		assert.Equal(t, "DEVABC123", r.Form.Get("device_code"))
		_, _ = w.Write([]byte(`{
			"access_token":"acc_tok",
			"refresh_token":"ref_tok",
			"expires_in":14400
		}`))
	}))
	defer srv.Close()

	a := &authFlow{deviceURL: "", tokenURL: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	sess, err := a.poll(context.Background(), deviceInternal{DeviceCode: "DEVABC123"})
	require.NoError(t, err)
	assert.Equal(t, "acc_tok", sess.AccessToken)
	assert.Equal(t, "ref_tok", sess.RefreshToken)
	assert.True(t, sess.ExpiresAt.After(time.Now()))
}

func TestAuth_PollDeviceLogin_ReturnsPendingErr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"authorization_pending","status":400}`))
	}))
	defer srv.Close()

	a := &authFlow{tokenURL: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	_, err := a.poll(context.Background(), deviceInternal{DeviceCode: "x"})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrAuthorizationPending))
}

func TestAuth_Refresh_ReturnsNewSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "refresh_token", r.Form.Get("grant_type"))
		assert.Equal(t, "old_refresh", r.Form.Get("refresh_token"))
		_, _ = w.Write([]byte(`{
			"access_token":"new_acc",
			"refresh_token":"new_ref",
			"expires_in":14400
		}`))
	}))
	defer srv.Close()

	a := &authFlow{tokenURL: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	sess, err := a.refresh(context.Background(), platform.Session{RefreshToken: "old_refresh"})
	require.NoError(t, err)
	assert.Equal(t, "new_acc", sess.AccessToken)
	assert.Equal(t, "new_ref", sess.RefreshToken)
}

func TestAuth_FormEncoding(t *testing.T) {
	v := url.Values{"client_id": {clientID}, "scopes": {"user:read:email channel:read:redemptions"}}
	enc := v.Encode()
	assert.Contains(t, enc, "client_id="+clientID)
}
