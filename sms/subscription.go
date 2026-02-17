// Package sms handles SMS subscription management, incoming command parsing,
// and webhook handling for both SMS-gate and Twilio providers.
package sms

import (
	"fmt"
	"strings"

	"waralert/config"
)

// SMSSender sends an SMS message to a phone number.
type SMSSender interface {
	SendSMS(phone, message string) error
}

// Manager manages SMS subscriptions backed by the application config.
type Manager struct {
	cfg        *config.Config
	configPath string
	smsSender  SMSSender
	logFn      func(string, ...interface{})
}

// NewManager creates a new subscription manager.
func NewManager(cfg *config.Config, configPath string, sender SMSSender, logFn func(string, ...interface{})) *Manager {
	if logFn == nil {
		logFn = func(string, ...interface{}) {}
	}
	return &Manager{
		cfg:        cfg,
		configPath: configPath,
		smsSender:  sender,
		logFn:      logFn,
	}
}

// Subscribe adds a topic to a subscriber's list with hierarchy awareness.
// If the subscriber does not exist, a new active subscriber is created.
// If an existing topic already covers the new one (parent), the new topic is silently ignored.
// If the new topic is a parent of existing subtopics, those subtopics are removed.
func (m *Manager) Subscribe(phone, topic string) error {
	m.cfg.Lock()

	upper := strings.ToUpper(topic)

	sub := m.cfg.FindSubscriber(phone)
	if sub == nil {
		m.cfg.AddSubscriber(config.SubscriberConfig{
			Phone:  phone,
			Topics: []string{upper},
			Active: true,
		})
		m.cfg.AddTopic(upper)
		return m.cfg.UnlockAndSave(m.configPath)
	}

	// Re-activate if previously stopped.
	sub.Active = true

	// Check if already covered by an exact match or a parent topic.
	for _, t := range sub.Topics {
		tu := strings.ToUpper(t)
		if tu == upper || strings.HasPrefix(upper, tu+"-") {
			// Already covered — nothing to add.
			m.cfg.AddTopic(upper)
			return m.cfg.UnlockAndSave(m.configPath)
		}
	}

	// Remove any existing subtopics that the new parent covers.
	filtered := sub.Topics[:0]
	for _, t := range sub.Topics {
		tu := strings.ToUpper(t)
		if !strings.HasPrefix(tu, upper+"-") {
			filtered = append(filtered, t)
		}
	}
	sub.Topics = append(filtered, upper)

	m.cfg.AddTopic(upper)
	return m.cfg.UnlockAndSave(m.configPath)
}

// Unsubscribe removes a topic and any subtopics from a subscriber's list.
// For example, UNSUB FIRE removes FIRE and any FIRE-* subtopics.
func (m *Manager) Unsubscribe(phone, topic string) error {
	m.cfg.Lock()

	sub := m.cfg.FindSubscriber(phone)
	if sub == nil {
		m.cfg.Unlock()
		return fmt.Errorf("subscriber %s not found", phone)
	}

	upper := strings.ToUpper(topic)
	filtered := sub.Topics[:0]
	for _, t := range sub.Topics {
		tu := strings.ToUpper(t)
		if tu != upper && !strings.HasPrefix(tu, upper+"-") {
			filtered = append(filtered, t)
		}
	}
	sub.Topics = filtered

	return m.cfg.UnlockAndSave(m.configPath)
}

// UnsubscribeAll deactivates a subscriber, used for the STOP command.
func (m *Manager) UnsubscribeAll(phone string) error {
	m.cfg.Lock()

	sub := m.cfg.FindSubscriber(phone)
	if sub == nil {
		m.cfg.Unlock()
		return fmt.Errorf("subscriber %s not found", phone)
	}

	sub.Active = false

	return m.cfg.UnlockAndSave(m.configPath)
}

// ListTopics returns the subscriber's current topic list, or nil if not found.
func (m *Manager) ListTopics(phone string) []string {
	m.cfg.Lock()
	defer m.cfg.Unlock()

	sub := m.cfg.FindSubscriber(phone)
	if sub == nil {
		return nil
	}
	out := make([]string, len(sub.Topics))
	copy(out, sub.Topics)
	return out
}

// ListAllTopics returns all globally configured topics.
func (m *Manager) ListAllTopics() []string {
	m.cfg.Lock()
	defer m.cfg.Unlock()

	out := make([]string, len(m.cfg.Topics))
	copy(out, m.cfg.Topics)
	return out
}
