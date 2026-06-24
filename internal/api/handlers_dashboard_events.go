package api

// Live-events drawer helpers, split out of handlers_dashboard.go: turning
// the in-memory log ring into the dashboard's event model (filtering,
// kind classification, colour, and detail flattening).

import (
	"fmt"
	"html"
	"sort"
	"strings"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/i18n"
	mlog "github.com/aalejandrofer/grubdrops/internal/log"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// eventMsgKeys maps the exact static slog message text emitted by the
// watcher / scheduler / discovery / canary / pubsub paths to an i18n
// key. Only static literal messages appear here; fmt.Sprintf-built
// messages can't be keyed and fall back to the raw (escaped) text.
var eventMsgKeys = map[string]string{
	"kick auto: WS start failed, falling back to Chrome sidecar":                               "event.kick_ws_start_failed_fallback",
	"kick sidecar absent, auto-creating":                                                       "event.kick_sidecar_absent_autocreate",
	"kick sidecar existence probe failed; assuming up":                                         "event.kick_sidecar_probe_failed",
	"kick sidecar idle, stopping":                                                              "event.kick_sidecar_idle_stopping",
	"kick sidecar inspect failed; assuming up":                                                 "event.kick_sidecar_inspect_failed",
	"kick sidecar missing and auto-create disabled (no image); needs a hand-defined container": "event.kick_sidecar_missing_no_image",
	"kick sidecar network self-detect failed; sidecars use default network":                    "event.kick_sidecar_network_selfdetect_failed",
	"kick sidecar orphan remove failed":                                                        "event.kick_sidecar_orphan_remove_failed",
	"kick sidecar orphan sweep: list failed":                                                   "event.kick_sidecar_orphan_sweep_list_failed",
	"kick sidecar orphan, removing":                                                            "event.kick_sidecar_orphan_removing",
	"kick sidecar ready":                                                                       "event.kick_sidecar_ready",
	"kick sidecar starting on demand":                                                          "event.kick_sidecar_starting_on_demand",
	"kick sidecar stop failed":                                                                 "event.kick_sidecar_stop_failed",
	"kick sweep: claim attempt returned error (likely already granted)":                        "event.kick_sweep_claim_error",
	"kick sweep: claimed completed reward":                                                     "event.kick_sweep_claimed_reward",
	"kick ws watch reconnecting":                                                               "event.kick_ws_watch_reconnecting",
	"kick ws watch started":                                                                    "event.kick_ws_watch_started",
	"kick: auto watch requested but no sidecar client configured; running pure WebSocket with no Chrome fallback": "event.kick_auto_no_sidecar_ws_only",
	"kick: browser-watch requested but no sidecar client configured; falling back to the WebSocket watch path":    "event.kick_browser_no_sidecar_ws_fallback",
	"pubsub add topic write failed":                "event.pubsub_add_topic_write_failed",
	"pubsub bad envelope":                          "event.pubsub_bad_envelope",
	"pubsub connected":                             "event.pubsub_connected",
	"pubsub disconnected":                          "event.pubsub_disconnected",
	"pubsub drop event decode failed":              "event.pubsub_drop_event_decode_failed",
	"pubsub listen error":                          "event.pubsub_listen_error",
	"pubsub message data decode failed":            "event.pubsub_message_data_decode_failed",
	"pubsub ping write failed":                     "event.pubsub_ping_write_failed",
	"pubsub reconnect hint received":               "event.pubsub_reconnect_hint_received",
	"pubsub reward code extracted":                 "event.pubsub_reward_code_extracted",
	"pubsub run exited":                            "event.pubsub_run_exited",
	"pubsub topic cap reached; refusing new topic": "event.pubsub_topic_cap_reached",
	"pubsub unlisten write failed":                 "event.pubsub_unlisten_write_failed",
	"pubsub: resolve user id failed, deferring":    "event.pubsub_resolve_user_id_failed",
	"twitch backend reused across accounts — events will misattribute; give each account its own backend": "event.twitch_backend_reused",
	"twitch gql 5xx":                                                   "event.twitch_gql_5xx",
	"twitch gql application error":                                     "event.twitch_gql_application_error",
	"twitch gql decode failed":                                         "event.twitch_gql_decode_failed",
	"twitch gql partial response (returning data anyway)":              "event.twitch_gql_partial_response",
	"twitch gql rate-limited (429); backing off":                       "event.twitch_gql_rate_limited",
	"twitch gql response":                                              "event.twitch_gql_response",
	"twitch gql still integrity-blocked after retry; flagging account": "event.twitch_gql_integrity_blocked_retry",
	"twitch integrity non-200":                                         "event.twitch_integrity_non_200",
	"twitch integrity token acquired":                                  "event.twitch_integrity_token_acquired",
	"twitch reward claim soft error":                                   "event.twitch_reward_claim_soft_error",
	"twitch sidecar listActive recoverable error; refreshing tab":      "event.twitch_sidecar_listactive_recoverable",
	"twitch sidecar still integrity-blocked after tab refresh; will retry next cycle (not flagging needs_auth)": "event.twitch_sidecar_integrity_blocked_refresh",
	"twitch sidecar tab missing on ClaimRewards; re-authenticating":                                             "event.twitch_sidecar_tab_missing_claim",
	"twitch sidecar tab missing on InventoryProgress; re-authenticating":                                        "event.twitch_sidecar_tab_missing_inventory",
	"twitch sidecar tab ready":                                                  "event.twitch_sidecar_tab_ready",
	"twitch skipping non-watch drop (0 required minutes)":                       "event.twitch_skipping_non_watch_drop",
	"watcher awaiting account connect":                                          "event.watcher_awaiting_account_connect",
	"watcher benefit complete, claiming":                                        "event.watcher_benefit_complete_claiming",
	"watcher benefit frozen (minutes not advancing); rotating channel":          "event.watcher_benefit_frozen_rotating",
	"watcher benefit vanished from inventory; treating as externally claimed":   "event.watcher_benefit_vanished",
	"watcher channel went offline; swapping":                                    "event.watcher_channel_offline_swapping",
	"watcher claim failed":                                                      "event.watcher_claim_failed",
	"watcher claim recorded":                                                    "event.watcher_claim_recorded",
	"watcher discovery":                                                         "event.watcher_discovery",
	"watcher has no eligible benefit, sleeping":                                 "event.watcher_no_eligible_benefit_sleeping",
	"watcher heartbeat failed":                                                  "event.watcher_heartbeat_failed",
	"watcher heartbeat sent":                                                    "event.watcher_heartbeat_sent",
	"watcher inventory failed; treating as no progress yet":                     "event.watcher_inventory_failed_no_progress",
	"watcher inventory failed":                                                  "event.watcher_inventory_failed",
	"watcher list campaigns failed":                                             "event.watcher_list_campaigns_failed",
	"watcher list channels failed":                                              "event.watcher_list_channels_failed",
	"watcher mining link-overridden campaign":                                   "event.watcher_mining_link_overridden",
	"watcher no eligible streams live, trying next campaign":                    "event.watcher_no_eligible_streams_live",
	"watcher persist campaigns failed":                                          "event.watcher_persist_campaigns_failed",
	"watcher picked benefit":                                                    "event.watcher_picked_benefit",
	"watcher post-claim: drop still in current session, server lag expected":    "event.watcher_post_claim_server_lag",
	"watcher progress":                                                          "event.watcher_progress",
	"watcher reconciled owned drop into claims":                                 "event.watcher_reconciled_owned_drop",
	"watcher record claim failed":                                               "event.watcher_record_claim_failed",
	"watcher record claim+code failed":                                          "event.watcher_record_claim_code_failed",
	"watcher reward claimed":                                                    "event.watcher_reward_claimed",
	"watcher reward code captured":                                              "event.watcher_reward_code_captured",
	"watcher reward reaper failed":                                              "event.watcher_reward_reaper_failed",
	"watcher reward reaper: nothing to claim":                                   "event.watcher_reward_reaper_nothing",
	"watcher skipped reward campaigns":                                          "event.watcher_skipped_reward_campaigns",
	"watcher skipped unlinked campaigns":                                        "event.watcher_skipped_unlinked_campaigns",
	"watcher skipping drop with unmet precondition":                             "event.watcher_skipping_unmet_precondition",
	"watcher starting watch":                                                    "event.watcher_starting_watch",
	"watcher StartWatch failed":                                                 "event.watcher_startwatch_failed",
	"watcher state change":                                                      "event.watcher_state_change",
	"watcher step error; will retry after backoff":                              "event.watcher_step_error_retry",
	"watcher swept completed reward":                                            "event.watcher_swept_completed_reward",
	"watcher: benefit never appeared in inventory (claimed or ghost); skipping": "event.watcher_benefit_never_appeared",
	"watcher: integrity blocked, marking account needs_auth":                    "event.watcher_integrity_blocked_needs_auth",
	"watcher: no whitelisted games match active campaigns, sleeping":            "event.watcher_no_whitelisted_games",
}

// eventsFromRing transforms the in-memory log ring into the
// dashboard's event drawer model. The ring stores typed entries
// (LogLine.Kind, set by the watcher / login handlers / ringHandler);
// when Kind is empty we fall back to substring matching on the message
// so older un-tagged log lines still classify usefully.
//
// `kindFilter` is one of "" / "all" / "claim" / "progress" / "state" /
// "discovery" / "error" / "auth"; anything else is treated as "all".
// `accountFilter` is the account ID to keep ("" or "all" = keep all).
func eventsFromRing(ring *mlog.Ring, kindFilter, accountFilter, lang string, accs []gen.Account, loc *time.Location) []dashEvent {
	if ring == nil {
		return nil
	}
	// Build account_id -> @login map so events render the human handle
	// instead of acc_XXXXXXXX... — matches how upstream
	// TwitchDropsMiner labels output.
	labelByID := make(map[string]string, len(accs))
	platformByID := make(map[string]string, len(accs))
	for _, a := range accs {
		labelByID[a.ID] = a.DisplayName
		platformByID[a.ID] = a.Platform
	}
	lines := ring.Snapshot()
	out := make([]dashEvent, 0, len(lines))
	for i := len(lines) - 1; i >= 0 && len(out) < 80; i-- {
		l := lines[i]
		kind := l.Kind
		if kind == "" {
			kind = classifyEvent(l.Msg, l.Level)
		}
		if kindFilter != "" && kindFilter != "all" && kind != kindFilter {
			continue
		}
		accID := fieldStr(l.Fields, "account")
		if accountFilter != "" && accountFilter != "all" && accID != accountFilter {
			continue
		}
		label := labelByID[accID]
		if label == "" {
			label = accID
		}
		out = append(out, dashEvent{
			ID:       fmt.Sprintf("ev-%d-%d", l.TS.UnixNano(), i),
			Time:     l.TS.In(loc).Format("15:04:05"),
			Kind:     kind,
			Color:    colorForKind(kind, l.Level),
			BodyHTML: fmt.Sprintf("<em>%s</em> · %s", translateKind(lang, kind), translateMsg(lang, l.Msg)),
			Account:  label,
			Platform: platformByID[accID],
			Details:  detailsFor(l),
		})
	}
	return out
}

// classifyEvent is the fallback for log lines without an explicit
// Kind. Conservative — only fires on unambiguous substrings. New
// structured emitters should set Kind directly instead of relying on
// this.
func classifyEvent(msg, level string) string {
	m := strings.ToLower(msg)
	switch {
	case strings.Contains(m, "claim"):
		return "claim"
	case strings.Contains(m, "progress") || strings.Contains(m, "heartbeat"):
		return "progress"
	case strings.Contains(m, "auth") || strings.Contains(m, "login") || strings.Contains(m, "session") || strings.Contains(m, "device-code") || strings.Contains(m, "cookies"):
		return "auth"
	case strings.Contains(m, "state") || strings.Contains(m, "pickcampaign") || strings.Contains(m, "pickstream") || strings.Contains(m, "starting watch"):
		return "state"
	case strings.Contains(m, "discovery") || strings.Contains(m, "campaign") || strings.Contains(m, "benefit") || strings.Contains(m, "inventory"):
		return "discovery"
	}
	switch strings.ToUpper(level) {
	case "ERROR", "WARN":
		return "error"
	}
	return "info"
}

// colorForKind maps a structured event kind to a CSS variable name.
// `level` is consulted as a fallback so unknown kinds still surface
// errors in red rather than the muted "info" grey.
func colorForKind(kind, level string) string {
	switch kind {
	case "claim":
		return "green"
	case "progress":
		return "amber"
	case "state":
		return "blue"
	case "discovery":
		return "muted"
	case "error":
		return "red"
	case "auth":
		return "accent"
	}
	switch strings.ToUpper(level) {
	case "ERROR":
		return "red"
	case "WARN":
		return "amber"
	}
	return "muted"
}

// detailsFor flattens the structured fields of a log line into a
// stable-ordered slice for rendering under each event row. Keys we
// surface first (account, channel, campaign, benefit, state) get a
// consistent ordering; remaining keys are sorted alphabetically.
// The `kind` field is dropped because it already appears in the row
// header as the colored chip.
func detailsFor(l mlog.LogLine) []dashEventField {
	if len(l.Fields) == 0 {
		return nil
	}
	priority := []string{"account", "platform", "state", "prev", "campaign", "game", "channel", "benefit", "benefit_name", "min_watched", "required", "err"}
	seen := map[string]bool{}
	out := make([]dashEventField, 0, len(l.Fields))
	for _, k := range priority {
		if v, ok := l.Fields[k]; ok {
			out = append(out, dashEventField{Key: k, Value: fmt.Sprintf("%v", v)})
			seen[k] = true
		}
	}
	rest := make([]string, 0, len(l.Fields))
	for k := range l.Fields {
		if k == "kind" || seen[k] {
			continue
		}
		rest = append(rest, k)
	}
	sort.Strings(rest)
	for _, k := range rest {
		out = append(out, dashEventField{Key: k, Value: fmt.Sprintf("%v", l.Fields[k])})
	}
	return out
}

func fieldStr(f map[string]any, k string) string {
	if v, ok := f[k]; ok {
		return fmt.Sprintf("%v", v)
	}
	return "—"
}

func htmlEscape(s string) string {
	return html.EscapeString(s)
}

// translateKind localises the event kind chip. The kind keys are all
// present in the locale files, but i18n.T echoes the key back when a
// translation is missing, so we fall back to the raw kind word in that
// case to avoid leaking "event.kind.foo" into the UI.
func translateKind(lang, kind string) string {
	key := "event.kind." + kind
	if v := i18n.T(lang, key); v != key {
		return v
	}
	return htmlEscape(kind)
}

// translateMsg localises a static log message when it has a known i18n
// key; otherwise it falls back to the raw (HTML-escaped) message so
// fmt.Sprintf-built / unkeyed lines still render.
func translateMsg(lang, msg string) string {
	if key, ok := eventMsgKeys[msg]; ok {
		if v := i18n.T(lang, key); v != key {
			return v
		}
	}
	return htmlEscape(msg)
}
