package sms

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"waralert/audit"
	"waralert/config"
)

// Handler provides HTTP handlers for incoming SMS webhooks.
type Handler struct {
	mgr      *Manager
	sender   MessageSender
	cfg      *config.Config
	logFn    func(string, ...interface{})
	auditLog *audit.Logger
}

// NewHandler creates a new webhook handler.
func NewHandler(mgr *Manager, sender MessageSender, cfg *config.Config, logFn func(string, ...interface{}), auditLog *audit.Logger) *Handler {
	if logFn == nil {
		logFn = func(string, ...interface{}) {}
	}
	return &Handler{
		mgr:      mgr,
		sender:   sender,
		cfg:      cfg,
		logFn:    logFn,
		auditLog: auditLog,
	}
}

// --- SMS-gate webhook ---

// smsGatePayload is the expected JSON body from an SMS-gate webhook.
type smsGatePayload struct {
	DeviceID  string         `json:"deviceId"`
	Event     string         `json:"event"`
	ID        string         `json:"id"`
	WebhookID string         `json:"webhookId"`
	Payload   smsGateMessage `json:"payload"`
}

// smsGateMessage is the nested message payload within an SMS-gate webhook.
type smsGateMessage struct {
	MessageID   string `json:"messageId"`
	Message     string `json:"message"`
	PhoneNumber string `json:"phoneNumber"`
	SimNumber   int    `json:"simNumber"`
	ReceivedAt  string `json:"receivedAt"`
}

// HandleSMSGateIncoming handles POST webhooks from an SMS-gate provider.
// It validates the HMAC-SHA256 signature, parses the command, executes it,
// and sends a reply via the configured SMSSender.
func (h *Handler) HandleSMSGateIncoming(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Validate HMAC signature.
	sig := r.Header.Get("X-Signature")
	ts := r.Header.Get("X-Timestamp")
	secret := h.cfg.Providers.SMS.WebhookSecret

	if secret != "" {
		// Try body+ts (SMS-gate documented order) then ts+body for robustness.
		valid := false
		for _, order := range [][2][]byte{{body, []byte(ts)}, {[]byte(ts), body}} {
			mac := hmac.New(sha256.New, []byte(secret))
			mac.Write(order[0])
			mac.Write(order[1])
			expected := fmt.Sprintf("%x", mac.Sum(nil))
			if hmac.Equal([]byte(sig), []byte(expected)) {
				valid = true
				break
			}
		}
		if !valid {
			h.logFn("sms-gate: invalid HMAC signature from request")
			h.logAudit("", "sms_incoming", "", "", false, "invalid HMAC signature")
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
	}

	var payload smsGatePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		h.logAudit("", "sms_incoming", "", "", false, "invalid JSON: "+err.Error())
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if payload.Event != "sms:received" {
		// Acknowledge non-SMS events silently.
		w.WriteHeader(http.StatusOK)
		return
	}

	from := payload.Payload.PhoneNumber
	msgText := payload.Payload.Message
	h.logFn("sms-gate: incoming from=%s body=%q", from, msgText)

	cmd := ParseCommand(msgText)
	reply := ExecuteCommand(h.mgr, from, cmd)

	h.logFn("sms-gate: reply to=%s body=%q", from, reply)

	var sendErr string
	if h.sender != nil {
		if err := h.sender.SendMessage(from, reply); err != nil {
			h.logFn("sms-gate: send reply error: %v", err)
			sendErr = err.Error()
		}
	}

	h.logAudit(from, "sms_incoming", msgText, reply, sendErr == "", sendErr)
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) logAudit(from, eventType, command, reply string, success bool, errMsg string) {
	if h.auditLog == nil {
		return
	}
	h.auditLog.Log(audit.Entry{
		EventType: eventType,
		From:      from,
		Command:   command,
		Reply:     reply,
		Success:   success,
		Error:     errMsg,
	})
}

// --- Twilio webhook ---

// twiMLResponse is the TwiML XML envelope for replying to Twilio.
type twiMLResponse struct {
	XMLName xml.Name     `xml:"Response"`
	Message *twiMLMessage `xml:"Message,omitempty"`
}

// twiMLMessage holds a single TwiML message body.
type twiMLMessage struct {
	Body string `xml:",chardata"`
}

// HandleTwilioIncoming handles POST webhooks from Twilio.
// It validates the Twilio request signature, parses the command, executes it,
// and responds with a TwiML XML document.
func (h *Handler) HandleTwilioIncoming(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form body", http.StatusBadRequest)
		return
	}

	phone := r.FormValue("From")
	body := r.FormValue("Body")

	// Validate Twilio signature.
	authToken := h.cfg.Providers.SMS.AuthToken
	twilioSig := r.Header.Get("X-Twilio-Signature")

	if authToken != "" {
		if !validateTwilioSignature(authToken, twilioSig, requestURL(r), r.PostForm) {
			h.logFn("twilio: invalid signature from request")
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
	}

	h.logFn("twilio: incoming from=%s body=%q", phone, body)

	cmd := ParseCommand(body)
	reply := ExecuteCommand(h.mgr, phone, cmd)

	h.logFn("twilio: reply to=%s body=%q", phone, reply)

	resp := twiMLResponse{
		Message: &twiMLMessage{Body: reply},
	}

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, xml.Header)
	xml.NewEncoder(w).Encode(resp)
}

// requestURL reconstructs the full request URL that Twilio used when
// computing the signature. It uses the scheme from X-Forwarded-Proto when
// available (common behind reverse proxies) and falls back to http/https
// based on TLS state.
func requestURL(r *http.Request) string {
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	return scheme + "://" + r.Host + r.RequestURI
}

// validateTwilioSignature validates a Twilio webhook request signature.
// Twilio signs requests by computing HMAC-SHA1 of the request URL with all
// POST parameters sorted and appended as key+value pairs.
func validateTwilioSignature(authToken, signature, url string, params url.Values) bool {
	// Build the data string: URL + sorted params.
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf strings.Builder
	buf.WriteString(url)
	for _, k := range keys {
		buf.WriteString(k)
		buf.WriteString(params.Get(k))
	}

	mac := hmac.New(sha1.New, []byte(authToken))
	mac.Write([]byte(buf.String()))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(signature), []byte(expected))
}
