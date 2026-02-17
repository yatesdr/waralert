package whatsapp

import (
	"fmt"
	"strings"

	"waralert/config"
)

// Manager manages WhatsApp subscriptions backed by the application config.
// It implements sms.SubscriptionManager.
type Manager struct {
	cfg        *config.Config
	configPath string
	logFn      func(string, ...interface{})
}

// NewManager creates a new WhatsApp subscription manager.
func NewManager(cfg *config.Config, configPath string, logFn func(string, ...interface{})) *Manager {
	if logFn == nil {
		logFn = func(string, ...interface{}) {}
	}
	return &Manager{
		cfg:        cfg,
		configPath: configPath,
		logFn:      logFn,
	}
}

// Subscribe adds a topic to a WA subscriber's list with hierarchy awareness.
func (m *Manager) Subscribe(phone, topic string) error {
	m.cfg.Lock()

	upper := strings.ToUpper(topic)

	sub := m.cfg.FindWASubscriber(phone)
	if sub == nil {
		m.cfg.AddWASubscriber(config.SubscriberConfig{
			Phone:  phone,
			Topics: []string{upper},
			Active: true,
		})
		m.cfg.AddTopic(upper)
		return m.cfg.UnlockAndSave(m.configPath)
	}

	sub.Active = true

	for _, t := range sub.Topics {
		tu := strings.ToUpper(t)
		if tu == upper || strings.HasPrefix(upper, tu+"-") {
			m.cfg.AddTopic(upper)
			return m.cfg.UnlockAndSave(m.configPath)
		}
	}

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

// Unsubscribe removes a topic and subtopics from a WA subscriber's list.
func (m *Manager) Unsubscribe(phone, topic string) error {
	m.cfg.Lock()

	sub := m.cfg.FindWASubscriber(phone)
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

// UnsubscribeAll deactivates a WA subscriber.
func (m *Manager) UnsubscribeAll(phone string) error {
	m.cfg.Lock()

	sub := m.cfg.FindWASubscriber(phone)
	if sub == nil {
		m.cfg.Unlock()
		return fmt.Errorf("subscriber %s not found", phone)
	}

	sub.Active = false

	return m.cfg.UnlockAndSave(m.configPath)
}

// ListTopics returns the subscriber's current topic list.
func (m *Manager) ListTopics(phone string) []string {
	m.cfg.Lock()
	defer m.cfg.Unlock()

	sub := m.cfg.FindWASubscriber(phone)
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
