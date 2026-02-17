package action

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"text/template"
	"time"

	"waralert/config"
)

// ensureScheme prepends http:// if the URL has no scheme.
func ensureScheme(rawURL string) string {
	if rawURL == "" {
		return rawURL
	}
	if !strings.Contains(rawURL, "://") {
		return "http://" + rawURL
	}
	return rawURL
}

// SMSProvider sends SMS messages via SMS-gate or Twilio.
type SMSProvider struct {
	cfg         *config.Config
	httpClient  *http.Client
	jwtToken    string
	jwtExpiry   time.Time
	jwtMu       sync.Mutex
	rateLimiter *RateLimiter
	logFn       func(string, ...interface{})
}

// NewSMSProvider creates a new SMS provider.
func NewSMSProvider(cfg *config.Config, logFn func(string, ...interface{})) *SMSProvider {
	rate := cfg.Providers.SMS.GlobalRatePerMin
	if rate <= 0 {
		rate = 30
	}
	return &SMSProvider{
		cfg:         cfg,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		rateLimiter: NewRateLimiter(rate),
		logFn:       logFn,
	}
}

// Type returns the provider type identifier.
func (p *SMSProvider) Type() string { return "sms" }

// Execute sends SMS messages for an action block.
func (p *SMSProvider) Execute(params ActionParams) error {
	block := params.Block
	if !p.cfg.Providers.SMS.Enabled {
		return fmt.Errorf("SMS provider is not enabled")
	}

	// Resolve message template
	message, err := resolveMessage(block.Message, params.TagReader)
	if err != nil {
		return fmt.Errorf("resolve message template: %w", err)
	}

	// Resolve topic -> subscriber phone numbers
	phones := p.cfg.SubscribersForTopic(block.Topic)
	if len(phones) == 0 {
		p.logFn("sms: no subscribers for topic %q", block.Topic)
		return nil
	}

	var lastErr error
	for _, phone := range phones {
		if err := p.SendSMS(phone, message); err != nil {
			p.logFn("sms: failed to send to %s: %v", phone, err)
			lastErr = err
		}
	}
	return lastErr
}

// SendSMS sends an SMS to a single phone number.
func (p *SMSProvider) SendSMS(phone, message string) error {
	if !p.rateLimiter.Allow() {
		return fmt.Errorf("SMS rate limit exceeded")
	}

	switch p.cfg.Providers.SMS.Type {
	case "twilio":
		return p.sendTwilio(phone, message)
	case "smsgate", "":
		return p.sendSMSGate(phone, message)
	default:
		return fmt.Errorf("unknown SMS provider type: %s", p.cfg.Providers.SMS.Type)
	}
}

// sendSMSGate sends an SMS via the SMS-gate API.
// Supports both local mode (basic auth, /message) and cloud/private mode (JWT, /3rdparty/v1/messages).
func (p *SMSProvider) sendSMSGate(phone, message string) error {
	smsCfg := p.cfg.Providers.SMS
	baseURL := ensureScheme(strings.TrimRight(smsCfg.BaseURL, "/"))

	payload := map[string]interface{}{
		"textMessage":  map[string]string{"text": message},
		"phoneNumbers": []string{phone},
	}
	body, _ := json.Marshal(payload)

	if smsCfg.Mode == "local" {
		// Local mode: basic auth, POST /message
		req, err := http.NewRequest("POST", baseURL+"/message", bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.SetBasicAuth(smsCfg.Username, smsCfg.Password)

		resp, err := p.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("smsgate local request: %w", err)
		}
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)

		if resp.StatusCode >= 300 {
			return fmt.Errorf("smsgate local returned status %d: %s", resp.StatusCode, string(respBody))
		}

		p.logFn("sms: sent to %s via smsgate (local)", phone)
		return nil
	}

	// Cloud/private mode: JWT auth, POST /3rdparty/v1/messages
	token, err := p.getSMSGateToken()
	if err != nil {
		return fmt.Errorf("smsgate auth: %w", err)
	}

	req, err := http.NewRequest("POST", baseURL+"/3rdparty/v1/messages", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("smsgate request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 300 {
		return fmt.Errorf("smsgate returned status %d: %s", resp.StatusCode, string(respBody))
	}

	p.logFn("sms: sent to %s via smsgate (cloud)", phone)
	return nil
}

// getSMSGateToken returns a cached JWT or authenticates to get a new one.
func (p *SMSProvider) getSMSGateToken() (string, error) {
	p.jwtMu.Lock()
	defer p.jwtMu.Unlock()

	// Return cached token if still valid (5 minute buffer)
	if p.jwtToken != "" && time.Now().Add(5*time.Minute).Before(p.jwtExpiry) {
		return p.jwtToken, nil
	}

	baseURL := ensureScheme(strings.TrimRight(p.cfg.Providers.SMS.BaseURL, "/"))
	req, err := http.NewRequest("POST", baseURL+"/3rdparty/v1/auth/token", nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(p.cfg.Providers.SMS.Username, p.cfg.Providers.SMS.Password)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		io.Copy(io.Discard, resp.Body)
		return "", fmt.Errorf("auth failed with status %d", resp.StatusCode)
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode auth response: %w", err)
	}

	p.jwtToken = result.Token
	p.jwtExpiry = parseJWTExpiry(result.Token)

	return p.jwtToken, nil
}

// parseJWTExpiry extracts the exp claim from a JWT payload without verification.
func parseJWTExpiry(token string) time.Time {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) < 2 {
		return time.Now().Add(30 * time.Minute)
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Now().Add(30 * time.Minute)
	}

	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == 0 {
		return time.Now().Add(30 * time.Minute)
	}

	return time.Unix(claims.Exp, 0)
}

// sendTwilio sends an SMS via the Twilio API.
func (p *SMSProvider) sendTwilio(phone, message string) error {
	smsCfg := p.cfg.Providers.SMS
	endpoint := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", smsCfg.AccountSID)

	form := url.Values{}
	form.Set("To", phone)
	form.Set("From", smsCfg.FromNumber)
	form.Set("Body", message)

	req, err := http.NewRequest("POST", endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(smsCfg.AccountSID, smsCfg.AuthToken)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("twilio request: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 300 {
		return fmt.Errorf("twilio returned status %d", resp.StatusCode)
	}

	p.logFn("sms: sent to %s via twilio", phone)
	return nil
}

// TestConnection checks connectivity to the SMS-gate server via its health endpoint.
// Parameters are taken from the form directly so the user can test before saving.
func (p *SMSProvider) TestConnection(mode, rawBaseURL, username, password string) error {
	baseURL := ensureScheme(strings.TrimRight(rawBaseURL, "/"))
	if baseURL == "" {
		return fmt.Errorf("base URL is required")
	}

	var healthURL string
	if mode == "local" {
		healthURL = baseURL + "/health"
	} else {
		healthURL = baseURL + "/3rdparty/v1/health"
	}

	req, err := http.NewRequest("GET", healthURL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if username != "" {
		req.SetBasicAuth(username, password)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("health check returned status %d: %s", resp.StatusCode, string(body))
	}

	var health struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &health); err != nil {
		return fmt.Errorf("invalid health response: %w", err)
	}

	if health.Status != "pass" {
		return fmt.Errorf("server health status: %s", health.Status)
	}

	return nil
}

// RegisterWebhook registers a webhook callback URL with the SMS-gate server.
// For local mode, it calls POST /webhooks; for cloud mode, POST /3rdparty/v1/webhooks.
func (p *SMSProvider) RegisterWebhook(baseURL, username, password, webhookURL string) error {
	base := ensureScheme(strings.TrimRight(baseURL, "/"))
	if base == "" {
		return fmt.Errorf("base URL is required")
	}

	payload := map[string]interface{}{
		"url":   webhookURL,
		"event": "sms:received",
	}
	body, _ := json.Marshal(payload)

	smsCfg := p.cfg.Providers.SMS
	var endpoint string
	if smsCfg.Mode == "local" {
		endpoint = base + "/webhooks"
	} else {
		endpoint = base + "/3rdparty/v1/webhooks"
	}

	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if smsCfg.Mode == "local" {
		req.SetBasicAuth(username, password)
	} else {
		token, err := p.getSMSGateToken()
		if err != nil {
			return fmt.Errorf("auth: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 300 {
		return fmt.Errorf("register webhook returned status %d: %s", resp.StatusCode, string(respBody))
	}

	p.logFn("sms: webhook registered at %s", webhookURL)
	return nil
}

// GetWebhooks fetches the list of registered webhooks from the SMS-gate server.
func (p *SMSProvider) GetWebhooks(baseURL, username, password string) ([]map[string]interface{}, error) {
	base := ensureScheme(strings.TrimRight(baseURL, "/"))
	if base == "" {
		return nil, fmt.Errorf("base URL is required")
	}

	smsCfg := p.cfg.Providers.SMS
	var endpoint string
	if smsCfg.Mode == "local" {
		endpoint = base + "/webhooks"
	} else {
		endpoint = base + "/3rdparty/v1/webhooks"
	}

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	if smsCfg.Mode == "local" {
		req.SetBasicAuth(username, password)
	} else {
		token, err := p.getSMSGateToken()
		if err != nil {
			return nil, fmt.Errorf("auth: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("get webhooks returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var webhooks []map[string]interface{}
	if err := json.Unmarshal(respBody, &webhooks); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return webhooks, nil
}

// DeleteWebhook deletes a webhook by ID from the SMS-gate server.
func (p *SMSProvider) DeleteWebhook(baseURL, username, password, webhookID string) error {
	base := ensureScheme(strings.TrimRight(baseURL, "/"))
	if base == "" {
		return fmt.Errorf("base URL is required")
	}

	smsCfg := p.cfg.Providers.SMS
	var endpoint string
	if smsCfg.Mode == "local" {
		endpoint = base + "/webhooks/" + webhookID
	} else {
		endpoint = base + "/3rdparty/v1/webhooks/" + webhookID
	}

	req, err := http.NewRequest("DELETE", endpoint, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	if smsCfg.Mode == "local" {
		req.SetBasicAuth(username, password)
	} else {
		token, err := p.getSMSGateToken()
		if err != nil {
			return fmt.Errorf("auth: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete webhook returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// resolveMessage parses and executes a text/template with tag references.
// Templates use {{.Tag "plcName" "tagName"}} to reference PLC tag values.
func resolveMessage(msgTemplate string, tagReader TagReader) (string, error) {
	if tagReader == nil {
		return msgTemplate, nil
	}

	funcMap := template.FuncMap{
		"Tag": func(plc, tag string) string {
			val, err := tagReader.ReadTagValue(plc, tag)
			if err != nil {
				return fmt.Sprintf("<err:%s.%s>", plc, tag)
			}
			return fmt.Sprintf("%v", val)
		},
	}

	tmpl, err := template.New("msg").Funcs(funcMap).Parse(msgTemplate)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, nil); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	return buf.String(), nil
}
