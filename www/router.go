package www

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"waralert/action"
	"waralert/audit"
	"waralert/chain"
	"waralert/config"
	"waralert/plcman"
	"waralert/sms"
)

// Handlers holds all HTTP handlers for the waralert web UI.
type Handlers struct {
	cfg          *config.Config
	configPath   string
	certReloader *CertReloader
	plcManager   *plcman.Manager
	chainMgr     *chain.Manager
	actionReg    *action.Registry
	smsMgr       *sms.Manager
	smsHandler   *sms.Handler
	auditLog     *audit.Logger
	sessions     *sessionStore
	tmpl         *template.Template
	eventHub     *EventHub
}

// NewRouter creates the waralert web UI router and returns a stop function for cleanup.
func NewRouter(
	cfg *config.Config,
	configPath string,
	certReloader *CertReloader,
	plcMgr *plcman.Manager,
	chainMgr *chain.Manager,
	actionReg *action.Registry,
	smsMgr *sms.Manager,
	smsHandler *sms.Handler,
	auditLog *audit.Logger,
) (chi.Router, func()) {
	h := &Handlers{
		cfg:          cfg,
		configPath:   configPath,
		certReloader: certReloader,
		plcManager:   plcMgr,
		chainMgr:     chainMgr,
		actionReg:    actionReg,
		smsMgr:       smsMgr,
		smsHandler:   smsHandler,
		auditLog:     auditLog,
		sessions:     newSessionStore(cfg.Web.SessionSecret),
		eventHub:     newEventHub(),
	}

	h.tmpl = template.Must(template.New("").Funcs(template.FuncMap{
		"isAdmin":   isAdmin,
		"lower":     strings.ToLower,
		"upper":     strings.ToUpper,
		"cacheBust": func() string { return fmt.Sprintf("%x", time.Now().UnixNano()) },
		"json": func(v interface{}) template.JS {
			b, _ := json.Marshal(v)
			return template.JS(b)
		},
		"join": strings.Join,
		"fmtDuration": func(sec int) string {
			if sec <= 0 {
				return ""
			}
			m := sec / 60
			s := sec % 60
			if m > 0 && s > 0 {
				return fmt.Sprintf("%d min %d sec", m, s)
			} else if m > 0 {
				return fmt.Sprintf("%d min", m)
			}
			return fmt.Sprintf("%d sec", s)
		},
	}).ParseFS(templatesFS, "templates/*.html", "templates/partials/*.html"))

	r := chi.NewRouter()

	// Favicon: serve with no-cache headers to defeat aggressive browser caching (Safari).
	faviconData, _ := fs.ReadFile(staticFS, "static/favicon.ico")
	faviconHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/x-icon")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		w.Write(faviconData)
	})
	r.Handle("/favicon.ico", faviconHandler)
	r.Handle("/static/favicon.ico", faviconHandler)

	// Static files (public)
	staticSub, _ := fs.Sub(staticFS, "static")
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	// Setup (public - only functional when no users exist)
	r.Get("/setup", h.handleSetupPage)
	r.Post("/setup", h.handleSetupSubmit)

	// Login/logout (public)
	r.Get("/login", h.handleLoginPage)
	r.Post("/login", h.handleLoginSubmit)
	r.Post("/logout", h.handleLogout)

	// SMS incoming webhooks (public - validated by signature)
	r.Post("/api/sms/incoming/smsgate", smsHandler.HandleSMSGateIncoming)
	r.Post("/api/sms/incoming/twilio", smsHandler.HandleTwilioIncoming)

	// Protected routes
	r.Group(func(r chi.Router) {
		r.Use(h.authMiddleware)

		// SSE endpoint
		r.Get("/events", h.handleSSE)

		// Pages
		r.Get("/", h.handleChainsPage)
		r.Get("/sources", h.handleSourcesRedirect)
		r.Get("/chains", h.handleChainsPage)
		r.Get("/subscribers", h.handleSubscribersPage)
		r.Get("/providers", h.handleProvidersPage)
		r.Get("/history", h.handleHistoryPage)
		r.Get("/settings", h.handleSettingsPage)

		// htmx partials (polling)
		r.Get("/htmx/sources", h.handleSourcesPartial)
		r.Get("/htmx/chains", h.handleChainsPartial)
		r.Get("/htmx/subscribers", h.handleSubscribersPartial)
		r.Get("/htmx/history", h.handleHistoryPartial)
		r.Get("/htmx/users", h.handleUsersPartial)

		// Admin actions
		r.Group(func(r chi.Router) {
			r.Use(h.adminOnlyMiddleware)

			// Chain actions
			r.Post("/htmx/chains", h.handleChainCreate)
			r.Get("/htmx/chains/{name}", h.handleChainGet)
			r.Put("/htmx/chains/{name}", h.handleChainUpdate)
			r.Delete("/htmx/chains/{name}", h.handleChainDelete)
			r.Post("/htmx/chains/{name}/start", h.handleChainStart)
			r.Post("/htmx/chains/{name}/stop", h.handleChainStop)
			r.Post("/htmx/chains/{name}/test", h.handleChainTestFire)
			r.Post("/htmx/chains/{name}/gates", h.handleChainGates)

			// Source actions
			r.Post("/htmx/sources", h.handleSourceCreate)
			r.Get("/htmx/sources/{name}", h.handleSourceGet)
			r.Put("/htmx/sources/{name}", h.handleSourceUpdate)
			r.Delete("/htmx/sources/{name}", h.handleSourceDelete)

			// Direct PLC actions (connect/disconnect for PLC-type sources)
			r.Post("/htmx/plcs/{name}/connect", h.handlePLCConnect)
			r.Post("/htmx/plcs/{name}/disconnect", h.handlePLCDisconnect)

			// PLC tag management
			r.Get("/htmx/plcs/{name}/tags", h.handlePLCTagList)
			r.Post("/htmx/plcs/{name}/tags", h.handlePLCTagAdd)
			r.Delete("/htmx/plcs/{name}/tags/{tag}", h.handlePLCTagRemove)

			// Tag picker data
			r.Get("/htmx/plc-tags/{plc}", h.handlePLCTags)

			// Subscriber actions
			r.Post("/htmx/subscribers", h.handleSubscriberCreate)
			r.Put("/htmx/subscribers/{phone}", h.handleSubscriberUpdate)
			r.Delete("/htmx/subscribers/{phone}", h.handleSubscriberDelete)
			r.Patch("/htmx/subscribers/{phone}", h.handleSubscriberToggle)

			// Topic actions
			r.Post("/htmx/topics", h.handleTopicCreate)
			r.Delete("/htmx/topics/{topic}", h.handleTopicDelete)

			// User management
			r.Post("/htmx/users", h.handleUserCreate)
			r.Put("/htmx/users/{username}", h.handleUserUpdate)
			r.Delete("/htmx/users/{username}", h.handleUserDelete)

			// Provider config
			r.Post("/htmx/providers/sms", h.handleSMSProviderUpdate)
			r.Post("/htmx/providers/email", h.handleEmailProviderUpdate)
			r.Post("/htmx/providers/sms/test", h.handleSMSTest)
			r.Post("/htmx/providers/sms/test-connection", h.handleSMSTestConnection)
			r.Post("/htmx/providers/sms/register-webhook", h.handleSMSRegisterWebhook)
			r.Post("/htmx/providers/sms/clean-webhooks", h.handleSMSCleanWebhooks)
			r.Post("/htmx/providers/sms/request-cert", h.handleSMSRequestCert)
			r.Get("/htmx/providers/sms/webhook-status", h.handleSMSWebhookStatus)
			r.Post("/htmx/providers/email/test", h.handleEmailTest)
			r.Post("/htmx/providers/external-url", h.handleExternalURLUpdate)
		})
	})

	return r, func() { h.eventHub.Stop() }
}

// authMiddleware checks if the user is authenticated.
func (h *Handlers) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(h.cfg.Web.Users) == 0 {
			if r.Header.Get("HX-Request") == "true" {
				w.Header().Set("HX-Redirect", "/setup")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}

		username, _, ok := h.sessions.getUser(r)
		if !ok || username == "" {
			if r.Header.Get("HX-Request") == "true" {
				w.Header().Set("HX-Redirect", "/login")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		user := h.cfg.FindWebUser(username)
		if user == nil {
			h.sessions.clear(w, r)
			if r.Header.Get("HX-Request") == "true" {
				w.Header().Set("HX-Redirect", "/login")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// adminOnlyMiddleware checks if the user has admin role.
func (h *Handlers) adminOnlyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, role, ok := h.sessions.getUser(r)
		if !ok || !isAdmin(role) {
			if r.Header.Get("HX-Request") == "true" {
				http.Error(w, "Forbidden: Admin access required", http.StatusForbidden)
				return
			}
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// renderTemplate renders a template with common data.
func (h *Handlers) renderTemplate(w http.ResponseWriter, name string, data map[string]interface{}) {
	if data == nil {
		data = make(map[string]interface{})
	}
	if err := h.tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// getUserInfo returns the current user info for templates.
func (h *Handlers) getUserInfo(r *http.Request) map[string]interface{} {
	username, role, _ := h.sessions.getUser(r)
	return map[string]interface{}{
		"Username": username,
		"Role":     role,
		"IsAdmin":  isAdmin(role),
	}
}
