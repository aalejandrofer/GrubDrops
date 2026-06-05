package web

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"strings"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

// Static returns an fs.FS rooted at the embedded static/ directory.
// Callers can serve it under any URL prefix via http.FileServer(http.FS(web.Static())).
func Static() fs.FS {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		// Embed-time guarantee: if the directory exists at build time this never fires.
		panic(err)
	}
	return sub
}

// PageTemplates holds per-page clones of the base template set so that each
// page's {{define "title"}} and {{define "content"}} blocks correctly override
// the layout's {{block}} directives without conflicting with other pages.
type PageTemplates struct {
	base  *template.Template            // shared partials (nav, layout, dashboard_mining_columns)
	pages map[string]*template.Template // per-page clones
}

// ExecuteTemplate renders the named page or partial template into w.
// Page templates (e.g. "setup.html") are rendered from their per-page clone
// so that their "title"/"content" block overrides apply. Partial templates
// (e.g. "dashboard_mining_columns") are rendered from the shared base set.
func (p *PageTemplates) ExecuteTemplate(w io.Writer, name string, data any) error {
	if tmpl, ok := p.pages[name]; ok {
		return tmpl.ExecuteTemplate(w, name, data)
	}
	// Fall back to base set for partials.
	if tmpl := p.base.Lookup(name); tmpl != nil {
		return tmpl.Execute(w, data)
	}
	return fmt.Errorf("template %q not found", name)
}

// Lookup returns the named template from the base set, or nil.
func (p *PageTemplates) Lookup(name string) *template.Template {
	return p.base.Lookup(name)
}

var sharedFuncs = template.FuncMap{
	"safe": func(s string) template.HTML { return template.HTML(s) },
	"not": func(v any) bool {
		if v == nil {
			return true
		}
		switch val := v.(type) {
		case bool:
			return !val
		case int:
			return val == 0
		case int64:
			return val == 0
		case string:
			return val == ""
		default:
			return false
		}
	},
}

// Templates parses all templates and returns a PageTemplates value.
func Templates() (*PageTemplates, error) {
	matches, err := fs.Glob(templatesFS, "templates/*.html")
	if err != nil {
		return nil, err
	}

	// Partition into base/shared files and page files.
	// Base files: names starting with "_" or the named dashboard partials.
	var baseFiles []string
	var pageFiles []string
	for _, m := range matches {
		name := m[len("templates/"):]
		if strings.HasPrefix(name, "_") || name == "dashboard_mining_columns.html" || name == "dashboard_events.html" || name == "dashboard_campaign_modal.html" || name == "login_twitch_status.html" {
			baseFiles = append(baseFiles, m)
		} else {
			pageFiles = append(pageFiles, m)
		}
	}

	// Build the shared base template set.
	base := template.New("").Funcs(sharedFuncs)
	for _, m := range baseFiles {
		raw, err := templatesFS.ReadFile(m)
		if err != nil {
			return nil, err
		}
		if _, err := base.Parse(string(raw)); err != nil {
			return nil, fmt.Errorf("parse base template %s: %w", m, err)
		}
	}

	// For each page file, clone the base and parse the page-specific defines.
	pages := make(map[string]*template.Template, len(pageFiles))
	for _, m := range pageFiles {
		raw, err := templatesFS.ReadFile(m)
		if err != nil {
			return nil, err
		}
		cloned, err := base.Clone()
		if err != nil {
			return nil, err
		}
		if _, err := cloned.Parse(string(raw)); err != nil {
			return nil, fmt.Errorf("parse page template %s: %w", m, err)
		}
		name := m[len("templates/"):]
		pages[name] = cloned
	}

	return &PageTemplates{base: base, pages: pages}, nil
}
