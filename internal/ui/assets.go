package ui

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Embed static assets using Go 1.16+ embed package
//go:embed ../../web/static
var staticFiles embed.FS

//go:embed ../../web/templates
var templateFiles embed.FS

// UI manages the web user interface
type UI struct {
	staticFS   fs.FS
	templates  *template.Template
	version    string
}

// TemplateData holds data passed to HTML templates
type TemplateData struct {
	Title   string
	Version string
	Data    interface{}
}

// New creates a new UI instance with embedded assets
func New(version string) (*UI, error) {
	// Create sub-filesystem for static files
	staticFS, err := fs.Sub(staticFiles, "web/static")
	if err != nil {
		return nil, err
	}

	// Parse HTML templates
	templates, err := template.ParseFS(templateFiles, "web/templates/*.html")
	if err != nil {
		return nil, err
	}

	return &UI{
		staticFS:  staticFS,
		templates: templates,
		version:   version,
	}, nil
}

// RegisterRoutes adds UI routes to the chi router
func (u *UI) RegisterRoutes(r chi.Router) {
	// Serve static files
	r.Handle("/static/*", u.serveStatic())
	
	// Serve main UI
	r.Get("/", u.serveIndex)
}

// serveStatic creates a handler for static asset serving
func (u *UI) serveStatic() http.Handler {
	return http.StripPrefix("/static/", http.FileServer(http.FS(u.staticFS)))
}

// serveIndex serves the main application HTML
func (u *UI) serveIndex(w http.ResponseWriter, r *http.Request) {
	data := TemplateData{
		Title:   "Home",
		Version: u.version,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	
	if err := u.templates.ExecuteTemplate(w, "index.html", data); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

// GetStaticFS returns the embedded static file system
// Useful for testing or advanced use cases
func (u *UI) GetStaticFS() fs.FS {
	return u.staticFS
}

// GetTemplates returns the parsed templates
// Useful for testing or adding custom templates
func (u *UI) GetTemplates() *template.Template {
	return u.templates
}