// Package web is the embedded server-rendered UI: standard library net/http
// and html/template, no JavaScript, no frontend build step, all assets
// embedded.
//
// Security posture (docs/threat-model.md): every route except /login and
// /static requires a session; the session cookie is HttpOnly and
// SameSite=Strict; state-changing requests are Origin-checked; login
// compares a hash of the admin secret in constant time and sleeps on
// failure. The request log records method, path, and status only — never a
// query string, form value, or entity field, because paths carry only
// opaque ids and slugs.
package web

import (
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"errors"
	"fmt"
	htmpl "html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/iotemylabs/prepkit/internal/model"
	"github.com/iotemylabs/prepkit/internal/readiness"
	"github.com/iotemylabs/prepkit/internal/store"
	tpack "github.com/iotemylabs/prepkit/internal/template"
)

//go:embed templates static
var assets embed.FS

type Server struct {
	st         *store.Store
	pack       *tpack.Pack
	secretHash [32]byte
	sessions   *sessionStore
	pages      map[string]*htmpl.Template
	log        *slog.Logger
}

const sessionCookie = "prepkit_session"

func New(st *store.Store, pack *tpack.Pack, adminSecret string, logger *slog.Logger) *Server {
	s := &Server{
		st:         st,
		pack:       pack,
		secretHash: sha256.Sum256([]byte(adminSecret)),
		sessions:   newSessionStore(7 * 24 * time.Hour),
		pages:      map[string]*htmpl.Template{},
		log:        logger,
	}

	funcs := htmpl.FuncMap{
		// eqs compares by string form, so typed enums compare against
		// their literal values in templates without type juggling.
		"eqs": func(a, b any) bool { return fmt.Sprint(a) == fmt.Sprint(b) },
		"catColor": func(c model.Category) string {
			color, ok := c.Color()
			if !ok {
				return "neutral"
			}
			return color
		},
		"fq":  fmtFloat,
		"fqp": fmtFloatPtr,
		"pct": func(c readiness.Coverage) string {
			f, ok := c.Fraction()
			if !ok {
				return "n/a"
			}
			return fmt.Sprintf("%d%%", int(f*100+0.5))
		},
		"statusClass": func(s readiness.LineStatus) string { return "st-" + string(s) },
	}

	withBase := []string{
		"dashboard.html", "household.html", "containers.html", "container.html",
		"kits.html", "kit_new.html", "kit.html", "kit_readiness.html",
		"item_new.html", "item.html", "documents.html", "document.html",
		"templates_list.html", "template_view.html", "exports.html", "error.html",
	}
	for _, p := range withBase {
		s.pages[p] = htmpl.Must(htmpl.New("").Funcs(funcs).ParseFS(assets,
			"templates/base.html", "templates/"+p))
	}
	// Standalone pages define the whole document themselves (no nav chrome):
	// the login form, and the printable QR label.
	for _, p := range []string{"login.html", "label.html"} {
		s.pages[p] = htmpl.Must(htmpl.New("").Funcs(funcs).ParseFS(assets, "templates/"+p))
	}
	return s
}

func fmtFloat(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func fmtFloatPtr(v *float64) string {
	if v == nil {
		return ""
	}
	return fmtFloat(*v)
}

// Handler returns the fully wired route table behind the middleware chain.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("GET /static/", http.FileServerFS(assets))
	mux.HandleFunc("GET /login", s.loginForm)
	mux.HandleFunc("POST /login", s.loginPost)
	mux.HandleFunc("POST /logout", s.logout)

	mux.HandleFunc("GET /{$}", s.dashboard)

	mux.HandleFunc("GET /household", s.householdPage)
	mux.HandleFunc("POST /household", s.householdSave)
	mux.HandleFunc("POST /persons", s.personCreate)
	mux.HandleFunc("POST /persons/{id}", s.personUpdate)
	mux.HandleFunc("POST /persons/{id}/delete", s.personDelete)

	mux.HandleFunc("GET /containers", s.containersPage)
	mux.HandleFunc("POST /containers", s.containerCreate)
	mux.HandleFunc("GET /containers/{id}", s.containerPage)
	mux.HandleFunc("POST /containers/{id}", s.containerUpdate)
	mux.HandleFunc("POST /containers/{id}/delete", s.containerDelete)
	mux.HandleFunc("GET /containers/{id}/label", s.containerLabel)
	mux.HandleFunc("GET /containers/{id}/qr.png", s.containerQRPNG)
	mux.HandleFunc("GET /qr/{slug}", s.qrResolve)

	mux.HandleFunc("GET /kits", s.kitsPage)
	mux.HandleFunc("GET /kits/new", s.kitNewForm)
	mux.HandleFunc("POST /kits", s.kitCreate)
	mux.HandleFunc("GET /kits/{id}", s.kitPage)
	mux.HandleFunc("POST /kits/{id}", s.kitUpdate)
	mux.HandleFunc("POST /kits/{id}/delete", s.kitDelete)
	mux.HandleFunc("POST /kits/{id}/verify", s.kitVerify)
	mux.HandleFunc("GET /kits/{id}/readiness", s.kitReadiness)

	mux.HandleFunc("GET /items/new", s.itemNewForm)
	mux.HandleFunc("POST /items", s.itemCreate)
	mux.HandleFunc("GET /items/{id}", s.itemPage)
	mux.HandleFunc("POST /items/{id}", s.itemUpdate)
	mux.HandleFunc("POST /items/{id}/delete", s.itemDelete)
	mux.HandleFunc("POST /items/{id}/check", s.itemCheck)
	mux.HandleFunc("POST /items/{id}/lots", s.lotCreate)
	mux.HandleFunc("POST /lots/{id}", s.lotUpdate)
	mux.HandleFunc("POST /lots/{id}/delete", s.lotDelete)

	mux.HandleFunc("GET /documents", s.documentsPage)
	mux.HandleFunc("POST /documents", s.documentCreate)
	mux.HandleFunc("GET /documents/{id}", s.documentPage)
	mux.HandleFunc("POST /documents/{id}", s.documentUpdate)
	mux.HandleFunc("POST /documents/{id}/delete", s.documentDelete)

	mux.HandleFunc("GET /templates", s.templatesPage)
	mux.HandleFunc("GET /templates/{id}", s.templatePage)

	mux.HandleFunc("GET /exports", s.exportsPage)
	mux.HandleFunc("GET /exports/inventory.json", s.exportJSON)
	mux.HandleFunc("GET /exports/inventory-full.json", s.exportJSONFull)
	mux.HandleFunc("GET /exports/offline.zip", s.exportOfflineZip)

	return s.logRequests(s.checkOrigin(s.requireAuth(mux)))
}

// --- middleware ---------------------------------------------------------

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "/login" || strings.HasPrefix(p, "/static/") {
			next.ServeHTTP(w, r)
			return
		}
		if p == "/favicon.ico" {
			http.NotFound(w, r) // no icon shipped; a 404 beats a login redirect
			return
		}
		if c, err := r.Cookie(sessionCookie); err == nil && s.sessions.valid(c.Value) {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodGet {
			http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.Path), http.StatusSeeOther)
			return
		}
		http.Error(w, "not authenticated", http.StatusUnauthorized)
	})
}

// checkOrigin rejects state-changing requests whose Origin (or, failing
// that, Referer) names a different host. Together with SameSite=Strict this
// is the CSRF defence; there is no JavaScript in this application to need
// anything more elaborate.
func (s *Server) checkOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			source := r.Header.Get("Origin")
			if source == "" {
				source = r.Header.Get("Referer")
			}
			if source != "" {
				u, err := url.Parse(source)
				if err != nil || u.Host != r.Host {
					http.Error(w, "cross-origin request rejected", http.StatusForbidden)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r)
		// Method, path, status, duration. Never query strings or form
		// values: redaction by classification means sensitive fields are
		// structurally unable to appear here.
		s.log.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"ms", time.Since(start).Milliseconds())
	})
}

// --- auth handlers ------------------------------------------------------

func (s *Server) loginForm(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusOK, "login.html", map[string]any{
		"Next": r.URL.Query().Get("next"),
	})
}

func (s *Server) loginPost(w http.ResponseWriter, r *http.Request) {
	got := sha256.Sum256([]byte(r.FormValue("secret")))
	if subtle.ConstantTimeCompare(got[:], s.secretHash[:]) != 1 {
		time.Sleep(500 * time.Millisecond) // blunt brute-force damper
		s.render(w, http.StatusUnauthorized, "login.html", map[string]any{
			"Error": "wrong admin secret",
			"Next":  r.FormValue("next"),
		})
		return
	}
	token := s.sessions.create()
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	next := r.FormValue("next")
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		next = "/"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.sessions.revoke(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// --- shared helpers -----------------------------------------------------

func (s *Server) render(w http.ResponseWriter, status int, page string, data any) {
	t, ok := s.pages[page]
	if !ok {
		http.Error(w, "template missing: "+page, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := t.ExecuteTemplate(w, "base", data); err != nil {
		s.log.Error("render", "page", page, "err", err.Error())
	}
}

// fail maps store errors onto HTTP statuses with a readable page. Store
// error text carries entity ids, never field content, so it is safe to
// show and to log.
func (s *Server) fail(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, store.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, store.ErrValidation):
		status = http.StatusBadRequest
	case errors.Is(err, store.ErrContainerCycle),
		errors.Is(err, store.ErrHasLots),
		errors.Is(err, store.ErrInUse):
		status = http.StatusConflict
	}
	if status == http.StatusInternalServerError {
		s.log.Error("handler", "err", err.Error())
	}
	s.render(w, status, "error.html", map[string]any{"Error": err.Error(), "Status": status})
}

func errValidationf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", store.ErrValidation, fmt.Sprintf(format, args...))
}

func formPtrFloat(r *http.Request, name string) (*float64, error) {
	v := strings.TrimSpace(r.FormValue(name))
	if v == "" {
		return nil, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return nil, fmt.Errorf("%w: %s is not a number", store.ErrValidation, name)
	}
	return &f, nil
}

func formFloat(r *http.Request, name string) (float64, error) {
	v := strings.TrimSpace(r.FormValue(name))
	if v == "" {
		return 0, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %s is not a number", store.ErrValidation, name)
	}
	return f, nil
}

func today() string { return time.Now().UTC().Format(model.DateLayout) }
