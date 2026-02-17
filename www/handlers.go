package www

import (
	"net"
	"net/http"

	"waralert/config"
)

// --- Login / Setup / Logout ---

func (h *Handlers) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	h.tmpl.ExecuteTemplate(w, "login.html", nil)
}

func (h *Handlers) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	password := r.FormValue("password")

	user := h.cfg.FindWebUser(username)
	if user == nil || !checkPassword(password, user.PasswordHash) {
		h.tmpl.ExecuteTemplate(w, "login.html", map[string]interface{}{
			"Error": "Invalid username or password",
		})
		return
	}

	h.sessions.setUser(w, r, username, user.Role)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handlers) handleSetupPage(w http.ResponseWriter, r *http.Request) {
	if len(h.cfg.Web.Users) > 0 {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	h.tmpl.ExecuteTemplate(w, "setup.html", nil)
}

func (h *Handlers) handleSetupSubmit(w http.ResponseWriter, r *http.Request) {
	if len(h.cfg.Web.Users) > 0 {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	if username == "" || password == "" {
		h.tmpl.ExecuteTemplate(w, "setup.html", map[string]interface{}{
			"Error": "Username and password are required",
		})
		return
	}

	hash, err := HashPassword(password)
	if err != nil {
		h.tmpl.ExecuteTemplate(w, "setup.html", map[string]interface{}{
			"Error": "Failed to create account",
		})
		return
	}

	h.cfg.Lock()
	h.cfg.AddWebUser(config.WebUser{
		Username:     username,
		PasswordHash: hash,
		Role:         config.RoleAdmin,
	})
	h.cfg.UnlockAndSave(h.configPath)

	h.sessions.setUser(w, r, username, config.RoleAdmin)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handlers) handleLogout(w http.ResponseWriter, r *http.Request) {
	h.sessions.clear(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// --- Pages ---

func (h *Handlers) handleChainsPage(w http.ResponseWriter, r *http.Request) {
	data := h.getUserInfo(r)
	data["Page"] = "chains"
	data["Chains"] = h.chainMgr.GetAllChainInfo()
	data["Topics"] = h.cfg.Topics

	// Separate ping sources from PLC names
	pingNames := h.plcManager.ListPingNames()
	pingSet := make(map[string]bool, len(pingNames))
	for _, n := range pingNames {
		pingSet[n] = true
	}
	allNames := h.plcManager.ListPLCs()
	plcNames := make([]string, 0, len(allNames))
	for _, n := range allNames {
		if !pingSet[n] {
			plcNames = append(plcNames, n)
		}
	}
	data["PLCNames"] = plcNames
	data["PingNames"] = pingNames

	h.renderTemplate(w, "chains.html", data)
}

func (h *Handlers) handleSourcesRedirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

// EmailTarget represents an email recipient extracted from a chain action block.
type EmailTarget struct {
	Email string
	Chain string
}

// WebhookTarget represents a webhook URL extracted from a chain action block.
type WebhookTarget struct {
	URL    string
	Method string
	Chain  string
}

func (h *Handlers) getEmailTargets() []EmailTarget {
	var targets []EmailTarget
	for _, chain := range h.cfg.Chains {
		for _, block := range chain.Blocks {
			if block.Type != "action" || block.ActionType != "email" {
				continue
			}
			for _, addr := range block.To {
				targets = append(targets, EmailTarget{Email: addr, Chain: chain.Name})
			}
		}
	}
	return targets
}

func (h *Handlers) getWebhookTargets() []WebhookTarget {
	var targets []WebhookTarget
	for _, chain := range h.cfg.Chains {
		for _, block := range chain.Blocks {
			if block.Type != "action" || block.ActionType != "webhook" {
				continue
			}
			if block.URL == "" {
				continue
			}
			method := block.Method
			if method == "" {
				method = "POST"
			}
			targets = append(targets, WebhookTarget{URL: block.URL, Method: method, Chain: chain.Name})
		}
	}
	return targets
}

func (h *Handlers) handleSubscribersPage(w http.ResponseWriter, r *http.Request) {
	data := h.getUserInfo(r)
	data["Page"] = "subscribers"
	data["Subscribers"] = h.cfg.Subscribers
	data["WASubscribers"] = h.cfg.WASubscribers
	data["Topics"] = h.cfg.Topics
	data["EmailTargets"] = h.getEmailTargets()
	data["WebhookTargets"] = h.getWebhookTargets()
	h.renderTemplate(w, "subscribers.html", data)
}

func (h *Handlers) handleProvidersPage(w http.ResponseWriter, r *http.Request) {
	data := h.getUserInfo(r)
	data["Page"] = "providers"
	data["SMS"] = h.cfg.Providers.SMS
	data["Email"] = h.cfg.Providers.Email
	data["WhatsApp"] = h.cfg.Providers.WhatsApp
	data["WAConnected"] = h.waClient != nil && h.waClient.IsConnected()
	data["WAPaired"] = h.waClient != nil && h.waClient.IsPaired()
	data["ExternalURL"] = h.cfg.Web.ExternalURL
	data["DetectedIPs"] = detectLocalIPs()
	data["WebPort"] = h.cfg.Web.Port

	// Compute webhook URL using external URL if set, otherwise from request.
	data["WebhookURL"] = h.webhookURL(r)

	h.renderTemplate(w, "providers.html", data)
}

// webhookURL computes the external webhook callback URL.
// Prefers cfg.Web.ExternalURL if set, otherwise falls back to the request host.
func (h *Handlers) webhookURL(r *http.Request) string {
	if h.cfg.Web.ExternalURL != "" {
		return h.cfg.Web.ExternalURL + "/api/sms/incoming/smsgate"
	}
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	return scheme + "://" + r.Host + "/api/sms/incoming/smsgate"
}

// detectLocalIPs returns non-loopback IPv4 addresses on the machine.
func detectLocalIPs() []string {
	var ips []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return ips
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() || ip.To4() == nil {
				continue
			}
			ips = append(ips, ip.String())
		}
	}
	return ips
}


func (h *Handlers) handleHistoryPage(w http.ResponseWriter, r *http.Request) {
	data := h.getUserInfo(r)
	data["Page"] = "history"
	data["Entries"] = h.auditLog.Recent(100)
	h.renderTemplate(w, "history.html", data)
}

// --- htmx partials ---

func (h *Handlers) handleChainsPartial(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Chains": h.chainMgr.GetAllChainInfo(),
	}
	data["IsAdmin"] = h.getUserInfo(r)["IsAdmin"]
	h.renderTemplate(w, "chain_list.html", data)
}

func (h *Handlers) handleSourcesPartial(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Sources": h.getSourceData(),
	}
	data["IsAdmin"] = h.getUserInfo(r)["IsAdmin"]
	h.renderTemplate(w, "source_list.html", data)
}

func (h *Handlers) handleSubscribersPartial(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Subscribers": h.cfg.Subscribers,
		"Topics":      h.cfg.Topics,
	}
	data["IsAdmin"] = h.getUserInfo(r)["IsAdmin"]
	h.renderTemplate(w, "subscriber_table.html", data)
}

func (h *Handlers) handleWASubscribersPartial(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"WASubscribers": h.cfg.WASubscribers,
		"Topics":        h.cfg.Topics,
	}
	data["IsAdmin"] = h.getUserInfo(r)["IsAdmin"]
	h.renderTemplate(w, "wa_subscriber_table.html", data)
}

func (h *Handlers) handleHistoryPartial(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Entries": h.auditLog.Recent(100),
	}
	h.renderTemplate(w, "history_table.html", data)
}

// --- Settings Page ---

func (h *Handlers) handleSettingsPage(w http.ResponseWriter, r *http.Request) {
	data := h.getUserInfo(r)
	data["Page"] = "settings"
	data["Sources"] = h.getSourceData()
	type userInfo struct {
		Username string
		Role     string
	}
	users := make([]userInfo, len(h.cfg.Web.Users))
	for i, u := range h.cfg.Web.Users {
		users[i] = userInfo{Username: u.Username, Role: u.Role}
	}
	data["Users"] = users
	h.renderTemplate(w, "settings.html", data)
}

func (h *Handlers) handleUsersPartial(w http.ResponseWriter, r *http.Request) {
	type userInfo struct {
		Username string
		Role     string
	}
	users := make([]userInfo, len(h.cfg.Web.Users))
	for i, u := range h.cfg.Web.Users {
		users[i] = userInfo{Username: u.Username, Role: u.Role}
	}
	data := map[string]interface{}{
		"Users": users,
	}
	data["IsAdmin"] = h.getUserInfo(r)["IsAdmin"]
	h.renderTemplate(w, "user_table.html", data)
}

// --- Data helpers ---

// SourceData holds display data for a source entry.
type SourceData struct {
	Name      string
	Type      string
	Enabled   bool
	URL       string
	Address   string
	Family    string
	Slot      int
	Host      string
	Port      int
	Status    string
	PLCCount  int
	TagCount  int
	Error     string
	Connected bool
}

func (h *Handlers) getSourceData() []SourceData {
	var sources []SourceData
	for _, src := range h.cfg.Sources {
		sd := SourceData{
			Name:    src.Name,
			Type:    src.Type,
			Enabled: src.Enabled,
			URL:     src.URL,
			Address: src.Address,
			Family:  string(src.Family),
			Slot:    int(src.Slot),
			Host:    src.Host,
			Port:    src.Port,
		}

		switch src.Type {
		case "warlink":
			poller := h.plcManager.GetWarLinkPoller(src.Name)
			if poller != nil {
				sd.Connected = poller.IsConnected()
				plcNames := poller.PLCNames()
				sd.PLCCount = len(plcNames)
				if err := poller.LastError(); err != nil {
					sd.Error = err.Error()
				}
				if sd.Connected {
					sd.Status = "Connected"
				} else if sd.Error != "" {
					sd.Status = "Error"
				} else {
					sd.Status = "Disconnected"
				}
				// Count total tags across discovered PLCs
				for _, pn := range plcNames {
					mp := h.plcManager.GetPLC(pn)
					if mp != nil {
						mp.Mu.RLock()
						sd.TagCount += len(mp.Values)
						mp.Mu.RUnlock()
					}
				}
			} else {
				sd.Status = "Disconnected"
			}

		case "plc":
			sd.PLCCount = 1
			mp := h.plcManager.GetPLC(src.Name)
			if mp != nil {
				mp.Mu.RLock()
				sd.Status = mp.Status.String()
				sd.TagCount = len(mp.Values)
				sd.Connected = sd.Status == "Connected"
				if mp.LastError != nil {
					sd.Error = mp.LastError.Error()
				}
				mp.Mu.RUnlock()
			} else {
				sd.Status = "Disconnected"
			}

		case "ping":
			sd.PLCCount = 0
			sd.TagCount = 1
			poller := h.plcManager.GetPingPoller(src.Name)
			if poller != nil {
				sd.Connected = poller.IsConnected()
				if err := poller.LastError(); err != nil {
					sd.Error = err.Error()
				}
				if sd.Connected {
					sd.Status = "Connected"
				} else if sd.Error != "" {
					sd.Status = "Error"
				} else {
					sd.Status = "Disconnected"
				}
			} else {
				sd.Status = "Disconnected"
			}
		}

		sources = append(sources, sd)
	}
	return sources
}
