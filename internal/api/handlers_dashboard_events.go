package api

// Live-events drawer helpers, split out of handlers_dashboard.go: turning
// the in-memory log ring into the dashboard's event model (filtering,
// kind classification, colour, and detail flattening).

import (
	"fmt"
	"sort"
	"strings"
	"time"

	mlog "github.com/aalejandrofer/grubdrops/internal/log"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// eventsFromRing transforms the in-memory log ring into the
// dashboard's event drawer model. The ring stores typed entries
// (LogLine.Kind, set by the watcher / login handlers / ringHandler);
// when Kind is empty we fall back to substring matching on the message
// so older un-tagged log lines still classify usefully.
//
// `kindFilter` is one of "" / "all" / "claim" / "progress" / "state" /
// "discovery" / "error" / "auth"; anything else is treated as "all".
// `accountFilter` is the account ID to keep ("" or "all" = keep all).
func eventsFromRing(ring *mlog.Ring, kindFilter, accountFilter string, accs []gen.Account, loc *time.Location) []dashEvent {
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
			BodyHTML: fmt.Sprintf("<em>%s</em> · %s", kind, htmlEscape(l.Msg)),
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
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}
