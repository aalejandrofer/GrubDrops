package api

import (
	"bytes"
	"io"
	"net/http"
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
}

func render(w http.ResponseWriter, t Renderer, name string, data templateData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(buf.Bytes())
}

func renderPartial(w http.ResponseWriter, t Renderer, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(buf.Bytes())
}
