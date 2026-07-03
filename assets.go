package main

import (
	"embed"
	"html/template"
	"net/http"
	"net/url"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

var tmpl *template.Template

func initTemplates() {
	tmpl = template.Must(template.New("").Funcs(template.FuncMap{
		"truncate": func(s string, n int) string {
			if len(s) > n {
				return s[:n] + "…"
			}
			return s
		},
		"queryenc": func(s string) string {
			return url.QueryEscape(s)
		},
	}).ParseFS(templateFS, "templates/*.html"))
}

// render executes a full-page template (each page pulls in shared head/nav partials).
func render(w http.ResponseWriter, page string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, page, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// renderFragment executes a standalone fragment (for htmx responses, no <html>).
func renderFragment(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
