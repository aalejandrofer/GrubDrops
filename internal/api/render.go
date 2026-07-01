package api

import (
	"bytes"
	"io"
	"net/http"

	"github.com/aalejandrofer/grubdrops/internal/i18n"
)

// Renderer is satisfied by *web.PageTemplates and by *template.Template.
type Renderer interface {
	ExecuteTemplate(w io.Writer, name string, data any) error
}

type templateData struct {
	AuthedAdmin      bool
	CSRFToken        string
	Page             any
	Flash            string
	Active           string // "dashboard" | "accounts" | "drops" | "settings" — for nav highlight
	AccountsRows     any    // optional: inline accounts table on settings page
	OIDCEnabled      bool   // show the SSO button on the login page
	OIDCProviderName string // SSO button label
	Lang             string // current language code (e.g. "en", "zh-CN")
	UpdateAvailable  bool   // a newer GitHub release exists
	LatestRelease    string // latest release tag, for the nav badge
	Timezone         string // IANA display zone (e.g. "Asia/Shanghai") for the client clock
}

func render(w http.ResponseWriter, r *http.Request, t Renderer, name string, data templateData) {
	// Detect and set language for the template function "t".
	lang := i18n.DetectLang(r)
	data.Lang = lang
	data.Timezone = "UTC"
	if displayZone != nil {
		data.Timezone = displayZone.Name()
	}
	data.UpdateAvailable, data.LatestRelease = updateInfoFromContext(r.Context())
	i18n.SetLang(lang)
	defer i18n.ClearLang()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(buf.Bytes())
}

func renderPartial(w http.ResponseWriter, r *http.Request, t Renderer, name string, data any) {
	lang := i18n.DetectLang(r)
	i18n.SetLang(lang)
	defer i18n.ClearLang()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(buf.Bytes())
}
