package web

import (
	"embed"
	"html/template"
	"io/fs"
)

//go:embed templates/*.html
var templatesFS embed.FS

func Templates() (*template.Template, error) {
	t := template.New("").Funcs(template.FuncMap{
		"safe": func(s string) template.HTML { return template.HTML(s) },
	})
	matches, err := fs.Glob(templatesFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	for _, m := range matches {
		raw, err := templatesFS.ReadFile(m)
		if err != nil {
			return nil, err
		}
		if _, err := t.New(m).Parse(string(raw)); err != nil {
			return nil, err
		}
	}
	return t, nil
}
