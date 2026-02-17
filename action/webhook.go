package action

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"text/template"
	"time"
)

// WebhookProvider sends HTTP webhook requests.
type WebhookProvider struct {
	httpClient *http.Client
	logFn      func(string, ...interface{})
}

// NewWebhookProvider creates a new webhook provider.
func NewWebhookProvider(logFn func(string, ...interface{})) *WebhookProvider {
	return &WebhookProvider{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		logFn:      logFn,
	}
}

// Type returns the provider type identifier.
func (p *WebhookProvider) Type() string { return "webhook" }

// Execute sends a webhook HTTP request for an action block.
func (p *WebhookProvider) Execute(params ActionParams) error {
	block := params.Block

	if block.URL == "" {
		return fmt.Errorf("webhook: no URL configured")
	}

	// Resolve body template
	body, err := resolveBody(block.Body, params.TagReader)
	if err != nil {
		return fmt.Errorf("webhook: resolve body: %w", err)
	}

	// Build HTTP request
	method := block.Method
	if method == "" {
		method = "POST"
	}

	var bodyReader io.Reader
	if body != "" {
		bodyReader = bytes.NewBufferString(body)
	}

	req, err := http.NewRequest(method, block.URL, bodyReader)
	if err != nil {
		return fmt.Errorf("webhook: build request: %w", err)
	}

	// Set Content-Type
	ct := block.ContentType
	if ct == "" {
		ct = "application/json"
	}
	if body != "" {
		req.Header.Set("Content-Type", ct)
	}

	// Set custom headers
	for k, v := range block.Headers {
		req.Header.Set(k, v)
	}

	// Apply auth
	switch block.Auth.Type {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+block.Auth.Token)
	case "basic":
		req.SetBasicAuth(block.Auth.Username, block.Auth.Password)
	case "custom_header":
		if block.Auth.HeaderName != "" {
			req.Header.Set(block.Auth.HeaderName, block.Auth.HeaderValue)
		}
	}

	// Use action timeout or default
	client := p.httpClient
	if block.Timeout > 0 {
		client = &http.Client{Timeout: block.Timeout}
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook: HTTP request failed: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook: %s %s returned status %d", method, block.URL, resp.StatusCode)
	}

	name := block.Name
	if name == "" {
		name = block.URL
	}
	p.logFn("webhook: sent %s to %s, status=%d", method, name, resp.StatusCode)
	return nil
}

// resolveBody replaces {{.Tag "plc" "tag"}} references in the body template
// with live values from the TagReader.
func resolveBody(bodyTemplate string, tagReader TagReader) (string, error) {
	if bodyTemplate == "" {
		return "", nil
	}

	if tagReader == nil {
		return bodyTemplate, nil
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

	tmpl, err := template.New("body").Funcs(funcMap).Parse(bodyTemplate)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, nil); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	return buf.String(), nil
}
