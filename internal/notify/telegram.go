package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// TelegramAPIBase is the Bot API root. Tests point a bot at an httptest server.
const TelegramAPIBase = "https://api.telegram.org"

// maxTelegramRetryWait caps how long one send waits on a 429 retry_after.
const maxTelegramRetryWait = 10 * time.Second

// telegramCaptionLimit is the Bot API caption length limit for sendPhoto.
const telegramCaptionLimit = 1024

// TelegramBot sends notify events through the Bot API sendMessage and
// sendPhoto methods with HTML parse mode.
type TelegramBot struct {
	Token  string
	ChatID string
	Filter *VerbosityFilter
	HTTP   *http.Client
	// APIBase defaults to TelegramAPIBase when empty.
	APIBase string
}

func NewTelegramBot(token, chatID string, filter *VerbosityFilter) *TelegramBot {
	return NewTelegramBotWithTransport(token, chatID, filter, nil)
}

func NewTelegramBotWithTransport(token, chatID string, filter *VerbosityFilter, transport *http.Transport) *TelegramBot {
	client := &http.Client{Timeout: 10 * time.Second}
	// Same nil-interface guard as NewDiscordWebhookWithTransport.
	if transport != nil {
		client.Transport = transport
	}
	return &TelegramBot{Token: token, ChatID: chatID, Filter: filter, HTTP: client}
}

func (t *TelegramBot) Notify(ctx context.Context, event Event, fields map[string]any) error {
	if t.Filter != nil && !t.Filter.Allow[event] {
		return nil
	}
	text := buildTelegramText(event, fields)
	if img, _ := fields["image"].(string); img != "" && validImageURL(img) && len([]rune(text)) <= telegramCaptionLimit {
		err := t.send(ctx, "sendPhoto", map[string]any{
			"chat_id":    t.ChatID,
			"photo":      img,
			"caption":    text,
			"parse_mode": "HTML",
		})
		if err == nil {
			return nil
		}
		// Telegram could not fetch the image: deliver the text alone.
		var apiErr *telegramAPIError
		if !errors.As(err, &apiErr) || apiErr.status != http.StatusBadRequest {
			return err
		}
	}
	return t.send(ctx, "sendMessage", map[string]any{
		"chat_id":                  t.ChatID,
		"text":                     text,
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	})
}

type telegramAPIError struct {
	method string
	status int
	desc   string
}

func (e *telegramAPIError) Error() string {
	if e.desc != "" {
		return fmt.Sprintf("telegram %s: %d %s", e.method, e.status, e.desc)
	}
	return fmt.Sprintf("telegram %s: %d", e.method, e.status)
}

type telegramResp struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

// send posts one Bot API call. A 429 waits retry_after (capped) and retries
// once. Errors never carry the token.
func (t *TelegramBot) send(ctx context.Context, method string, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	for attempt := 0; ; attempt++ {
		status, tr, err := t.post(ctx, method, body)
		if err != nil {
			return t.redact(err)
		}
		if status < 300 {
			return nil
		}
		if status == http.StatusTooManyRequests && attempt == 0 {
			wait := time.Duration(tr.Parameters.RetryAfter) * time.Second
			if wait <= 0 {
				wait = time.Second
			}
			if wait > maxTelegramRetryWait {
				return &telegramAPIError{method: method, status: status, desc: t.redactString(tr.Description)}
			}
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			continue
		}
		return &telegramAPIError{method: method, status: status, desc: t.redactString(tr.Description)}
	}
}

func (t *TelegramBot) post(ctx context.Context, method string, body []byte) (int, telegramResp, error) {
	var tr telegramResp
	base := t.APIBase
	if base == "" {
		base = TelegramAPIBase
	}
	endpoint := strings.TrimRight(base, "/") + "/bot" + t.Token + "/" + method
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, tr, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := t.HTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, tr, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	_ = json.Unmarshal(raw, &tr)
	return resp.StatusCode, tr, nil
}

// redact strips the token from an error; *url.Error includes the request URL.
func (t *TelegramBot) redact(err error) error {
	if err == nil || t.Token == "" {
		return err
	}
	return errors.New(t.redactString(err.Error()))
}

func (t *TelegramBot) redactString(s string) string {
	if t.Token == "" {
		return s
	}
	s = strings.ReplaceAll(s, t.Token, "REDACTED")
	return strings.ReplaceAll(s, url.PathEscape(t.Token), "REDACTED")
}

func validImageURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

// buildTelegramText renders an event as Bot API HTML. It carries the same
// content as buildEmbed: every outside-controlled value is escaped.
func buildTelegramText(event Event, fields map[string]any) string {
	str := func(k string) string {
		s, _ := fields[k].(string)
		return s
	}
	intf := func(k string) int {
		n, _ := fields[k].(int)
		return n
	}
	esc := html.EscapeString
	var b strings.Builder
	line := func(s string) {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(s)
	}

	game := str("game")
	drop := str("drop")
	if game != "" || drop != "" {
		title := drop
		if title == "" {
			title = titleFor(event)
		}
		line("<b>" + esc(title) + "</b>")
		if game != "" {
			name := game
			if plat := platformLabel(str("platform")); plat != "" {
				name = game + " · " + plat
			}
			line(esc(name))
		}
		acct := str("account_label")
		if acct == "" {
			acct = str("account")
		}
		if acct != "" {
			line("Account: " + esc(acct))
		}
		if ch := str("channel"); ch != "" {
			line("Channel: " + telegramChannel(str("platform"), ch))
		}
		if req := intf("req_min"); req > 0 {
			cur := intf("cur_min")
			label := "⏳ Mining"
			switch event {
			case EventClaim:
				label = "✅ Claimed"
				if cur < req {
					cur = req
				}
			case EventTest:
				label = "✅ Test"
				cur = req
			}
			line(fmt.Sprintf("%s <code>%d/%d min</code>", label, cur, req))
			line(progressBar(cur, req))
		}
		if msg := str("msg"); msg != "" {
			line("Detail: " + esc(msg))
		}
		footer := "GrubDrops"
		if camp := str("campaign"); camp != "" {
			footer = "GrubDrops • " + camp
		}
		line("<i>" + esc(footer) + "</i>")
		return b.String()
	}

	if event == EventSummary {
		title := str("title")
		if title == "" {
			title = titleFor(event)
		}
		line("<b>" + esc(title) + "</b>")
		if msg := str("msg"); msg != "" {
			line(esc(msg))
		}
		return b.String()
	}

	line("<b>" + esc(titleFor(event)) + "</b>")
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		line(esc(fmt.Sprintf("%s: %v", k, fields[k])))
	}
	return b.String()
}

func telegramChannel(platform, ch string) string {
	var host string
	switch platform {
	case "twitch":
		host = "https://twitch.tv/"
	case "kick":
		host = "https://kick.com/"
	default:
		return html.EscapeString(ch)
	}
	href := host + url.PathEscape(ch)
	return `<a href="` + html.EscapeString(href) + `">` + html.EscapeString(ch) + `</a>`
}
