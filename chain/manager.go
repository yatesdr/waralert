package chain

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"waralert/config"
)

// GateStatus represents the evaluation state of a gate block.
type GateStatus struct {
	Status       string `json:"status"`                  // "true", "waiting", "false"
	RemainingSec int    `json:"remaining_sec,omitempty"`  // seconds remaining on duration timer (only for "waiting")
}

// ChainInfo provides a summary view of a chain's state for external consumers.
type ChainInfo struct {
	Name       string
	Status     Status
	FireCount  int
	LastFire   time.Time
	BlockCount int
	Error      string
	// Gate progress (live evaluation)
	GatesPassing    int
	GatesTotal      int
	TimerRemainingSec int // >0 if first gate waiting on duration; -1 otherwise
}

// Manager manages the lifecycle of all chains.
type Manager struct {
	mu             sync.RWMutex
	chains         map[string]*Chain
	tagReader      TagReader
	actionExecutor ActionExecutor
	logFn          func(string, ...interface{})
	editTimers     map[int]time.Time // gate index → time conditions first became true (editor polling)
}

// NewManager creates a new chain Manager.
func NewManager(tagReader TagReader, actionExecutor ActionExecutor, logFn func(string, ...interface{})) *Manager {
	if logFn == nil {
		logFn = func(string, ...interface{}) {}
	}
	return &Manager{
		chains:         make(map[string]*Chain),
		tagReader:      tagReader,
		actionExecutor: actionExecutor,
		logFn:          logFn,
		editTimers:     make(map[int]time.Time),
	}
}

// AddChain creates and registers a new chain from configuration.
// Returns an error if a chain with the same name already exists.
func (m *Manager) AddChain(cfg config.ChainConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.chains[cfg.Name]; exists {
		return fmt.Errorf("chain %q already exists", cfg.Name)
	}

	c := NewChain(cfg, m.tagReader, m.actionExecutor, m.logFn)
	m.chains[cfg.Name] = c
	return nil
}

// RemoveChain stops and removes a chain by name.
// Returns an error if the chain does not exist.
func (m *Manager) RemoveChain(name string) error {
	m.mu.Lock()
	c, exists := m.chains[name]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("chain %q not found", name)
	}
	delete(m.chains, name)
	m.mu.Unlock()

	c.Stop()
	return nil
}

// GetChain returns the chain with the given name, or nil if not found.
func (m *Manager) GetChain(name string) *Chain {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.chains[name]
}

// ListChains returns the names of all registered chains.
func (m *Manager) ListChains() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.chains))
	for name := range m.chains {
		names = append(names, name)
	}
	return names
}

// Start starts all enabled chains.
func (m *Manager) Start() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, c := range m.chains {
		c.Start()
	}
	m.logFn("chain manager: started %d chains", len(m.chains))
}

// Stop stops all running chains.
func (m *Manager) Stop() {
	m.mu.RLock()
	chains := make([]*Chain, 0, len(m.chains))
	for _, c := range m.chains {
		chains = append(chains, c)
	}
	m.mu.RUnlock()

	for _, c := range chains {
		c.Stop()
	}
	m.logFn("chain manager: stopped all chains")
}

// StartChain starts a single chain by name.
func (m *Manager) StartChain(name string) error {
	m.mu.RLock()
	c, exists := m.chains[name]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("chain %q not found", name)
	}

	c.Start()
	return nil
}

// StopChain stops a single chain by name.
func (m *Manager) StopChain(name string) error {
	m.mu.RLock()
	c, exists := m.chains[name]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("chain %q not found", name)
	}

	c.Stop()
	return nil
}

// LoadFromConfig loads all chains from the provided configuration slice.
// Existing chains are stopped and removed first.
func (m *Manager) LoadFromConfig(chains []config.ChainConfig) {
	// Stop and remove all existing chains
	m.Stop()

	m.mu.Lock()
	m.chains = make(map[string]*Chain)
	m.mu.Unlock()

	for _, cfg := range chains {
		if err := m.AddChain(cfg); err != nil {
			m.logFn("chain manager: failed to add chain %q: %v", cfg.Name, err)
		}
	}
	m.logFn("chain manager: loaded %d chains from config", len(chains))
}

// EvaluateBlocks evaluates the given gate blocks against live tag values and returns
// a map of block index → GateStatus with status and remaining timer info.
func (m *Manager) EvaluateBlocks(blocks []config.BlockConfig) map[int]GateStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	results := make(map[int]GateStatus)
	for i, block := range blocks {
		if block.Type != "gate" {
			continue
		}
		gate := NewGate(block)
		result, err := gate.Evaluate(m.tagReader)
		if err != nil {
			delete(m.editTimers, i)
			results[i] = GateStatus{Status: "false"}
			continue
		}
		if !result {
			delete(m.editTimers, i)
			results[i] = GateStatus{Status: "false"}
		} else if block.DurationMin > 0 {
			started, exists := m.editTimers[i]
			if !exists {
				m.editTimers[i] = time.Now()
				started = time.Now()
			}
			elapsed := time.Since(started)
			total := time.Duration(block.DurationMin) * time.Minute
			if elapsed >= total {
				results[i] = GateStatus{Status: "true"}
			} else {
				remaining := int(math.Ceil((total - elapsed).Seconds()))
				results[i] = GateStatus{Status: "waiting", RemainingSec: remaining}
			}
		} else {
			results[i] = GateStatus{Status: "true"}
		}
	}
	return results
}

// ResetEditTimers clears the editor gate timers (call when opening a new edit session).
func (m *Manager) ResetEditTimers() {
	m.mu.Lock()
	m.editTimers = make(map[int]time.Time)
	m.mu.Unlock()
}

// GetAllChainInfo returns a ChainInfo snapshot for every registered chain,
// sorted by name for stable display order.
func (m *Manager) GetAllChainInfo() []ChainInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	infos := make([]ChainInfo, 0, len(m.chains))
	for _, c := range m.chains {
		stats := c.GetStats()
		cfg := c.GetConfig()
		gp := c.GetGateProgress()
		infos = append(infos, ChainInfo{
			Name:              cfg.Name,
			Status:            stats.Status,
			FireCount:         stats.FireCount,
			LastFire:          stats.LastFire,
			BlockCount:        len(cfg.Blocks),
			Error:             stats.LastError,
			GatesPassing:      gp.Passing,
			GatesTotal:        gp.Total,
			TimerRemainingSec: gp.RemainingSec,
		})
	}
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Name < infos[j].Name
	})
	return infos
}
