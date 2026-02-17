// Package config handles configuration persistence for the WarAlert application.
package config

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yatesdr/plcio/driver"
	"gopkg.in/yaml.v3"
)

// TagReader reads a PLC tag value by PLC name and tag name.
// This interface is defined here to avoid import cycles between chain and action packages.
type TagReader interface {
	ReadTagValue(plcName, tagName string) (interface{}, error)
}

// ConfigListenerID is a unique identifier for a config change listener.
type ConfigListenerID string

// PLCFamily is an alias for the PLC family type defined in plcio/driver.
type PLCFamily = driver.PLCFamily

// PLCConfig is an alias for the PLC config type defined in plcio/driver.
type PLCConfig = driver.PLCConfig

const (
	FamilyLogix    = driver.FamilyLogix
	FamilyMicro800 = driver.FamilyMicro800
	FamilyS7       = driver.FamilyS7
	FamilyOmron    = driver.FamilyOmron
	FamilyBeckhoff = driver.FamilyBeckhoff
)

// SourceConfig represents a data source: a WarLink REST API, a direct PLC connection, or a ping monitor.
type SourceConfig struct {
	Type    string `yaml:"type" json:"type"`       // "warlink", "plc", or "ping"
	Name    string `yaml:"name" json:"name"`       // display name / PLC name
	Enabled bool   `yaml:"enabled" json:"enabled"`

	// WarLink fields (type == "warlink")
	URL      string        `yaml:"url,omitempty" json:"url,omitempty"`
	PollRate time.Duration `yaml:"poll_rate,omitempty" json:"poll_rate,omitempty"`

	// Direct PLC fields (type == "plc")
	Address string         `yaml:"address,omitempty" json:"address,omitempty"`
	Family  PLCFamily      `yaml:"family,omitempty" json:"family,omitempty"`
	Slot    byte           `yaml:"slot,omitempty" json:"slot,omitempty"`
	Tags    []TagSelection `yaml:"tags,omitempty" json:"tags,omitempty"`

	// Ping monitor fields (type == "ping")
	Host         string        `yaml:"host,omitempty" json:"host,omitempty"`
	Port         int           `yaml:"port,omitempty" json:"port,omitempty"`          // 0 = ICMP, >0 = TCP
	PingInterval time.Duration `yaml:"ping_interval,omitempty" json:"ping_interval,omitempty"`
}

// ToPLCConfig converts a direct PLC source config to the PLCConfig type used by plcio.
func (s SourceConfig) ToPLCConfig() PLCConfig {
	return PLCConfig{
		Name:    s.Name,
		Address: s.Address,
		Family:  s.Family,
		Slot:    s.Slot,
		Enabled: s.Enabled,
		Tags:    s.Tags,
	}
}

// Config holds the complete application configuration.
type Config struct {
	Sources     []SourceConfig    `yaml:"sources"`
	PLCs        []PLCConfig       `yaml:"plcs,omitempty"`
	Web         WebConfig         `yaml:"web"`
	PollRate    time.Duration     `yaml:"poll_rate"`
	AuditLog    string            `yaml:"audit_log"`
	Providers   ProvidersConfig   `yaml:"providers"`
	Topics      []string          `yaml:"topics"`
	Subscribers []SubscriberConfig `yaml:"subscribers"`
	Chains      []ChainConfig     `yaml:"chains"`

	dataMu          sync.Mutex                     `yaml:"-"`
	changeListeners map[ConfigListenerID]func()    `yaml:"-"`
	listenersMu     sync.RWMutex                   `yaml:"-"`
	listenerCounter uint64                         `yaml:"-"`
}

// WebConfig holds web server configuration.
type WebConfig struct {
	Host          string    `yaml:"host"`
	Port          int       `yaml:"port"`
	SessionSecret string   `yaml:"session_secret,omitempty"`
	Users         []WebUser `yaml:"users,omitempty"`
}

// WebUser represents a web interface user.
type WebUser struct {
	Username     string `yaml:"username"`
	PasswordHash string `yaml:"password_hash"`
	Role         string `yaml:"role"`
}

// Web user roles
const (
	RoleAdmin  = "admin"
	RoleViewer = "viewer"
)

// ProvidersConfig holds alert provider configurations.
type ProvidersConfig struct {
	SMS   SMSProviderConfig   `yaml:"sms"`
	Email EmailProviderConfig `yaml:"email"`
}

// SMSProviderConfig holds SMS provider configuration.
type SMSProviderConfig struct {
	Type             string `yaml:"type" json:"type"`
	Enabled          bool   `yaml:"enabled" json:"enabled"`
	Mode             string `yaml:"mode,omitempty" json:"mode,omitempty"` // "local" or "cloud" (default: "cloud")
	BaseURL          string `yaml:"base_url,omitempty" json:"base_url,omitempty"`
	Username         string `yaml:"username,omitempty" json:"username,omitempty"`
	Password         string `yaml:"password,omitempty" json:"password,omitempty"`
	WebhookSecret    string `yaml:"webhook_secret,omitempty" json:"webhook_secret,omitempty"`
	AccountSID       string `yaml:"account_sid,omitempty" json:"account_sid,omitempty"`
	AuthToken        string `yaml:"auth_token,omitempty" json:"auth_token,omitempty"`
	FromNumber       string `yaml:"from_number,omitempty" json:"from_number,omitempty"`
	GlobalRatePerMin int    `yaml:"global_rate_per_min,omitempty" json:"global_rate_per_min,omitempty"`
}

// EmailProviderConfig holds email provider configuration.
type EmailProviderConfig struct {
	Enabled  bool   `yaml:"enabled" json:"enabled"`
	Host     string `yaml:"host" json:"host"`
	Port     int    `yaml:"port" json:"port"`
	Username string `yaml:"username,omitempty" json:"username,omitempty"`
	Password string `yaml:"password,omitempty" json:"password,omitempty"`
	From     string `yaml:"from" json:"from"`
	UseTLS   bool   `yaml:"use_tls" json:"use_tls"`
}

// SubscriberConfig holds SMS subscriber configuration.
type SubscriberConfig struct {
	Phone  string   `yaml:"phone"`
	Name   string   `yaml:"name"`
	Topics []string `yaml:"topics"`
	Active bool     `yaml:"active"`
}

// ChainConfig holds configuration for an alert chain.
type ChainConfig struct {
	Name       string        `yaml:"name" json:"name"`
	Enabled    bool          `yaml:"enabled" json:"enabled"`
	DebounceSec int          `yaml:"debounce_sec,omitempty" json:"debounce_sec,omitempty"`
	CooldownSec int          `yaml:"cooldown_sec,omitempty" json:"cooldown_sec,omitempty"`
	Blocks     []BlockConfig `yaml:"blocks" json:"blocks"`
}

// BlockConfig represents a single block in a chain (gate or action).
type BlockConfig struct {
	Type       string            `yaml:"type" json:"type"`
	Name       string            `yaml:"name" json:"name"`
	LogicMode  string            `yaml:"logic_mode,omitempty" json:"logic_mode,omitempty"`
	Conditions []ConditionConfig `yaml:"conditions,omitempty" json:"conditions,omitempty"`
	DurationMin int              `yaml:"duration_min,omitempty" json:"duration_min,omitempty"`

	// Action fields
	ActionType string            `yaml:"action_type,omitempty" json:"action_type,omitempty"`
	Topic      string            `yaml:"topic,omitempty" json:"topic,omitempty"`
	To         []string          `yaml:"to,omitempty" json:"to,omitempty"`
	Subject    string            `yaml:"subject,omitempty" json:"subject,omitempty"`
	Body       string            `yaml:"body,omitempty" json:"body,omitempty"`
	Message    string            `yaml:"message,omitempty" json:"message,omitempty"`

	// Webhook fields
	URL         string            `yaml:"url,omitempty" json:"url,omitempty"`
	Method      string            `yaml:"method,omitempty" json:"method,omitempty"`
	ContentType string            `yaml:"content_type,omitempty" json:"content_type,omitempty"`
	Headers     map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	Auth        AuthConfig        `yaml:"auth,omitempty" json:"auth,omitempty"`
	Timeout     time.Duration     `yaml:"timeout,omitempty" json:"timeout,omitempty"`
}

// ConditionConfig defines a single condition.
type ConditionConfig struct {
	Type     string      `yaml:"type" json:"type"`
	PLC      string      `yaml:"plc,omitempty" json:"plc,omitempty"`
	Tag      string      `yaml:"tag,omitempty" json:"tag,omitempty"`
	Operator string      `yaml:"operator,omitempty" json:"operator,omitempty"`
	Value    interface{} `yaml:"value,omitempty" json:"value,omitempty"`
	TimeFrom string      `yaml:"time_from,omitempty" json:"time_from,omitempty"`
	TimeTo   string      `yaml:"time_to,omitempty" json:"time_to,omitempty"`
	Days     []string    `yaml:"days,omitempty" json:"days,omitempty"`
}

// AuthConfig holds authentication configuration for webhook actions.
type AuthConfig struct {
	Type        string `yaml:"type,omitempty" json:"type,omitempty"`
	Token       string `yaml:"token,omitempty" json:"token,omitempty"`
	Username    string `yaml:"username,omitempty" json:"username,omitempty"`
	Password    string `yaml:"password,omitempty" json:"password,omitempty"`
	HeaderName  string `yaml:"header_name,omitempty" json:"header_name,omitempty"`
	HeaderValue string `yaml:"header_value,omitempty" json:"header_value,omitempty"`
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Sources: []SourceConfig{
			{
				Type:    "warlink",
				Name:    "WarLink",
				Enabled: true,
				URL:     "http://localhost:8080/api",
			},
		},
		PollRate: 500 * time.Millisecond,
		AuditLog: "audit.log",
		Web: WebConfig{
			Host: "0.0.0.0",
			Port: 8082,
		},
		Providers: ProvidersConfig{
			SMS: SMSProviderConfig{
				Type:             "smsgate",
				GlobalRatePerMin: 30,
			},
			Email: EmailProviderConfig{
				Port:   587,
				UseTLS: true,
			},
		},
		Topics:      []string{},
		Subscribers: []SubscriberConfig{},
		Chains:      []ChainConfig{},
	}
}

// Load reads configuration from a YAML file.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()
	dirty := false

	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		dirty = true
	} else {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, err
		}
	}

	// Auto-migrate old PLCs config to Sources
	if len(cfg.PLCs) > 0 && len(cfg.Sources) == 0 {
		for _, plc := range cfg.PLCs {
			cfg.Sources = append(cfg.Sources, SourceConfig{
				Type:    "plc",
				Name:    plc.Name,
				Enabled: plc.Enabled,
				Address: plc.Address,
				Family:  plc.Family,
				Slot:    plc.Slot,
				Tags:    plc.Tags,
			})
		}
		cfg.PLCs = nil
		dirty = true
	}

	if cfg.Web.SessionSecret == "" {
		secret := make([]byte, 32)
		rand.Read(secret)
		cfg.Web.SessionSecret = base64.StdEncoding.EncodeToString(secret)
		dirty = true
	}

	if dirty {
		cfg.Save(path)
	}

	return cfg, nil
}

// AddOnChangeListener registers a callback to be called when the config is saved.
func (c *Config) AddOnChangeListener(cb func()) ConfigListenerID {
	c.listenersMu.Lock()
	defer c.listenersMu.Unlock()

	if c.changeListeners == nil {
		c.changeListeners = make(map[ConfigListenerID]func())
	}

	id := ConfigListenerID(fmt.Sprintf("listener-%d", atomic.AddUint64(&c.listenerCounter, 1)))
	c.changeListeners[id] = cb
	return id
}

// RemoveOnChangeListener removes a previously registered listener.
func (c *Config) RemoveOnChangeListener(id ConfigListenerID) {
	c.listenersMu.Lock()
	defer c.listenersMu.Unlock()
	delete(c.changeListeners, id)
}

func (c *Config) notifyChangeListeners() {
	c.listenersMu.RLock()
	listeners := make([]func(), 0, len(c.changeListeners))
	for _, cb := range c.changeListeners {
		listeners = append(listeners, cb)
	}
	c.listenersMu.RUnlock()

	for _, cb := range listeners {
		go cb()
	}
}

// Lock acquires the config data mutex for exclusive access.
func (c *Config) Lock() { c.dataMu.Lock() }

// Unlock releases the config data mutex without saving.
func (c *Config) Unlock() { c.dataMu.Unlock() }

// Save acquires the lock, marshals, writes, and notifies.
func (c *Config) Save(path string) error {
	c.dataMu.Lock()
	return c.saveLocked(path)
}

// UnlockAndSave marshals, releases the lock, writes, and notifies.
func (c *Config) UnlockAndSave(path string) error {
	return c.saveLocked(path)
}

func (c *Config) saveLocked(path string) error {
	data, err := yaml.Marshal(c)
	c.dataMu.Unlock()

	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return err
	}

	c.notifyChangeListeners()
	return nil
}

// FindPLC returns the PLC config with the given name, or nil if not found.
func (c *Config) FindPLC(name string) *PLCConfig {
	for i := range c.PLCs {
		if c.PLCs[i].Name == name {
			return &c.PLCs[i]
		}
	}
	return nil
}

// AddPLC adds a new PLC configuration.
func (c *Config) AddPLC(plc PLCConfig) {
	c.PLCs = append(c.PLCs, plc)
}

// RemovePLC removes a PLC by name.
func (c *Config) RemovePLC(name string) bool {
	for i, plc := range c.PLCs {
		if plc.Name == name {
			c.PLCs = append(c.PLCs[:i], c.PLCs[i+1:]...)
			return true
		}
	}
	return false
}

// UpdatePLC updates an existing PLC configuration.
func (c *Config) UpdatePLC(name string, updated PLCConfig) bool {
	for i, plc := range c.PLCs {
		if plc.Name == name {
			c.PLCs[i] = updated
			return true
		}
	}
	return false
}

// FindSource returns the Source config with the given name, or nil if not found.
func (c *Config) FindSource(name string) *SourceConfig {
	for i := range c.Sources {
		if c.Sources[i].Name == name {
			return &c.Sources[i]
		}
	}
	return nil
}

// AddSource adds a new source configuration.
func (c *Config) AddSource(src SourceConfig) {
	c.Sources = append(c.Sources, src)
}

// RemoveSource removes a source by name.
func (c *Config) RemoveSource(name string) bool {
	for i, s := range c.Sources {
		if s.Name == name {
			c.Sources = append(c.Sources[:i], c.Sources[i+1:]...)
			return true
		}
	}
	return false
}

// UpdateSource updates an existing source configuration.
func (c *Config) UpdateSource(name string, updated SourceConfig) bool {
	for i, s := range c.Sources {
		if s.Name == name {
			c.Sources[i] = updated
			return true
		}
	}
	return false
}

// FindChain returns the Chain config with the given name, or nil if not found.
func (c *Config) FindChain(name string) *ChainConfig {
	for i := range c.Chains {
		if c.Chains[i].Name == name {
			return &c.Chains[i]
		}
	}
	return nil
}

// AddChain adds a new Chain configuration.
func (c *Config) AddChain(chain ChainConfig) {
	c.Chains = append(c.Chains, chain)
}

// RemoveChain removes a Chain config by name.
func (c *Config) RemoveChain(name string) bool {
	for i, ch := range c.Chains {
		if ch.Name == name {
			c.Chains = append(c.Chains[:i], c.Chains[i+1:]...)
			return true
		}
	}
	return false
}

// UpdateChain updates an existing Chain configuration.
func (c *Config) UpdateChain(name string, updated ChainConfig) bool {
	for i, ch := range c.Chains {
		if ch.Name == name {
			c.Chains[i] = updated
			return true
		}
	}
	return false
}

// FindWebUser returns the web user with the given username, or nil if not found.
func (c *Config) FindWebUser(username string) *WebUser {
	for i := range c.Web.Users {
		if c.Web.Users[i].Username == username {
			return &c.Web.Users[i]
		}
	}
	return nil
}

// AddWebUser adds a new web user.
func (c *Config) AddWebUser(user WebUser) {
	c.Web.Users = append(c.Web.Users, user)
}

// FindSubscriber returns the subscriber with the given phone, or nil if not found.
func (c *Config) FindSubscriber(phone string) *SubscriberConfig {
	for i := range c.Subscribers {
		if c.Subscribers[i].Phone == phone {
			return &c.Subscribers[i]
		}
	}
	return nil
}

// AddSubscriber adds a new subscriber.
func (c *Config) AddSubscriber(sub SubscriberConfig) {
	c.Subscribers = append(c.Subscribers, sub)
}

// RemoveSubscriber removes a subscriber by phone.
func (c *Config) RemoveSubscriber(phone string) bool {
	for i, s := range c.Subscribers {
		if s.Phone == phone {
			c.Subscribers = append(c.Subscribers[:i], c.Subscribers[i+1:]...)
			return true
		}
	}
	return false
}

// TagSelection is an alias for the driver tag selection type.
type TagSelection = driver.TagSelection

// AddTopic appends a topic if it doesn't already exist.
func (c *Config) AddTopic(topic string) bool {
	for _, t := range c.Topics {
		if t == topic {
			return false
		}
	}
	c.Topics = append(c.Topics, topic)
	return true
}

// RemoveTopic removes a topic by value.
func (c *Config) RemoveTopic(topic string) bool {
	for i, t := range c.Topics {
		if t == topic {
			c.Topics = append(c.Topics[:i], c.Topics[i+1:]...)
			return true
		}
	}
	return false
}

// RemoveWebUser removes a web user by username.
func (c *Config) RemoveWebUser(username string) bool {
	for i, u := range c.Web.Users {
		if u.Username == username {
			c.Web.Users = append(c.Web.Users[:i], c.Web.Users[i+1:]...)
			return true
		}
	}
	return false
}

// UpdateWebUser updates an existing web user by username.
func (c *Config) UpdateWebUser(username string, updated WebUser) bool {
	for i, u := range c.Web.Users {
		if u.Username == username {
			c.Web.Users[i] = updated
			return true
		}
	}
	return false
}

// UpdateSubscriber updates an existing subscriber by phone.
func (c *Config) UpdateSubscriber(phone string, updated SubscriberConfig) bool {
	for i, s := range c.Subscribers {
		if s.Phone == phone {
			c.Subscribers[i] = updated
			return true
		}
	}
	return false
}

// AddPLCTag adds a tag selection to a PLC source's config.
func (c *Config) AddPLCTag(plcName string, tag TagSelection) bool {
	src := c.FindSource(plcName)
	if src == nil || src.Type != "plc" {
		return false
	}
	for _, t := range src.Tags {
		if t.Name == tag.Name {
			return false
		}
	}
	src.Tags = append(src.Tags, tag)
	return true
}

// RemovePLCTag removes a tag from a PLC source's config by tag name.
func (c *Config) RemovePLCTag(plcName, tagName string) bool {
	src := c.FindSource(plcName)
	if src == nil || src.Type != "plc" {
		return false
	}
	for i, t := range src.Tags {
		if t.Name == tagName {
			src.Tags = append(src.Tags[:i], src.Tags[i+1:]...)
			return true
		}
	}
	return false
}

// SubscribersForTopic returns all active subscriber phone numbers subscribed to a topic.
// Matching is case-insensitive and supports hierarchy: subscriber topic "FIRE" matches
// fired topic "FIRE-ZONE1" (parent covers subtopic at "-" boundary).
func (c *Config) SubscribersForTopic(topic string) []string {
	upper := strings.ToUpper(topic)
	var phones []string
	for _, sub := range c.Subscribers {
		if !sub.Active {
			continue
		}
		for _, t := range sub.Topics {
			tu := strings.ToUpper(t)
			if tu == upper || strings.HasPrefix(upper, tu+"-") {
				phones = append(phones, sub.Phone)
				break
			}
		}
	}
	return phones
}
