// Package i18n provides lightweight internationalisation for the GrubDrops web
// UI. Locale files are embedded at compile time via //go:embed and keyed by
// flat dot-separated paths (e.g. "nav.console", "settings.save").
//
// To add a new language:
//  1. Copy locales/en.json to locales/<lang>.json
//  2. Translate the values
//  3. Restart — the new locale is loaded automatically.
package i18n

import (
	"embed"
	"encoding/json"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

//go:embed locales/*.json
var localeFS embed.FS

// locales holds all loaded translations: locales["en"]["nav.console"] = "Console"
var (
	locales  map[string]map[string]string
	loadOnce sync.Once
	loadErr  error
)

// Supported lists the BCP-47 tags the UI exposes.
var Supported = []string{"en", "zh-CN", "es"}

const defaultLang = "en"

func init() {
	// Auto-load locales at package init so templates work in tests too.
	_ = Load()
}

// Per-request language storage keyed by goroutine ID.
var langStore sync.Map // int → string

// getGoroutineID extracts the current goroutine's ID from the runtime stack.
func getGoroutineID() int {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	id := 0
	for i := 10; i < n; i++ { // skip "goroutine "
		if buf[i] < '0' || buf[i] > '9' {
			break
		}
		id = id*10 + int(buf[i]-'0')
	}
	return id
}

// SetLang stores the language for the current goroutine.
func SetLang(lang string) {
	langStore.Store(getGoroutineID(), lang)
}

// ClearLang removes the language entry for the current goroutine.
func ClearLang() {
	langStore.Delete(getGoroutineID())
}

// currentLang reads back the language set by SetLang for this goroutine.
func currentLang() string {
	if v, ok := langStore.Load(getGoroutineID()); ok {
		return v.(string)
	}
	return defaultLang
}

// Load reads all embedded locale JSON files. Safe to call multiple times.
func Load() error {
	loadOnce.Do(func() {
		entries, err := localeFS.ReadDir("locales")
		if err != nil {
			loadErr = err
			return
		}
		locales = make(map[string]map[string]string, len(entries))
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			lang := strings.TrimSuffix(e.Name(), ".json")
			raw, err := localeFS.ReadFile("locales/" + e.Name())
			if err != nil {
				loadErr = err
				return
			}
			var m map[string]string
			if err := json.Unmarshal(raw, &m); err != nil {
				loadErr = err
				return
			}
			locales[lang] = m
		}
	})
	return loadErr
}

// T returns the translation for key in the given language.
func T(lang, key string) string {
	if locales == nil {
		return key
	}
	if m, ok := locales[lang]; ok {
		if v, ok := m[key]; ok {
			return v
		}
	}
	if lang != defaultLang {
		if m, ok := locales[defaultLang]; ok {
			if v, ok := m[key]; ok {
				return v
			}
		}
	}
	return key
}

// DetectLang determines the preferred UI language from the request.
// Priority: "lang" cookie → Accept-Language header → defaultLang.
func DetectLang(r *http.Request) string {
	if r == nil {
		return defaultLang
	}
	// 1. Cookie (user override).
	if c, err := r.Cookie("lang"); err == nil && c.Value != "" {
		v := strings.TrimSpace(c.Value)
		if isSupported(v) {
			return v
		}
	}
	// 2. Accept-Language header.
	if al := r.Header.Get("Accept-Language"); al != "" {
		if best := pickBest(al); best != "" {
			return best
		}
	}
	return defaultLang
}

// TemplateFunc returns a template function that translates keys using the
// language stored for the current goroutine by SetLang.
func TemplateFunc() func(key string) string {
	return func(key string) string {
		return T(currentLang(), key)
	}
}

func isSupported(tag string) bool {
	for _, s := range Supported {
		if strings.EqualFold(s, tag) {
			return true
		}
	}
	return false
}

func pickBest(header string) string {
	type candidate struct {
		tag string
		q   float64
	}
	var cands []candidate
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tag := part
		q := 1.0
		if idx := strings.Index(part, ";q="); idx >= 0 {
			tag = strings.TrimSpace(part[:idx])
			if parsed, err := strconv.ParseFloat(strings.TrimSpace(part[idx+3:]), 64); err == nil {
				q = parsed
			} else {
				q = 0 // invalid quality = skip
			}
		}
		cands = append(cands, candidate{tag: tag, q: q})
	}

	bestTag := ""
	bestQ := -1.0
	for _, c := range cands {
		if c.q <= 0 || (c.q <= bestQ && bestTag != "") {
			continue
		}
		if isSupported(c.tag) {
			bestTag = c.tag
			bestQ = c.q
			continue
		}
		lower := strings.ToLower(c.tag)
		for _, s := range Supported {
			if strings.HasPrefix(strings.ToLower(s), lower) {
				bestTag = s
				bestQ = c.q
				break
			}
		}
	}
	return bestTag
}
