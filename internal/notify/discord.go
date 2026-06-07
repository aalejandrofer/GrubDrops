package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type VerbosityFilter struct {
	Allow map[string]bool
}

type DiscordWebhook struct {
	URL    string
	Filter *VerbosityFilter
	HTTP   *http.Client
}

func NewDiscordWebhook(url string, filter *VerbosityFilter) *DiscordWebhook {
	return &DiscordWebhook{
		URL:    url,
		Filter: filter,
		HTTP:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (d *DiscordWebhook) Notify(ctx context.Context, event Event, fields map[string]any) error {
	if d.Filter != nil && !d.Filter.Allow[event] {
		return nil
	}
	embed := buildEmbed(event, fields)
	body, err := json.Marshal(map[string]any{"embeds": []any{embed}})
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

func buildEmbed(event Event, fields map[string]any) map[string]any {
	embed := map[string]any{
		"title":     titleFor(event),
		"color":     colorFor(event),
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	// Render known human fields as proper inline embed fields instead of
	// dumping raw "account: acc_… / benefit: <uuid>" lines. account_label
	// is the @handle (account is the raw id used only for routing).
	str := func(k string) string {
		if v, ok := fields[k]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}
	var ef []map[string]any
	addField := func(name, val string, inline bool) {
		if val == "" {
			return
		}
		ef = append(ef, map[string]any{"name": name, "value": val, "inline": inline})
	}
	acct := str("account_label")
	if acct == "" {
		acct = str("account")
	}
	addField("Account", acct, true)
	addField("Game", str("game"), true)
	addField("Drop", str("drop"), false)
	if ch := str("channel"); ch != "" {
		plat := str("platform")
		link := ch
		switch plat {
		case "twitch":
			link = "[" + ch + "](https://twitch.tv/" + ch + ")"
		case "kick":
			link = "[" + ch + "](https://kick.com/" + ch + ")"
		}
		addField("Channel", link, true)
	}
	if msg := str("msg"); msg != "" {
		addField("Detail", msg, false)
	}

	if len(ef) > 0 {
		embed["fields"] = ef
	} else {
		// Fallback for events without recognised fields (state/auth/error
		// emitters that pass arbitrary keys).
		embed["description"] = descFor(event, fields)
	}
	if img := str("image"); img != "" {
		embed["thumbnail"] = map[string]any{"url": img}
	}
	return embed
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
	default:
		return event
	}
}

func colorFor(event Event) int {
	switch event {
	case EventClaim:
		return 0x2ecc71
	case EventError:
		return 0xe74c3c
	case EventProgress:
		return 0xf1c40f
	default:
		return 0x95a5a6
	}
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
