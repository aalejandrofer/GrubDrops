package notify

import (
	"context"
	"net/http"
	"sync"
)

// AccountURLResolver returns the per-account Discord webhook URL for
// the given account id, or empty string if none is configured.
type AccountURLResolver func(accountID string) string

// AccountRoutedNotifier inspects the "account" field of a notify event
// and routes to a per-account Discord webhook when one is configured.
// Otherwise it delegates to the fallback notifier.
type AccountRoutedNotifier struct {
	fallback  Notifier
	resolve   AccountURLResolver
	filter    *VerbosityFilter
	transport *http.Transport

	// Branding applied to each per-account webhook client.
	Username  string
	AvatarURL string

	mu    sync.Mutex
	cache map[string]*DiscordWebhook
}

func NewAccountRouted(fallback Notifier, resolve AccountURLResolver, filter *VerbosityFilter, transport *http.Transport) *AccountRoutedNotifier {
	return &AccountRoutedNotifier{
		fallback:  fallback,
		resolve:   resolve,
		filter:    filter,
		transport: transport,
		cache:     map[string]*DiscordWebhook{},
	}
}

func (r *AccountRoutedNotifier) Notify(ctx context.Context, event Event, fields map[string]any) error {
	accountID, _ := fields["account"].(string)
	if accountID == "" {
		return r.fallback.Notify(ctx, event, fields)
	}
	url := r.resolve(accountID)
	if url == "" {
		return r.fallback.Notify(ctx, event, fields)
	}
	wh := r.client(url)
	return wh.Notify(ctx, event, fields)
}

func (r *AccountRoutedNotifier) client(url string) *DiscordWebhook {
	r.mu.Lock()
	defer r.mu.Unlock()
	if wh, ok := r.cache[url]; ok {
		return wh
	}
	wh := NewDiscordWebhookWithTransport(url, r.filter, r.transport)
	wh.Username = r.Username
	wh.AvatarURL = r.AvatarURL
	r.cache[url] = wh
	return wh
}
