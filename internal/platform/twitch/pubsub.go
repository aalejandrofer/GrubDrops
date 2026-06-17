package twitch

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// PubSubEndpoint is Twitch's PubSub edge. Uses the legacy v1 protocol
// (newer EventSub-WebSockets doesn't expose drop topics yet). LISTEN +
// MESSAGE frames are JSON; PING/PONG keep the socket alive.
const PubSubEndpoint = "wss://pubsub-edge.twitch.tv/v1"

// PubSubMaxTopics is the upstream cap per socket (~50 in practice).
// We shard across multiple sockets if the topic count exceeds this.
const PubSubMaxTopics = 50

// PubSubPingInterval — Twitch disconnects after ~5 min of silence. Ping
// every 4 min with ±10s jitter (per upstream docs).
const PubSubPingInterval = 4 * time.Minute

// PubSubHandlers is the callback surface the watcher wires into the
// client. All methods may be invoked concurrently from the read loop.
// Implementations must be cheap (no blocking I/O) — kick off goroutines
// for anything substantial.
type PubSubHandlers struct {
	// OnDropProgress fires for `drop-progress` messages on
	// `user-drop-events.<user_id>`. cur/req are in seconds (Twitch's
	// public field) — the watcher converts to minutes downstream.
	OnDropProgress func(dropID string, cur, req int64)
	// OnDropClaim fires for `drop-claim` messages on
	// `user-drop-events.<user_id>`. The payload carries the
	// drop_instance_id that the claim mutation needs.
	OnDropClaim func(dropID, instanceID string)
	// OnStreamDown fires for `stream-down` on
	// `video-playback-by-id.<channel_id>`. The watcher uses this to
	// re-pick a stream immediately rather than waiting for the next
	// tick.
	OnStreamDown func(channelID string)
	// OnStreamUp fires for `stream-up`. Used so a recently-down
	// channel can be re-picked without waiting for the discovery
	// poll.
	OnStreamUp func(channelID string)
	// OnRewardCode fires when an onsite-notification payload carries
	// a Twitch/Mojang-style redemption code (5 blocks of 5
	// alphanumerics separated by hyphens — e.g. Minecraft cape codes).
	// notificationID is the per-event identifier so the receiver can
	// dedupe; body is the raw notification text so the receiver can
	// capture surrounding context (game / drop name) when persisting.
	OnRewardCode func(notificationID, code, body string)
}

// PubSubClient holds one WebSocket connection and the topics LISTENed
// on it. Topic list can be modified at runtime — RemoveTopic /
// AddTopic emit the appropriate frames. The client reconnects with
// exponential backoff and re-subscribes its topic set on every
// reconnect.
type PubSubClient struct {
	authToken string
	handlers  PubSubHandlers

	mu      sync.Mutex
	topics  map[string]struct{}
	conn    *websocket.Conn
	closed  bool
	cancel  context.CancelFunc // stops the Run loop
	writeMu sync.Mutex         // protects WebSocket writes
}

// NewPubSubClient builds a client. Call Connect to dial.
func NewPubSubClient(authToken string, handlers PubSubHandlers) *PubSubClient {
	return &PubSubClient{
		authToken: authToken,
		handlers:  handlers,
		topics:    map[string]struct{}{},
	}
}

// Close stops the Run loop and closes the WebSocket connection.
func (p *PubSubClient) Close() {
	p.mu.Lock()
	p.closed = true
	if p.cancel != nil {
		p.cancel()
	}
	if p.conn != nil {
		p.conn.Close()
	}
	p.mu.Unlock()
}

// Run dials, subscribes, and pumps messages until ctx is cancelled.
// Reconnects on disconnect with exponential backoff (capped at 60s).
// Returns when ctx is done — never returns nil otherwise.
func (p *PubSubClient) Run(ctx context.Context, initialTopics []string) error {
	for _, t := range initialTopics {
		p.topics[t] = struct{}{}
	}
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := p.dialAndPump(ctx); err != nil {
			slog.Warn("pubsub disconnected", "err", err, "backoff", backoff)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > 60*time.Second {
				backoff = 60 * time.Second
			}
			continue
		}
		backoff = time.Second
	}
}

// AddTopic appends a topic to the current subscription. Idempotent.
// Emits a LISTEN frame if the socket is up; on reconnect the topic is
// re-subscribed automatically.
func (p *PubSubClient) AddTopic(topic string) {
	p.mu.Lock()
	_, dup := p.topics[topic]
	// Twitch silently drops LISTENs past ~50 topics/connection, which
	// would make real-time events vanish with no error. Refuse past the
	// cap and log instead of pretending it was subscribed. (Per-account
	// PubSub clients keep counts low, so this is a safety net.)
	if !dup && len(p.topics) >= PubSubMaxTopics {
		p.mu.Unlock()
		slog.Warn("pubsub topic cap reached; refusing new topic",
			"topic", topic, "cap", PubSubMaxTopics)
		return
	}
	p.topics[topic] = struct{}{}
	conn := p.conn
	p.mu.Unlock()
	if dup || conn == nil {
		return
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	if err := writeListen(conn, p.authToken, []string{topic}); err != nil {
		slog.Warn("pubsub add topic write failed", "topic", topic, "err", err)
	}
}

// RemoveTopic drops a topic. UNLISTEN is emitted if the socket is up.
func (p *PubSubClient) RemoveTopic(topic string) {
	p.mu.Lock()
	if _, ok := p.topics[topic]; !ok {
		p.mu.Unlock()
		return
	}
	delete(p.topics, topic)
	conn := p.conn
	p.mu.Unlock()
	if conn == nil {
		return
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	frame := map[string]any{
		"type":  "UNLISTEN",
		"nonce": newNonce(),
		"data":  map[string]any{"topics": []string{topic}},
	}
	if err := conn.WriteJSON(frame); err != nil {
		slog.Warn("pubsub unlisten write failed", "topic", topic, "err", err)
	}
}

func (p *PubSubClient) dialAndPump(ctx context.Context) error {
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, PubSubEndpoint, http.Header{})
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()
	p.mu.Lock()
	p.conn = conn
	topics := make([]string, 0, len(p.topics))
	for t := range p.topics {
		topics = append(topics, t)
	}
	p.mu.Unlock()
	if len(topics) > 0 {
		p.writeMu.Lock()
		err := writeListen(conn, p.authToken, topics)
		p.writeMu.Unlock()
		if err != nil {
			return fmt.Errorf("listen: %w", err)
		}
	}
	slog.Info("pubsub connected", "topics", len(topics))

	pumpCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Ping pump.
	go func() {
		t := time.NewTimer(jitter(PubSubPingInterval))
		defer t.Stop()
		for {
			select {
			case <-pumpCtx.Done():
				return
			case <-t.C:
				p.writeMu.Lock()
				err := conn.WriteJSON(map[string]any{"type": "PING"})
				p.writeMu.Unlock()
				if err != nil {
					slog.Warn("pubsub ping write failed", "err", err)
					cancel()
					return
				}
				t.Reset(jitter(PubSubPingInterval))
			}
		}
	}()

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			p.mu.Lock()
			p.conn = nil
			p.mu.Unlock()
			return fmt.Errorf("read: %w", err)
		}
		p.handleFrame(raw)
	}
}

func (p *PubSubClient) handleFrame(raw []byte) {
	var env struct {
		Type  string          `json:"type"`
		Nonce string          `json:"nonce,omitempty"`
		Error string          `json:"error,omitempty"`
		Data  json.RawMessage `json:"data,omitempty"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		slog.Warn("pubsub bad envelope", "err", err, "raw", string(raw))
		return
	}
	switch env.Type {
	case "PONG":
		return
	case "RECONNECT":
		// Twitch's "you should reconnect soon" hint. The current read
		// loop returning EOF will trigger reconnect on its own when the
		// edge closes the socket; nothing to do here.
		slog.Info("pubsub reconnect hint received")
		return
	case "RESPONSE":
		if env.Error != "" {
			slog.Warn("pubsub listen error", "nonce", env.Nonce, "error", env.Error)
		}
		return
	case "MESSAGE":
		var msg struct {
			Topic   string `json:"topic"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(env.Data, &msg); err != nil {
			slog.Warn("pubsub message data decode failed", "err", err)
			return
		}
		p.dispatch(msg.Topic, []byte(msg.Message))
	default:
		slog.Debug("pubsub unknown frame", "type", env.Type)
	}
}

func (p *PubSubClient) dispatch(topic string, payload []byte) {
	switch {
	case startsWith(topic, "user-drop-events."):
		p.dispatchDropEvent(payload)
	case startsWith(topic, "video-playback-by-id."):
		p.dispatchPlaybackEvent(topic, payload)
	case startsWith(topic, "onsite-notifications."):
		// Onsite notifications surface user_drop_reward_reminder + a
		// pile of unrelated stuff. Drop-claim is already covered by
		// user-drop-events.drop-claim, so we only mine this stream
		// for redemption codes (Minecraft etc) that aren't carried
		// by the regular claim event.
		p.dispatchNotification(payload)
		return
	default:
		slog.Debug("pubsub unhandled topic", "topic", topic)
	}
}

// dispatchDropEvent decodes a user-drop-events payload. Two relevant
// shapes:
//
//	type=drop-progress  → data.current_progress_min / data.required_progress_min
//	type=drop-claim     → data.drop_instance_id + data.benefit / drop_id
func (p *PubSubClient) dispatchDropEvent(payload []byte) {
	var env struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		slog.Warn("pubsub drop event decode failed", "err", err)
		return
	}
	switch env.Type {
	case "drop-progress":
		var d struct {
			DropID              string `json:"drop_id"`
			CurrentProgressMin  int64  `json:"current_progress_min"`
			RequiredProgressMin int64  `json:"required_progress_min"`
		}
		if err := json.Unmarshal(env.Data, &d); err != nil {
			return
		}
		if p.handlers.OnDropProgress != nil {
			p.handlers.OnDropProgress(d.DropID, d.CurrentProgressMin, d.RequiredProgressMin)
		}
	case "drop-claim":
		var d struct {
			DropID         string `json:"drop_id"`
			DropInstanceID string `json:"drop_instance_id"`
		}
		if err := json.Unmarshal(env.Data, &d); err != nil {
			return
		}
		if p.handlers.OnDropClaim != nil {
			p.handlers.OnDropClaim(d.DropID, d.DropInstanceID)
		}
	}
}

// codePattern matches Twitch reward redemption codes, which use the
// 5-block uppercase form Mojang/Twitch issues (e.g.
// "7Y64H-GKPXP-YKHMG-H7634-79D2Z"). Anchored case-sensitive to keep
// false positives (sentence words, emails) out of the match. The
// extractor is intentionally narrow — we'd rather miss a non-standard
// code than misclassify random body text.
var codePattern = regexp.MustCompile(`\b[A-Z0-9]{5}-[A-Z0-9]{5}-[A-Z0-9]{5}-[A-Z0-9]{5}-[A-Z0-9]{5}\b`)

// dispatchNotification decodes an onsite-notifications.<uid> payload
// and emits drop-redemption codes via OnRewardCode when the body
// matches the canonical "click here to redeem" shape. Best-effort —
// if the shape changes or codes don't match the pattern we just log
// at Debug and move on.
func (p *PubSubClient) dispatchNotification(payload []byte) {
	var env struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return
	}
	if env.Type != "create-notification" && env.Type != "" {
		return
	}
	var d struct {
		Notification struct {
			ID      string `json:"id"`
			Type    string `json:"type"`
			Body    string `json:"body"`
			Summary string `json:"summary"`
		} `json:"notification"`
	}
	if err := json.Unmarshal(env.Data, &d); err != nil {
		slog.Debug("pubsub notification decode failed", "err", err)
		return
	}
	if d.Notification.Body == "" && d.Notification.Summary == "" {
		return
	}
	text := d.Notification.Body
	if text == "" {
		text = d.Notification.Summary
	}
	code := codePattern.FindString(text)
	if code == "" {
		slog.Debug("pubsub notification: no code in body",
			"notification_type", d.Notification.Type,
			"body", truncateString(text, 200))
		return
	}
	slog.Info("pubsub reward code extracted",
		"kind", "claim",
		"notification_id", d.Notification.ID,
		"notification_type", d.Notification.Type,
		"code", code)
	if p.handlers.OnRewardCode != nil {
		p.handlers.OnRewardCode(d.Notification.ID, code, text)
	}
}

func truncateString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// dispatchPlaybackEvent decodes video-playback-by-id payloads. Topic
// is `video-playback-by-id.<channel_id>`; extract the ID from topic so
// the watcher can correlate.
func (p *PubSubClient) dispatchPlaybackEvent(topic string, payload []byte) {
	channelID := topic[len("video-playback-by-id."):]
	var env struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return
	}
	switch env.Type {
	case "stream-up":
		if p.handlers.OnStreamUp != nil {
			p.handlers.OnStreamUp(channelID)
		}
	case "stream-down":
		if p.handlers.OnStreamDown != nil {
			p.handlers.OnStreamDown(channelID)
		}
	}
}

func writeListen(conn *websocket.Conn, token string, topics []string) error {
	return conn.WriteJSON(map[string]any{
		"type":  "LISTEN",
		"nonce": newNonce(),
		"data": map[string]any{
			"topics":     topics,
			"auth_token": token,
		},
	})
}

func newNonce() string {
	return strconv.FormatUint(rand.Uint64(), 36)
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func jitter(d time.Duration) time.Duration {
	// ±10s wobble so many sockets don't all ping in lockstep.
	return d - 10*time.Second + time.Duration(rand.Int64N(int64(20*time.Second)))
}

// Topic builders mirror DevilXD's constants.

// TopicUserDropEvents subscribes to drop progress + claim events for a
// user. user_id is the numeric Twitch ID (currentUser.id).
func TopicUserDropEvents(userID int64) string {
	return "user-drop-events." + strconv.FormatInt(userID, 10)
}

// TopicVideoPlaybackByID watches a channel's broadcast state — up,
// down, viewcount, commercials. channelID is the broadcaster's
// numeric Twitch ID.
func TopicVideoPlaybackByID(channelID string) string {
	return "video-playback-by-id." + channelID
}

// TopicOnsiteNotifications subscribes to the user's notification
// stream — includes user_drop_reward_reminder among many others.
func TopicOnsiteNotifications(userID int64) string {
	return "onsite-notifications." + strconv.FormatInt(userID, 10)
}
