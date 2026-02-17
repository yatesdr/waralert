// Package plcman provides a simplified PLC connection manager for the waralert
// alert system. It manages per-PLC polling goroutines, detects value changes,
// and provides tag values to the chain evaluation engine.
package plcman

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/yatesdr/plcio/driver"
	"waralert/config"
)

// ConnectionStatus represents the current state of a PLC connection.
type ConnectionStatus int

const (
	Disconnected ConnectionStatus = iota
	Connecting
	Connected
	Error
)

// String returns a human-readable name for the connection status.
func (s ConnectionStatus) String() string {
	switch s {
	case Disconnected:
		return "Disconnected"
	case Connecting:
		return "Connecting"
	case Connected:
		return "Connected"
	case Error:
		return "Error"
	default:
		return "Unknown"
	}
}

// maxReconnectAttempts is the number of consecutive connection failures before
// the worker stops retrying and moves to Error status.
const maxReconnectAttempts = 5

// ValueChangeCallback is called when a tag value changes. It receives the PLC
// name, tag name, and new TagValue.
type ValueChangeCallback func(plcName, tagName string, value TagValue)

// ManagedPLC holds the runtime state for a single PLC connection.
type ManagedPLC struct {
	Config    config.PLCConfig
	Driver    driver.Driver
	Tags      []driver.TagSelection
	Values    map[string]TagValue
	Status    ConnectionStatus
	LastError error

	Mu sync.RWMutex
}

// PLCWorker runs a per-PLC polling goroutine.
type PLCWorker struct {
	plc      *ManagedPLC
	manager  *Manager
	cancel   context.CancelFunc
	done     chan struct{}
}

// Manager orchestrates all PLC connections and their polling workers.
type Manager struct {
	plcs     map[string]*ManagedPLC
	workers  map[string]*PLCWorker
	pollRate time.Duration

	warlinkPollers map[string]*WarLinkPoller // warlink pollers, keyed by source name
	pingPollers    map[string]*PingPoller    // ping pollers, keyed by source name
	plcSources     map[string]string         // plcName -> sourceName (tracks ownership)

	ctx    context.Context
	cancel context.CancelFunc

	onValueChange ValueChangeCallback

	mu sync.RWMutex
}

// NewManager creates a Manager with the given poll rate.
func NewManager(pollRate time.Duration) *Manager {
	if pollRate <= 0 {
		pollRate = 500 * time.Millisecond
	}
	return &Manager{
		plcs:           make(map[string]*ManagedPLC),
		workers:        make(map[string]*PLCWorker),
		pollRate:       pollRate,
		warlinkPollers: make(map[string]*WarLinkPoller),
		pingPollers:    make(map[string]*PingPoller),
		plcSources:     make(map[string]string),
	}
}

// SetOnValueChange registers a callback invoked whenever a tag value changes.
func (m *Manager) SetOnValueChange(cb ValueChangeCallback) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onValueChange = cb
}

// AddPLC registers a PLC configuration with the manager. The PLC is not
// connected until Connect or Start is called.
func (m *Manager) AddPLC(cfg config.PLCConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.plcs[cfg.Name]; exists {
		return fmt.Errorf("plc %q already exists", cfg.Name)
	}

	managed := &ManagedPLC{
		Config: cfg,
		Tags:   cfg.Tags,
		Values: make(map[string]TagValue),
		Status: Disconnected,
	}
	m.plcs[cfg.Name] = managed
	return nil
}

// RemovePLC stops and removes a PLC by name.
func (m *Manager) RemovePLC(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	plc, exists := m.plcs[name]
	if !exists {
		return fmt.Errorf("plc %q not found", name)
	}

	// Stop worker if running.
	if w, ok := m.workers[name]; ok {
		w.cancel()
		<-w.done
		delete(m.workers, name)
	}

	// Close driver if connected.
	plc.Mu.Lock()
	if plc.Driver != nil {
		plc.Driver.Close()
		plc.Driver = nil
	}
	plc.Status = Disconnected
	plc.Mu.Unlock()

	delete(m.plcs, name)
	return nil
}

// Connect establishes a connection to the named PLC and starts its poll worker.
func (m *Manager) Connect(name string) error {
	m.mu.Lock()
	plc, exists := m.plcs[name]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("plc %q not found", name)
	}

	// If the manager hasn't been started yet there is no parent context.
	if m.ctx == nil {
		m.ctx, m.cancel = context.WithCancel(context.Background())
	}
	m.mu.Unlock()

	plc.Mu.Lock()
	plc.Status = Connecting
	plc.LastError = nil
	plc.Mu.Unlock()

	drv, err := driver.Create(&plc.Config)
	if err != nil {
		plc.Mu.Lock()
		plc.Status = Error
		plc.LastError = err
		plc.Mu.Unlock()
		return fmt.Errorf("create driver for %q: %w", name, err)
	}

	if err := drv.Connect(); err != nil {
		plc.Mu.Lock()
		plc.Status = Error
		plc.LastError = err
		plc.Mu.Unlock()
		return fmt.Errorf("connect %q: %w", name, err)
	}

	plc.Mu.Lock()
	plc.Driver = drv
	plc.Status = Connected
	plc.Mu.Unlock()

	m.startWorker(name, plc)
	return nil
}

// Disconnect stops the poll worker and closes the connection for the named PLC.
func (m *Manager) Disconnect(name string) error {
	m.mu.Lock()
	plc, exists := m.plcs[name]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("plc %q not found", name)
	}

	if w, ok := m.workers[name]; ok {
		w.cancel()
		<-w.done
		delete(m.workers, name)
	}
	m.mu.Unlock()

	plc.Mu.Lock()
	defer plc.Mu.Unlock()

	if plc.Driver != nil {
		plc.Driver.Close()
		plc.Driver = nil
	}
	plc.Status = Disconnected
	plc.LastError = nil
	return nil
}

// GetPLC returns the ManagedPLC for the given name, or nil if not found.
func (m *Manager) GetPLC(name string) *ManagedPLC {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.plcs[name]
}

// ListPLCs returns a snapshot of all managed PLC names.
func (m *Manager) ListPLCs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.plcs))
	for name := range m.plcs {
		names = append(names, name)
	}
	return names
}

// ListPingNames returns a snapshot of all managed ping source names.
func (m *Manager) ListPingNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.pingPollers))
	for name := range m.pingPollers {
		names = append(names, name)
	}
	return names
}

// Start creates a lifecycle context, connects all enabled PLCs, and starts warlink/ping pollers.
func (m *Manager) Start() {
	m.mu.Lock()
	m.ctx, m.cancel = context.WithCancel(context.Background())
	// Start warlink pollers
	for _, poller := range m.warlinkPollers {
		poller.Start(m.ctx)
	}
	// Start ping pollers
	for _, poller := range m.pingPollers {
		poller.Start(m.ctx)
	}
	m.mu.Unlock()

	m.ConnectEnabled()
}

// Stop cancels all workers, stops warlink/ping pollers, and disconnects every PLC.
func (m *Manager) Stop() {
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
	}

	// Stop warlink pollers
	for name, poller := range m.warlinkPollers {
		poller.Stop()
		delete(m.warlinkPollers, name)
	}

	// Stop ping pollers
	for name, poller := range m.pingPollers {
		poller.Stop()
		delete(m.pingPollers, name)
	}

	// Wait for all workers to finish.
	for name, w := range m.workers {
		<-w.done
		delete(m.workers, name)
	}
	m.mu.Unlock()

	// Disconnect all PLCs.
	m.mu.RLock()
	names := make([]string, 0, len(m.plcs))
	for name := range m.plcs {
		names = append(names, name)
	}
	m.mu.RUnlock()

	for _, name := range names {
		m.Disconnect(name)
	}
}

// LoadFromConfig populates the manager with PLCs defined in the given config.
// Existing PLCs are not removed; duplicates are skipped.
func (m *Manager) LoadFromConfig(cfg *config.Config) {
	for _, plcCfg := range cfg.PLCs {
		if err := m.AddPLC(plcCfg); err != nil {
			log.Printf("plcman: load %q: %v", plcCfg.Name, err)
		}
	}
}

// LoadFromSources populates the manager from SourceConfig entries.
func (m *Manager) LoadFromSources(sources []config.SourceConfig) {
	for _, src := range sources {
		switch src.Type {
		case "plc":
			plcCfg := src.ToPLCConfig()
			if err := m.AddPLC(plcCfg); err != nil {
				log.Printf("plcman: load source %q: %v", src.Name, err)
				continue
			}
			m.mu.Lock()
			m.plcSources[src.Name] = src.Name
			m.mu.Unlock()
		case "warlink":
			if !src.Enabled {
				continue
			}
			poller := newWarLinkPoller(m, src.Name, src.URL, src.PollRate)
			m.mu.Lock()
			m.warlinkPollers[src.Name] = poller
			m.mu.Unlock()
		case "ping":
			if !src.Enabled {
				continue
			}
			ensurePingPLC(m, src.Name)
			poller := newPingPoller(m, src.Name, src.Host, src.Port, src.PingInterval)
			m.mu.Lock()
			m.pingPollers[src.Name] = poller
			m.mu.Unlock()
		}
	}
}

// AddSource adds a source at runtime and starts it if the manager is running.
func (m *Manager) AddSource(src config.SourceConfig) error {
	switch src.Type {
	case "plc":
		plcCfg := src.ToPLCConfig()
		if err := m.AddPLC(plcCfg); err != nil {
			return err
		}
		m.mu.Lock()
		m.plcSources[src.Name] = src.Name
		m.mu.Unlock()
		if src.Enabled && m.ctx != nil {
			m.Connect(src.Name)
		}
	case "warlink":
		poller := newWarLinkPoller(m, src.Name, src.URL, src.PollRate)
		m.mu.Lock()
		m.warlinkPollers[src.Name] = poller
		if src.Enabled && m.ctx != nil {
			poller.Start(m.ctx)
		}
		m.mu.Unlock()
	case "ping":
		ensurePingPLC(m, src.Name)
		poller := newPingPoller(m, src.Name, src.Host, src.Port, src.PingInterval)
		m.mu.Lock()
		m.pingPollers[src.Name] = poller
		if src.Enabled && m.ctx != nil {
			poller.Start(m.ctx)
		}
		m.mu.Unlock()
	default:
		return fmt.Errorf("unknown source type %q", src.Type)
	}
	return nil
}

// RemoveSource removes a source and all its owned PLCs.
func (m *Manager) RemoveSource(name string, srcType string) {
	switch srcType {
	case "plc":
		m.RemovePLC(name)
		m.mu.Lock()
		delete(m.plcSources, name)
		m.mu.Unlock()
	case "warlink":
		m.mu.Lock()
		poller, ok := m.warlinkPollers[name]
		if ok {
			delete(m.warlinkPollers, name)
		}
		m.mu.Unlock()
		if poller != nil {
			plcNames := poller.PLCNames()
			poller.Stop()
			// Remove PLCs discovered by this source
			for _, plcName := range plcNames {
				m.mu.Lock()
				owner := m.plcSources[plcName]
				m.mu.Unlock()
				if owner == name {
					m.RemovePLC(plcName)
					m.mu.Lock()
					delete(m.plcSources, plcName)
					m.mu.Unlock()
				}
			}
		}
	case "ping":
		m.mu.Lock()
		poller, ok := m.pingPollers[name]
		if ok {
			delete(m.pingPollers, name)
		}
		m.mu.Unlock()
		if poller != nil {
			poller.Stop()
		}
		m.RemovePLC(name)
		m.mu.Lock()
		delete(m.plcSources, name)
		m.mu.Unlock()
	}
}

// GetWarLinkPoller returns the WarLinkPoller for a source name, or nil.
func (m *Manager) GetWarLinkPoller(name string) *WarLinkPoller {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.warlinkPollers[name]
}

// GetPingPoller returns the PingPoller for a source name, or nil.
func (m *Manager) GetPingPoller(name string) *PingPoller {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.pingPollers[name]
}

// ensurePLC creates or updates a ManagedPLC entry for a warlink-discovered PLC.
func (m *Manager) ensurePLC(plcName, statusStr, errStr, sourceName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	mp, exists := m.plcs[plcName]
	if !exists {
		mp = &ManagedPLC{
			Config: config.PLCConfig{Name: plcName},
			Values: make(map[string]TagValue),
		}
		m.plcs[plcName] = mp
	}
	m.plcSources[plcName] = sourceName

	mp.Mu.Lock()
	mp.Status = parseWarlinkStatus(statusStr)
	if errStr != "" {
		mp.LastError = fmt.Errorf("%s", errStr)
	} else {
		mp.LastError = nil
	}
	mp.Mu.Unlock()
}

// ConnectEnabled connects all PLCs whose config has Enabled set to true.
func (m *Manager) ConnectEnabled() {
	m.mu.RLock()
	names := make([]string, 0)
	for name, plc := range m.plcs {
		if plc.Config.Enabled {
			names = append(names, name)
		}
	}
	m.mu.RUnlock()

	for _, name := range names {
		if err := m.Connect(name); err != nil {
			log.Printf("plcman: connect %q: %v", name, err)
		}
	}
}

// ReadTagValue reads the last polled value for a tag on the named PLC. This is
// the primary interface for chain condition evaluation -- it returns the cached
// value without performing a live read.
func (m *Manager) ReadTagValue(plcName, tagName string) (interface{}, error) {
	m.mu.RLock()
	plc, exists := m.plcs[plcName]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("plc %q not found", plcName)
	}

	plc.Mu.RLock()
	defer plc.Mu.RUnlock()

	tv, ok := plc.Values[tagName]
	if !ok {
		return nil, fmt.Errorf("tag %q not found on plc %q", tagName, plcName)
	}
	if tv.Error != nil {
		return nil, tv.Error
	}
	return tv.GoValue(), nil
}

// startWorker launches a polling goroutine for the given PLC.
func (m *Manager) startWorker(name string, plc *ManagedPLC) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Cancel any existing worker.
	if w, ok := m.workers[name]; ok {
		w.cancel()
		<-w.done
	}

	workerCtx, workerCancel := context.WithCancel(m.ctx)
	w := &PLCWorker{
		plc:     plc,
		manager: m,
		cancel:  workerCancel,
		done:    make(chan struct{}),
	}
	m.workers[name] = w

	go w.run(workerCtx)
}

// run is the main poll loop for a single PLC.
func (w *PLCWorker) run(ctx context.Context) {
	defer close(w.done)

	plcName := w.plc.Config.Name
	pollRate := w.manager.pollRate
	if w.plc.Config.PollRate > 0 {
		pollRate = w.plc.Config.PollRate
	}
	ticker := time.NewTicker(pollRate)
	defer ticker.Stop()

	reconnectAttempts := 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.plc.Mu.RLock()
			drv := w.plc.Driver
			tags := w.plc.Tags
			status := w.plc.Status
			w.plc.Mu.RUnlock()

			if status != Connected || drv == nil {
				continue
			}

			if len(tags) == 0 {
				continue
			}

			// Build read requests from configured tags.
			requests := make([]driver.TagRequest, 0, len(tags))
			for _, t := range tags {
				if !t.Enabled {
					continue
				}
				requests = append(requests, driver.TagRequest{
					Name:     t.Name,
					TypeHint: t.DataType,
				})
			}
			if len(requests) == 0 {
				continue
			}

			results, err := drv.Read(requests)
			if err != nil {
				if driver.IsLikelyConnectionError(err) {
					log.Printf("plcman: %s connection error: %v", plcName, err)
					w.handleConnectionLoss(ctx, &reconnectAttempts)
					continue
				}
				log.Printf("plcman: %s read error: %v", plcName, err)
				continue
			}

			// Successful read -- reset reconnect counter.
			reconnectAttempts = 0

			w.processResults(plcName, results)
		}
	}
}

// processResults updates cached values and fires change callbacks.
func (w *PLCWorker) processResults(plcName string, results []*driver.TagValue) {
	w.plc.Mu.Lock()
	defer w.plc.Mu.Unlock()

	for _, res := range results {
		if res == nil {
			continue
		}

		tv := FromDriverTagValue(res)
		prev, existed := w.plc.Values[res.Name]
		w.plc.Values[res.Name] = tv

		changed := !existed || !valuesEqual(prev.Value, tv.Value)
		if changed {
			w.manager.mu.RLock()
			cb := w.manager.onValueChange
			w.manager.mu.RUnlock()

			if cb != nil {
				cb(plcName, res.Name, tv)
			}
		}
	}
}

// handleConnectionLoss attempts to reconnect with a retry limit.
func (w *PLCWorker) handleConnectionLoss(ctx context.Context, attempts *int) {
	*attempts++

	w.plc.Mu.Lock()
	plcName := w.plc.Config.Name
	if w.plc.Driver != nil {
		w.plc.Driver.Close()
		w.plc.Driver = nil
	}

	if *attempts > maxReconnectAttempts {
		w.plc.Status = Error
		w.plc.LastError = fmt.Errorf("exceeded %d reconnect attempts", maxReconnectAttempts)
		w.plc.Mu.Unlock()
		log.Printf("plcman: %s giving up after %d attempts", plcName, maxReconnectAttempts)
		return
	}

	w.plc.Status = Connecting
	w.plc.Mu.Unlock()

	// Back off before retrying.
	backoff := time.Duration(*attempts) * 2 * time.Second
	select {
	case <-ctx.Done():
		return
	case <-time.After(backoff):
	}

	drv, err := driver.Create(&w.plc.Config)
	if err != nil {
		w.plc.Mu.Lock()
		w.plc.Status = Error
		w.plc.LastError = err
		w.plc.Mu.Unlock()
		log.Printf("plcman: %s reconnect create driver: %v", plcName, err)
		return
	}

	if err := drv.Connect(); err != nil {
		w.plc.Mu.Lock()
		w.plc.Status = Error
		w.plc.LastError = err
		w.plc.Mu.Unlock()
		log.Printf("plcman: %s reconnect failed (%d/%d): %v", plcName, *attempts, maxReconnectAttempts, err)
		return
	}

	w.plc.Mu.Lock()
	w.plc.Driver = drv
	w.plc.Status = Connected
	w.plc.LastError = nil
	w.plc.Mu.Unlock()

	log.Printf("plcman: %s reconnected (attempt %d)", plcName, *attempts)
	*attempts = 0
}

// valuesEqual performs a simple equality check between two tag values.
func valuesEqual(a, b interface{}) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}
