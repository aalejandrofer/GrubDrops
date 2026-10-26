package twitch

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aalejandrofer/grubdrops/internal/platform"
)

const tok30 = "abcdefghijklmnopqrstuvwxyz0123"

func TestAndroidLoginURL_MatchesUpstream(t *testing.T) {
	// DevilXD/TwitchDropsMiner 3f6f0ef _browser_login.
	want := "https://www.twitch.tv/login?client_id=kd1unb4b3q4t58fwlpcbzcbnm76a8fp" +
		"&redirect_params=client_id%3Dkd1unb4b3q4t58fwlpcbzcbnm76a8fp%26" +
		"redirect_uri%3Dhttps%253A%252F%252Fwww.twitch.tv%252F%26response_type%3Dtoken"
	assert.Equal(t, want, AndroidLoginURL())
}

func TestExtractAccessToken(t *testing.T) {
	cases := []struct {
		name, in, want string
		err            error
	}{
		{"redirect url", "https://www.twitch.tv/#access_token=" + tok30 + "&scope=&token_type=bearer", tok30, nil},
		{"token not first", "https://www.twitch.tv/#scope=user_read&access_token=" + tok30 + "&token_type=bearer", tok30, nil},
		{"surrounding space", "  https://www.twitch.tv/#access_token=" + tok30 + "\n", tok30, nil},
		{"bare token", tok30, tok30, nil},
		{"bare token spaces", " " + tok30 + " ", tok30, nil},
		{"empty", "", "", ErrNoAccessToken},
		{"garbage", "hello world", "", ErrNoAccessToken},
		{"url without token", "https://www.twitch.tv/?error=access_denied", "", ErrNoAccessToken},
		{"short bare", "abc", "", ErrNoAccessToken},
		{"lookalike param", "https://x.test/#my_access_token=" + tok30, "", ErrNoAccessToken},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ExtractAccessToken(c.in)
			if c.err != nil {
				require.ErrorIs(t, err, c.err)
				if c.in != "" {
					assert.NotContains(t, err.Error(), c.in)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}

// validateServer answers id.twitch.tv/oauth2/validate with status and body,
// recording the Authorization and User-Agent headers.
func validateServer(t *testing.T, status int, body string) (*authFlow, *http.Header) {
	t.Helper()
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &authFlow{validateURL: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}, &got
}

func TestAndroidLogin_Success(t *testing.T) {
	a, hdr := validateServer(t, 200, `{"client_id":"kd1unb4b3q4t58fwlpcbzcbnm76a8fp","login":"alice","user_id":"1234","scopes":["user_read"],"expires_in":5000000}`)
	sess, info, err := a.androidLogin(context.Background(), "https://www.twitch.tv/#access_token="+tok30+"&scope=user_read&token_type=bearer")
	require.NoError(t, err)
	assert.Equal(t, "OAuth "+tok30, hdr.Get("Authorization"))
	assert.Equal(t, profileAndroid.UserAgent, hdr.Get("User-Agent"))
	assert.Empty(t, hdr.Get("Client-Integrity"))
	assert.Equal(t, tok30, sess.AccessToken)
	assert.Empty(t, sess.RefreshToken)
	assert.Equal(t, ClientAndroid, sess.ClientID)
	assert.Equal(t, "1234", sess.UserID)
	assert.Equal(t, "alice", info.Login)
	assert.WithinDuration(t, time.Now().Add(5000000*time.Second), sess.ExpiresAt, time.Minute)
	assert.Contains(t, sess.Cookies["twitch"], tok30, "sidecar cookie blob carries the token")
}

func TestAndroidLogin_WrongClientRejected(t *testing.T) {
	a, _ := validateServer(t, 200, `{"client_id":"ue6666qo983tsx6so1t0vnawi233wa","login":"alice","user_id":"1234","expires_in":100}`)
	_, _, err := a.androidLogin(context.Background(), tok30)
	require.ErrorIs(t, err, ErrWrongClient)
}

func TestAndroidLogin_InvalidToken(t *testing.T) {
	a, _ := validateServer(t, 401, `{"status":401,"message":"invalid access token"}`)
	_, _, err := a.androidLogin(context.Background(), tok30)
	require.ErrorIs(t, err, ErrTokenInvalid)
}

func TestAndroidLogin_UpstreamErrorHidesToken(t *testing.T) {
	a, _ := validateServer(t, 500, `oops`)
	_, _, err := a.androidLogin(context.Background(), tok30)
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrTokenInvalid))
	assert.NotContains(t, err.Error(), tok30)
}

func TestAndroidLogin_GarbageSkipsValidate(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer srv.Close()
	a := &authFlow{validateURL: srv.URL, http: http.DefaultClient}
	_, _, err := a.androidLogin(context.Background(), "not a url")
	require.ErrorIs(t, err, ErrNoAccessToken)
	assert.False(t, called)
}

// An Android session has no refresh token: refresh re-validates instead of
// failing with "no refresh token".
func TestRefresh_AndroidRevalidates(t *testing.T) {
	a, hdr := validateServer(t, 200, `{"client_id":"kd1unb4b3q4t58fwlpcbzcbnm76a8fp","login":"alice","user_id":"1234","expires_in":3600}`)
	in := platform.Session{AccessToken: tok30, ClientID: ClientAndroid, UserID: "1234", ExpiresAt: time.Now().Add(-time.Hour), Games: []string{"Rust"}}
	out, err := a.refresh(context.Background(), in)
	require.NoError(t, err)
	assert.Equal(t, "OAuth "+tok30, hdr.Get("Authorization"))
	assert.Equal(t, tok30, out.AccessToken)
	assert.Equal(t, ClientAndroid, out.ClientID)
	assert.Equal(t, []string{"Rust"}, out.Games)
	assert.True(t, out.ExpiresAt.After(time.Now()))
}

func TestRefresh_AndroidDeadTokenFails(t *testing.T) {
	a, _ := validateServer(t, 401, `{}`)
	_, err := a.refresh(context.Background(), platform.Session{AccessToken: tok30, ClientID: ClientAndroid})
	require.ErrorIs(t, err, ErrTokenInvalid)
}

func TestRefresh_LegacyWithoutRefreshTokenStillErrors(t *testing.T) {
	a := &authFlow{validateURL: "http://127.0.0.1:1", http: http.DefaultClient}
	_, err := a.refresh(context.Background(), platform.Session{AccessToken: tok30})
	require.EqualError(t, err, "no refresh token")
}

func TestBrowserBackendRefresh_AndroidRevalidates(t *testing.T) {
	a, _ := validateServer(t, 401, `{}`)
	b := &BrowserBackend{authFlow: a}
	_, err := b.RefreshSession(context.Background(), platform.Session{AccessToken: tok30, ClientID: ClientAndroid})
	require.ErrorIs(t, err, ErrTokenInvalid, "a dead Android token must not pass as-is")
}

func TestProfileFor_Android(t *testing.T) {
	assert.Equal(t, profileAndroid, profileFor(ClientAndroid))
}

// An Android paste-login session runs the dashboard discovery path, not the
// TV channel-first walk, and every GQL call goes out as the Android app.
func TestAndroidSession_DashboardPath(t *testing.T) {
	var mu sync.Mutex
	ops := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req gqlRequest
		if r.Method == http.MethodPost {
			require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			assert.Equal(t, clientID, r.Header.Get("Client-Id"), req.OperationName)
			assert.Equal(t, userAgent, r.Header.Get("User-Agent"), req.OperationName)
			assert.Equal(t, "OAuth "+tok30, r.Header.Get("Authorization"), req.OperationName)
			assert.Empty(t, r.Header.Get("Client-Integrity"), req.OperationName)
		}
		mu.Lock()
		ops[req.OperationName] = true
		mu.Unlock()
		switch req.OperationName {
		case OpCampaigns.Name:
			_, _ = w.Write(loadFixture(t, "campaigns.json"))
		case "CurrentUser":
			_, _ = w.Write([]byte(`{"data":{"currentUser":{"id":"1234","login":"alice"}}}`))
		case OpDropCampaignDetails.Name:
			_, _ = w.Write(loadFixture(t, "campaign_details.json"))
		case OpGetStreamInfo.Name:
			_, _ = w.Write(loadFixture(t, "streamlive.json"))
		case OpInventory.Name:
			_, _ = w.Write([]byte(`{"data":{"currentUser":{"inventory":{"dropCampaignsInProgress":[]}}}}`))
		case OpClaimDrop.Name:
			_, _ = w.Write(loadFixture(t, "claim_ok.json"))
		default:
			_, _ = w.Write([]byte(`{"data":{}}`))
		}
	}))
	defer srv.Close()

	b := newForTest(srv.URL)
	sess := platform.Session{AccessToken: tok30, ClientID: ClientAndroid, UserID: "1234", Games: []string{"Rust"}}
	ctx := context.Background()

	camps, err := b.ListActiveCampaigns(ctx, sess)
	require.NoError(t, err)
	require.NotEmpty(t, camps)
	_, err = b.CampaignDetails(ctx, sess, camps[0].ID)
	require.NoError(t, err)
	_, err = b.InventoryProgress(ctx, sess)
	require.NoError(t, err)
	require.NoError(t, b.Claim(ctx, sess, platform.DropBenefit{ID: "drop1", CampaignID: camps[0].ID}))

	mu.Lock()
	defer mu.Unlock()
	assert.True(t, ops[OpCampaigns.Name], "ViewerDropsDashboard must run for Android sessions")
	assert.True(t, ops[OpDropCampaignDetails.Name])
	assert.True(t, ops[OpInventory.Name])
	assert.True(t, ops[OpClaimDrop.Name])
	assert.False(t, ops[OpGameDirectory.Name], "no TV channel-first walk")
}
