package www

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"waralert/config"
)

// --- Chain API ---

func (h *Handlers) handleChainCreate(w http.ResponseWriter, r *http.Request) {
	var cfg config.ChainConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if cfg.Name == "" {
		http.Error(w, "Chain name is required", http.StatusBadRequest)
		return
	}

	h.cfg.Lock()
	if h.cfg.FindChain(cfg.Name) != nil {
		h.cfg.Unlock()
		http.Error(w, "Chain already exists", http.StatusConflict)
		return
	}
	h.cfg.AddChain(cfg)
	h.cfg.UnlockAndSave(h.configPath)

	if err := h.chainMgr.AddChain(cfg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if cfg.Enabled {
		h.chainMgr.StartChain(cfg.Name)
	}

	h.handleChainsPartial(w, r)
}

func (h *Handlers) handleChainGet(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	c := h.chainMgr.GetChain(name)
	if c == nil {
		http.Error(w, "Chain not found", http.StatusNotFound)
		return
	}
	h.chainMgr.ResetEditTimers()
	cfg := c.GetConfig()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cfg)
}

func (h *Handlers) handleChainUpdate(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var cfg config.ChainConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	h.chainMgr.RemoveChain(name)

	h.cfg.Lock()
	h.cfg.UpdateChain(name, cfg)
	h.cfg.UnlockAndSave(h.configPath)

	h.chainMgr.AddChain(cfg)
	if cfg.Enabled {
		h.chainMgr.StartChain(cfg.Name)
	}

	h.handleChainsPartial(w, r)
}

func (h *Handlers) handleChainDelete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	h.chainMgr.RemoveChain(name)

	h.cfg.Lock()
	h.cfg.RemoveChain(name)
	h.cfg.UnlockAndSave(h.configPath)

	h.handleChainsPartial(w, r)
}

func (h *Handlers) handleChainStart(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.chainMgr.StartChain(name); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	h.handleChainsPartial(w, r)
}

func (h *Handlers) handleChainStop(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.chainMgr.StopChain(name); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	h.handleChainsPartial(w, r)
}

func (h *Handlers) handleChainTestFire(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	c := h.chainMgr.GetChain(name)
	if c == nil {
		http.Error(w, "Chain not found", http.StatusNotFound)
		return
	}
	result, err := c.TestFire()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (h *Handlers) handleChainGates(w http.ResponseWriter, r *http.Request) {
	var blocks []config.BlockConfig
	if err := json.NewDecoder(r.Body).Decode(&blocks); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	results := h.chainMgr.EvaluateBlocks(blocks)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// --- Source API ---

func (h *Handlers) handleSourceCreate(w http.ResponseWriter, r *http.Request) {
	var src config.SourceConfig
	if err := json.NewDecoder(r.Body).Decode(&src); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if src.Name == "" {
		http.Error(w, "Source name is required", http.StatusBadRequest)
		return
	}
	if src.Type != "warlink" && src.Type != "plc" && src.Type != "ping" {
		http.Error(w, "Source type must be 'warlink', 'plc', or 'ping'", http.StatusBadRequest)
		return
	}

	h.cfg.Lock()
	if h.cfg.FindSource(src.Name) != nil {
		h.cfg.Unlock()
		http.Error(w, "Source already exists", http.StatusConflict)
		return
	}
	h.cfg.AddSource(src)
	h.cfg.UnlockAndSave(h.configPath)

	if err := h.plcManager.AddSource(src); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.handleSourcesPartial(w, r)
}

func (h *Handlers) handleSourceGet(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	src := h.cfg.FindSource(name)
	if src == nil {
		http.Error(w, "Source not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(src)
}

func (h *Handlers) handleSourceUpdate(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var updated config.SourceConfig
	if err := json.NewDecoder(r.Body).Decode(&updated); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	h.cfg.Lock()
	existing := h.cfg.FindSource(name)
	if existing == nil {
		h.cfg.Unlock()
		http.Error(w, "Source not found", http.StatusNotFound)
		return
	}
	oldType := existing.Type
	h.cfg.UpdateSource(name, updated)
	h.cfg.UnlockAndSave(h.configPath)

	// Remove old source from manager and add updated
	h.plcManager.RemoveSource(name, oldType)
	h.plcManager.AddSource(updated)

	h.handleSourcesPartial(w, r)
}

func (h *Handlers) handleSourceDelete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	h.cfg.Lock()
	src := h.cfg.FindSource(name)
	if src == nil {
		h.cfg.Unlock()
		http.Error(w, "Source not found", http.StatusNotFound)
		return
	}
	srcType := src.Type
	h.cfg.RemoveSource(name)
	h.cfg.UnlockAndSave(h.configPath)

	h.plcManager.RemoveSource(name, srcType)

	h.handleSourcesPartial(w, r)
}

func (h *Handlers) handlePLCConnect(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.plcManager.Connect(name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.handleSourcesPartial(w, r)
}

func (h *Handlers) handlePLCDisconnect(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.plcManager.Disconnect(name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.handleSourcesPartial(w, r)
}

func (h *Handlers) handlePLCTags(w http.ResponseWriter, r *http.Request) {
	plcName := chi.URLParam(r, "plc")
	mp := h.plcManager.GetPLC(plcName)
	if mp == nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
		return
	}

	mp.Mu.RLock()
	tags := make([]map[string]string, 0, len(mp.Values))
	for name, tv := range mp.Values {
		tags = append(tags, map[string]string{
			"name": name,
			"type": tv.TypeName(),
		})
	}
	mp.Mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tags)
}

// --- Subscriber API ---

func (h *Handlers) handleSubscriberCreate(w http.ResponseWriter, r *http.Request) {
	var sub config.SubscriberConfig
	if err := json.NewDecoder(r.Body).Decode(&sub); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if sub.Phone == "" {
		http.Error(w, "Phone number is required", http.StatusBadRequest)
		return
	}

	h.cfg.Lock()
	if h.cfg.FindSubscriber(sub.Phone) != nil {
		h.cfg.Unlock()
		http.Error(w, "Subscriber already exists", http.StatusConflict)
		return
	}
	sub.Active = true
	h.cfg.AddSubscriber(sub)
	h.cfg.UnlockAndSave(h.configPath)

	h.handleSubscribersPartial(w, r)
}

func (h *Handlers) handleSubscriberDelete(w http.ResponseWriter, r *http.Request) {
	phone, _ := url.PathUnescape(chi.URLParam(r, "phone"))

	h.cfg.Lock()
	h.cfg.RemoveSubscriber(phone)
	h.cfg.UnlockAndSave(h.configPath)

	h.handleSubscribersPartial(w, r)
}

func (h *Handlers) handleSubscriberToggle(w http.ResponseWriter, r *http.Request) {
	phone, _ := url.PathUnescape(chi.URLParam(r, "phone"))

	h.cfg.Lock()
	sub := h.cfg.FindSubscriber(phone)
	if sub != nil {
		sub.Active = !sub.Active
	}
	h.cfg.UnlockAndSave(h.configPath)

	h.handleSubscribersPartial(w, r)
}

// --- Provider API ---

func (h *Handlers) handleSMSProviderUpdate(w http.ResponseWriter, r *http.Request) {
	var smsCfg config.SMSProviderConfig
	if err := json.NewDecoder(r.Body).Decode(&smsCfg); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	h.cfg.Lock()
	h.cfg.Providers.SMS = smsCfg
	h.cfg.UnlockAndSave(h.configPath)

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "SMS provider updated")
}

func (h *Handlers) handleEmailProviderUpdate(w http.ResponseWriter, r *http.Request) {
	var emailCfg config.EmailProviderConfig
	if err := json.NewDecoder(r.Body).Decode(&emailCfg); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	h.cfg.Lock()
	h.cfg.Providers.Email = emailCfg
	h.cfg.UnlockAndSave(h.configPath)

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "Email provider updated")
}

func (h *Handlers) handleSMSTestConnection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode     string `json:"mode"`
		BaseURL  string `json:"base_url"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if req.BaseURL == "" {
		http.Error(w, "Base URL is required", http.StatusBadRequest)
		return
	}

	if err := h.actionReg.TestSMSConnection(req.Mode, req.BaseURL, req.Username, req.Password); err != nil {
		http.Error(w, "Connection failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	fmt.Fprint(w, "Connection successful")
}

func (h *Handlers) handleSMSTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phone   string `json:"phone"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Phone == "" {
		http.Error(w, "Phone number is required", http.StatusBadRequest)
		return
	}
	if req.Message == "" {
		req.Message = "WarAlert test message"
	}

	if err := h.actionReg.TestSMS(req.Phone, req.Message); err != nil {
		http.Error(w, "SMS send failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	fmt.Fprint(w, "Test SMS sent")
}

func (h *Handlers) handleEmailTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		To      []string `json:"to"`
		Subject string   `json:"subject"`
		Body    string   `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if len(req.To) == 0 {
		http.Error(w, "At least one recipient is required", http.StatusBadRequest)
		return
	}
	if req.Subject == "" {
		req.Subject = "WarAlert Test Email"
	}
	if req.Body == "" {
		req.Body = "This is a test email from the WarAlert alert system."
	}

	if err := h.actionReg.TestEmail(req.To, req.Subject, req.Body); err != nil {
		http.Error(w, "Email send failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	fmt.Fprint(w, "Test email sent")
}

// --- Topic API ---

func (h *Handlers) handleTopicCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Topic string `json:"topic"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	topic := strings.TrimSpace(strings.ToUpper(req.Topic))
	if topic == "" {
		http.Error(w, "Topic name is required", http.StatusBadRequest)
		return
	}

	h.cfg.Lock()
	if !h.cfg.AddTopic(topic) {
		h.cfg.Unlock()
		http.Error(w, "Topic already exists", http.StatusConflict)
		return
	}
	h.cfg.UnlockAndSave(h.configPath)

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "Topic created")
}

func (h *Handlers) handleTopicDelete(w http.ResponseWriter, r *http.Request) {
	topic, _ := url.PathUnescape(chi.URLParam(r, "topic"))

	h.cfg.Lock()
	h.cfg.RemoveTopic(topic)
	h.cfg.UnlockAndSave(h.configPath)

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "Topic deleted")
}

// --- User API ---

func (h *Handlers) handleUserCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Username == "" || req.Password == "" {
		http.Error(w, "Username and password are required", http.StatusBadRequest)
		return
	}
	if req.Role == "" {
		req.Role = config.RoleViewer
	}

	hash, err := HashPassword(req.Password)
	if err != nil {
		http.Error(w, "Failed to hash password", http.StatusInternalServerError)
		return
	}

	h.cfg.Lock()
	if h.cfg.FindWebUser(req.Username) != nil {
		h.cfg.Unlock()
		http.Error(w, "User already exists", http.StatusConflict)
		return
	}
	h.cfg.AddWebUser(config.WebUser{
		Username:     req.Username,
		PasswordHash: hash,
		Role:         req.Role,
	})
	h.cfg.UnlockAndSave(h.configPath)

	h.handleUsersPartial(w, r)
}

func (h *Handlers) handleUserUpdate(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	var req struct {
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	h.cfg.Lock()
	user := h.cfg.FindWebUser(username)
	if user == nil {
		h.cfg.Unlock()
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	if req.Role != "" {
		user.Role = req.Role
	}
	if req.Password != "" {
		hash, err := HashPassword(req.Password)
		if err != nil {
			h.cfg.Unlock()
			http.Error(w, "Failed to hash password", http.StatusInternalServerError)
			return
		}
		user.PasswordHash = hash
	}
	h.cfg.UnlockAndSave(h.configPath)

	h.handleUsersPartial(w, r)
}

func (h *Handlers) handleUserDelete(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")

	currentUser, _, _ := h.sessions.getUser(r)
	if username == currentUser {
		http.Error(w, "Cannot delete your own account", http.StatusBadRequest)
		return
	}

	h.cfg.Lock()
	h.cfg.RemoveWebUser(username)
	h.cfg.UnlockAndSave(h.configPath)

	h.handleUsersPartial(w, r)
}

// --- PLC Tag API ---

func (h *Handlers) handlePLCTagAdd(w http.ResponseWriter, r *http.Request) {
	plcName := chi.URLParam(r, "name")
	var tag config.TagSelection
	if err := json.NewDecoder(r.Body).Decode(&tag); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if tag.Name == "" {
		http.Error(w, "Tag name is required", http.StatusBadRequest)
		return
	}

	h.cfg.Lock()
	if !h.cfg.AddPLCTag(plcName, tag) {
		h.cfg.Unlock()
		http.Error(w, "Tag already exists or PLC not found", http.StatusConflict)
		return
	}
	h.cfg.UnlockAndSave(h.configPath)

	// Return updated tag list
	h.handlePLCTagList(w, r)
}

func (h *Handlers) handlePLCTagRemove(w http.ResponseWriter, r *http.Request) {
	plcName := chi.URLParam(r, "name")
	tagName, _ := url.PathUnescape(chi.URLParam(r, "tag"))

	h.cfg.Lock()
	h.cfg.RemovePLCTag(plcName, tagName)
	h.cfg.UnlockAndSave(h.configPath)

	h.handlePLCTagList(w, r)
}

func (h *Handlers) handlePLCTagList(w http.ResponseWriter, r *http.Request) {
	plcName := chi.URLParam(r, "name")
	src := h.cfg.FindSource(plcName)
	tags := []config.TagSelection{}
	if src != nil && src.Type == "plc" {
		tags = src.Tags
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tags)
}

// --- SMS Webhook Registration ---

func (h *Handlers) handleSMSRegisterWebhook(w http.ResponseWriter, r *http.Request) {
	// Compute webhook URL from request.
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	webhookURL := scheme + "://" + r.Host + "/api/sms/incoming/smsgate"

	if err := h.actionReg.RegisterSMSWebhook(webhookURL); err != nil {
		http.Error(w, "Register webhook failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	fmt.Fprint(w, "Webhook registered")
}

func (h *Handlers) handleSMSWebhookStatus(w http.ResponseWriter, r *http.Request) {
	smsCfg := h.cfg.Providers.SMS
	if smsCfg.Type == "twilio" || smsCfg.BaseURL == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"registered": false, "reason": "not applicable"})
		return
	}

	// Compute our expected webhook URL.
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	ourURL := scheme + "://" + r.Host + "/api/sms/incoming/smsgate"

	webhooks, err := h.actionReg.GetSMSWebhooks()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"registered": false, "error": err.Error()})
		return
	}

	registered := false
	for _, wh := range webhooks {
		if u, ok := wh["url"].(string); ok && u == ourURL {
			registered = true
			break
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"registered": registered})
}

// --- Subscriber Update API ---

func (h *Handlers) handleSubscriberUpdate(w http.ResponseWriter, r *http.Request) {
	phone, _ := url.PathUnescape(chi.URLParam(r, "phone"))
	var req struct {
		Name   string   `json:"name"`
		Topics []string `json:"topics"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	h.cfg.Lock()
	sub := h.cfg.FindSubscriber(phone)
	if sub == nil {
		h.cfg.Unlock()
		http.Error(w, "Subscriber not found", http.StatusNotFound)
		return
	}
	sub.Name = req.Name
	sub.Topics = req.Topics
	h.cfg.UnlockAndSave(h.configPath)

	h.handleSubscribersPartial(w, r)
}

