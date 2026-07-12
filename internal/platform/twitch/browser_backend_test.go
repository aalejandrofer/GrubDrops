package twitch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/aalejandrofer/grubdrops/internal/auth/browser/gen/browser/v1"
	"github.com/aalejandrofer/grubdrops/internal/platform"
)

// fakeSidecar implements TwitchGQLSender + TwitchSidecarAuthenticator.
// ViewerDropsDashboard returns an integrity-failure body for the first
// `integrityFailsLeft` calls, then a clean (empty) campaign list. Every
// other op returns an error so the PubSub bootstrap defers (no websocket
// in unit tests).
type fakeSidecar struct {
	mu                 sync.Mutex
	integrityFailsLeft int
	dashboardCalls     int
	authCalls          int
	alwaysIntegrity    bool
}

func (f *fakeSidecar) TwitchGQL(_ context.Context, _, opName string, _ []byte) ([]byte, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch opName {
	case OpCampaigns.Name: // ViewerDropsDashboard
		f.dashboardCalls++
		if f.alwaysIntegrity || f.integrityFailsLeft > 0 {
			if f.integrityFailsLeft > 0 {
				f.integrityFailsLeft--
			}
			// Status 200 + "failed integrity check" → client.do maps to
			// ErrIntegrityBlocked, the path BrowserBackend must recover.
			return []byte(`{"errors":[{"message":"failed integrity check","path":["currentUser"]}],"data":{"currentUser":null}}`), 200, nil
		}
		return []byte(`{"data":{"currentUser":{"dropCampaigns":[]}}}`), 200, nil
	default:
		// CurrentUser / resolveUserID / etc. — fail so PubSub defers and
		// resolveCurrentLogin stays empty (no detail fetches needed for
		// an empty dashboard).
		return nil, 0, fmt.Errorf("fakeSidecar: unhandled op %q", opName)
	}
}

func (f *fakeSidecar) TwitchAuthenticate(_ context.Context, _ string, _ *pb.TwitchSession) (*pb.TwitchAuthenticateResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.authCalls++
	return &pb.TwitchAuthenticateResponse{Username: "tester", UserId: "123"}, nil
}

func browserTestSession() platform.Session {
	return platform.Session{
		AccessToken: "tok",
		AccountID:   "acc_browser",
		Cookies: map[string]string{
			"twitch": `{"cookies":[{"name":"auth-token","value":"x","domain":".twitch.tv","path":"/"}]}`,
		},
	}
}

// A transient integrity block on the browser path must be recovered by
// refreshing the sidecar tab (re-auth) and retrying — NOT surfaced as
// ErrIntegrityBlocked (which would flip the account to needs_auth).
func TestBrowserBackend_RecoversFromIntegrityBlock(t *testing.T) {
	f := &fakeSidecar{integrityFailsLeft: 1}
	b := NewBrowserBackend(f)

	camps, err := b.ListActiveCampaigns(context.Background(), browserTestSession())
	require.NoError(t, err, "integrity block should be recovered after tab refresh")
	assert.Empty(t, camps)

	f.mu.Lock()
	defer f.mu.Unlock()
	assert.Equal(t, 2, f.dashboardCalls, "dashboard fetched twice: initial (blocked) + retry")
	assert.GreaterOrEqual(t, f.authCalls, 2, "tab re-authenticated on recovery")
}

// When integrity NEVER clears, the browser backend must return a plain
// transient error — the ErrIntegrityBlocked sentinel must be stripped so
// the watcher sleeps + retries instead of flagging needs_auth.
func TestBrowserBackend_PersistentIntegrityIsTransient(t *testing.T) {
	f := &fakeSidecar{alwaysIntegrity: true}
	b := NewBrowserBackend(f)

	_, err := b.ListActiveCampaigns(context.Background(), browserTestSession())
	require.Error(t, err)
	assert.False(t, errors.Is(err, platform.ErrIntegrityBlocked),
		"persistent browser integrity must NOT surface as ErrIntegrityBlocked (would flip account to needs_auth)")
}

// TestBrowserBackend_SpadeBeaconHonoursProxyTransport pins the proxy
// follow-up to PR #28: the direct Spade beacon calls (channel-page GET +
// beacon POST) that don't route through the sidecar must use the global
// proxy transport, not a default direct dialer. Otherwise the browser
// path's outbound analytics traffic leaks around the configured proxy.
func TestBrowserBackend_SpadeBeaconHonoursProxyTransport(t *testing.T) {
	proxyTransport := &http.Transport{}

	withProxy := NewBrowserBackendWithTransport(&fakeSidecar{}, proxyTransport)
	acct := withProxy.accountFor("acc-1")
	require.Same(t, proxyTransport, acct.c.http.Transport,
		"per-account beacon client must use the global proxy transport")

	// No proxy configured → direct dial (nil transport), unchanged behaviour.
	direct := NewBrowserBackend(&fakeSidecar{})
	require.Nil(t, direct.accountFor("acc-1").c.http.Transport,
		"nil transport must dial direct")
}
