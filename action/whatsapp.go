package action

import (
	"context"
	"fmt"

	"waralert/config"
)

// WhatsAppSender sends a WhatsApp message to a phone number.
type WhatsAppSender interface {
	SendMessage(ctx context.Context, phone, message string) error
}

// WhatsAppProvider sends WhatsApp messages to subscribers.
type WhatsAppProvider struct {
	cfg         *config.Config
	sender      WhatsAppSender
	rateLimiter *RateLimiter
	logFn       func(string, ...interface{})
}

// NewWhatsAppProvider creates a new WhatsApp provider.
func NewWhatsAppProvider(cfg *config.Config, logFn func(string, ...interface{})) *WhatsAppProvider {
	rate := cfg.Providers.WhatsApp.GlobalRatePerMin
	if rate <= 0 {
		rate = 30
	}
	return &WhatsAppProvider{
		cfg:         cfg,
		rateLimiter: NewRateLimiter(rate),
		logFn:       logFn,
	}
}

// SetSender sets the WhatsApp sender (called after client init to avoid import cycles).
func (p *WhatsAppProvider) SetSender(sender WhatsAppSender) {
	p.sender = sender
}

// Type returns the provider type identifier.
func (p *WhatsAppProvider) Type() string { return "whatsapp" }

// Execute sends WhatsApp messages for an action block.
func (p *WhatsAppProvider) Execute(params ActionParams) error {
	block := params.Block
	if !p.cfg.Providers.WhatsApp.Enabled {
		return fmt.Errorf("WhatsApp provider is not enabled")
	}
	if p.sender == nil {
		return fmt.Errorf("WhatsApp client is not connected")
	}

	message, err := resolveMessage(block.Message, params.TagReader)
	if err != nil {
		return fmt.Errorf("resolve message template: %w", err)
	}

	phones := p.cfg.WASubscribersForTopic(block.Topic)
	if len(phones) == 0 {
		p.logFn("whatsapp: no subscribers for topic %q", block.Topic)
		return nil
	}

	var errs []string
	for _, phone := range phones {
		if err := p.SendWhatsApp(phone, message); err != nil {
			p.logFn("whatsapp: failed to send to %s: %v", phone, err)
			errs = append(errs, phone+": "+err.Error())
		} else {
			p.logFn("whatsapp: sent to %s", phone)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("whatsapp: %d/%d failed: %s", len(errs), len(phones), fmt.Sprintf("%v", errs))
	}
	return nil
}

// SendWhatsApp sends a WhatsApp message to a single phone number.
func (p *WhatsAppProvider) SendWhatsApp(phone, message string) error {
	if p.sender == nil {
		return fmt.Errorf("WhatsApp client is not connected")
	}
	if !p.rateLimiter.Allow() {
		return fmt.Errorf("WhatsApp rate limit exceeded")
	}
	return p.sender.SendMessage(context.Background(), phone, message)
}
