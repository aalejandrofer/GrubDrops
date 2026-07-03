package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type VerbosityFilter struct {
	Allow map[string]bool
}

type DiscordWebhook struct {
	URL    string
	Filter *VerbosityFilter
	HTTP   *http.Client

	// Username / AvatarURL brand the webhook sender. Empty values are
	// omitted from the payload (Discord then uses the webhook's own name).
	Username  string
	AvatarURL string
}

func NewDiscordWebhook(url string, filter *VerbosityFilter) *DiscordWebhook {
	return &DiscordWebhook{
		URL:    url,
		Filter: filter,
		HTTP:   &http.Client{Timeout: 10 * time.Second},
	}
}

func NewDiscordWebhookWithTransport(url string, filter *VerbosityFilter, transport *http.Transport) *DiscordWebhook {
	client := &http.Client{Timeout: 10 * time.Second}
	// A nil *http.Transport assigned into http.Client.Transport (an
	// interface) is a non-nil interface wrapping a nil pointer, which
	// panics on first use. Only set Transport when non-nil so the
	// zero-value case behaves exactly like NewDiscordWebhook (client uses
	// http.DefaultTransport, i.e. direct).
	if transport != nil {
		client.Transport = transport
	}
	return &DiscordWebhook{
		URL:    url,
		Filter: filter,
		HTTP:   client,
	}
}

func (d *DiscordWebhook) Notify(ctx context.Context, event Event, fields map[string]any) error {
	if d.Filter != nil && !d.Filter.Allow[event] {
		return nil
	}
	embed := buildEmbed(event, fields)
	payload := map[string]any{"embeds": []any{embed}}
	if d.Username != "" {
		payload["username"] = d.Username
	}
	if d.AvatarURL != "" {
		payload["avatar_url"] = d.AvatarURL
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("discord webhook %s: %s", d.URL, resp.Status)
	}
	return nil
}

// buildEmbed renders a notify event into a Discord embed.
//
// For drop events (progress/claim) it produces the rich layout: an author
// line ("Game · Platform"), the drop name as title, a platform-brand accent
// (green on claim), Account/Channel fields, a progress field with a unicode
// bar, the benefit image as thumbnail, and a "GrubDrops • Campaign" footer.
//
// Events without a drop/game (auth/error/state emitters) fall back to a
// simple title + key/value description so nothing is lost.
func buildEmbed(event Event, fields map[string]any) map[string]any {
	str := func(k string) string {
		if v, ok := fields[k]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}
	intf := func(k string) int {
		if v, ok := fields[k]; ok {
			if n, ok := v.(int); ok {
				return n
			}
		}
		return 0
	}

	embed := map[string]any{
		"color":     colorFor(event, fields),
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	game := str("game")
	drop := str("drop")

	// Rich path: a real drop event with game/drop context.
	if game != "" || drop != "" {
		if drop != "" {
			embed["title"] = drop
		} else {
			embed["title"] = titleFor(event)
		}
		if game != "" {
			name := game
			if plat := platformLabel(str("platform")); plat != "" {
				name = game + " · " + plat
			}
			embed["author"] = map[string]any{"name": name}
		}

		var ef []map[string]any
		add := func(name, val string, inline bool) {
			if val == "" {
				return
			}
			ef = append(ef, map[string]any{"name": name, "value": val, "inline": inline})
		}
		acct := str("account_label")
		if acct == "" {
			acct = str("account")
		}
		add("Account", acct, true)
		if ch := str("channel"); ch != "" {
			add("Channel", channelLink(str("platform"), ch), true)
		}
		// Progress field: status label + minutes + unicode bar.
		if req := intf("req_min"); req > 0 {
			cur := intf("cur_min")
			label := "⏳ Mining"
			switch event {
			case EventClaim:
				label = "✅ Claimed"
				if cur < req {
					cur = req // claim implies the requirement is met
				}
			case EventTest:
				label = "✅ Test"
				cur = req // a test always shows complete
			}
			add(label, fmt.Sprintf("`%d/%d min`\n%s", cur, req, progressBar(cur, req)), false)
		}
		if msg := str("msg"); msg != "" {
			add("Detail", msg, false)
		}
		if len(ef) > 0 {
			embed["fields"] = ef
		}

		if img := str("image"); img != "" {
			embed["thumbnail"] = map[string]any{"url": img}
		}
		footer := "GrubDrops"
		if camp := str("campaign"); camp != "" {
			footer = "GrubDrops • " + camp
		}
		embed["footer"] = map[string]any{"text": footer}
		return embed
	}

	// Fallback path: auth/error/state — simple title + description dump.
	embed["title"] = titleFor(event)
	embed["description"] = descFor(event, fields)
	return embed
}

// progressBar renders cur/req as a fixed 10-segment unicode bar. Returns
// "" when req<=0 (nothing to chart). Over-full clamps to all-filled.
func progressBar(cur, req int) string {
	if req <= 0 {
		return ""
	}
	const segments = 10
	filled := (cur*segments + req/2) / req // round to nearest segment
	if filled > segments {
		filled = segments
	}
	if filled < 0 {
		filled = 0
	}
	return strings.Repeat("▰", filled) + strings.Repeat("▱", segments-filled)
}

func platformLabel(p string) string {
	switch p {
	case "twitch":
		return "Twitch"
	case "kick":
		return "Kick"
	case "":
		return ""
	default:
		return strings.ToUpper(p[:1]) + p[1:]
	}
}

func channelLink(platform, ch string) string {
	switch platform {
	case "twitch":
		return "[" + ch + "](https://twitch.tv/" + ch + ")"
	case "kick":
		return "[" + ch + "](https://kick.com/" + ch + ")"
	default:
		return ch
	}
}

func titleFor(event Event) string {
	switch event {
	case EventClaim:
		return "Drop claimed"
	case EventProgress:
		return "Drop progress"
	case EventState:
		return "State change"
	case EventAuth:
		return "Auth event"
	case EventError:
		return "Error"
	case EventCanary:
		return "Heartbeat health check failed"
	default:
		return event
	}
}

// colorFor picks the embed accent. Claim is always green and error always
// red; otherwise drop events take their platform's brand color.
func colorFor(event Event, fields map[string]any) int {
	switch event {
	case EventClaim, EventTest:
		return 0x23A55A
	case EventError, EventCanary:
		return 0xE74C3C
	}
	if plat, _ := fields["platform"].(string); plat != "" {
		switch plat {
		case "twitch":
			return 0x9146FF
		case "kick":
			return 0x53FC18
		}
	}
	if event == EventProgress {
		return 0xF1C40F
	}
	return 0x95A5A6
}

func descFor(_ Event, fields map[string]any) string {
	if len(fields) == 0 {
		return ""
	}
	parts := make([]string, 0, len(fields))
	for k, v := range fields {
		parts = append(parts, fmt.Sprintf("%s: %v", k, v))
	}
	var buf bytes.Buffer
	for i, p := range parts {
		if i > 0 {
			buf.WriteByte('\n')
		}
		buf.WriteString(p)
	}
	return buf.String()
}
