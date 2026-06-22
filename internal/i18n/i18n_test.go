package i18n

import "testing"

// TestLocaleParity ensures every supported locale defines exactly the same key
// set as the default (en) locale — no missing keys, no stray extras.
func TestLocaleParity(t *testing.T) {
	if err := Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	base, ok := locales[defaultLang]
	if !ok {
		t.Fatalf("default locale %q not loaded", defaultLang)
	}
	for _, lang := range Supported {
		if lang == defaultLang {
			continue
		}
		m, ok := locales[lang]
		if !ok {
			t.Errorf("supported locale %q not loaded", lang)
			continue
		}
		for k := range base {
			if _, ok := m[k]; !ok {
				t.Errorf("locale %q missing key %q", lang, k)
			}
		}
		for k := range m {
			if _, ok := base[k]; !ok {
				t.Errorf("locale %q has extra key %q not in %q", lang, k, defaultLang)
			}
		}
	}
}

// TestSpanishLoaded sanity-checks the Spanish locale resolves a known key.
func TestSpanishLoaded(t *testing.T) {
	if err := Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := T("es", "nav.settings"); got != "Ajustes" {
		t.Errorf("T(es, nav.settings) = %q, want %q", got, "Ajustes")
	}
	if !isSupported("es") {
		t.Error("es should be a supported language")
	}
}
