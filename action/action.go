package action

import (
	"fmt"

	"waralert/config"
)

// TagReader is an alias for config.TagReader.
type TagReader = config.TagReader

// Provider executes an action (SMS, email, webhook).
type Provider interface {
	Execute(params ActionParams) error
	Type() string
}

// ActionParams holds parameters for action execution.
type ActionParams struct {
	Block     config.BlockConfig
	TagReader config.TagReader
}

// Registry holds all action providers.
type Registry struct {
	sms     *SMSProvider
	email   *EmailProvider
	webhook *WebhookProvider
	cfg     *config.Config
	logFn   func(string, ...interface{})
}

// NewRegistry creates a new action provider registry.
func NewRegistry(cfg *config.Config) *Registry {
	logFn := func(format string, args ...interface{}) {}

	r := &Registry{
		cfg:   cfg,
		logFn: logFn,
	}

	r.sms = NewSMSProvider(cfg, logFn)
	r.email = NewEmailProvider(cfg, logFn)
	r.webhook = NewWebhookProvider(logFn)

	return r
}

// SetLogFunc sets the logging function on the registry and all providers.
func (r *Registry) SetLogFunc(fn func(string, ...interface{})) {
	r.logFn = fn
	r.sms.logFn = fn
	r.email.logFn = fn
	r.webhook.logFn = fn
}

// Execute dispatches the action block to the appropriate provider.
func (r *Registry) Execute(block config.BlockConfig, tagReader config.TagReader) error {
	params := ActionParams{
		Block:     block,
		TagReader: tagReader,
	}

	switch block.ActionType {
	case "sms":
		return r.sms.Execute(params)
	case "email":
		return r.email.Execute(params)
	case "webhook":
		return r.webhook.Execute(params)
	default:
		return fmt.Errorf("unknown action type: %s", block.ActionType)
	}
}

// TestSMS sends a test SMS to the given phone number.
func (r *Registry) TestSMS(phone, message string) error {
	return r.sms.SendSMS(phone, message)
}

// TestSMSConnection tests connectivity to the SMS-gate server using the provided settings.
func (r *Registry) TestSMSConnection(mode, baseURL, username, password string) error {
	return r.sms.TestConnection(mode, baseURL, username, password)
}

// TestEmail sends a test email.
func (r *Registry) TestEmail(to []string, subject, body string) error {
	return r.email.SendEmail(to, subject, body)
}

// RegisterSMSWebhook registers a webhook URL with the SMS-gate server.
func (r *Registry) RegisterSMSWebhook(webhookURL string) error {
	smsCfg := r.cfg.Providers.SMS
	return r.sms.RegisterWebhook(smsCfg.BaseURL, smsCfg.Username, smsCfg.Password, webhookURL)
}

// GetSMSWebhooks fetches the list of registered webhooks from the SMS-gate server.
func (r *Registry) GetSMSWebhooks() ([]map[string]interface{}, error) {
	smsCfg := r.cfg.Providers.SMS
	return r.sms.GetWebhooks(smsCfg.BaseURL, smsCfg.Username, smsCfg.Password)
}
