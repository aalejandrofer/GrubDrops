// Package i18n provides lightweight internationalisation for the GrubDrops
// miner's server-side strings (notifications, summaries). Locale files are
// embedded at compile time via //go:embed and keyed by flat dot-separated
// paths (e.g. "summary.title").
//
// To add a new language:
//  1. Copy locales/en.json to locales/<lang>.json
//  2. Translate the values
//  3. Restart — the new locale is loaded automatically.
package i18n

import (
	"embed"
	"encoding/json"
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

// Supported lists the locales the miner ships; each must have every key in en.
var Supported = []string{"en", "zh-CN", "es", "tr"}

const defaultLang = "en"

func init() {
	// Auto-load locales at package init so T works in tests too.
	_ = Load()
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
