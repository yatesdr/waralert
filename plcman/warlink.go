package plcman

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// warlinkPLCResponse matches the WarLink GET /api/ JSON response element.
type warlinkPLCResponse struct {
	Name        string `json:"name"`
	Address     string `json:"address"`
	Slot        byte   `json:"slot"`
	Status      string `json:"status"`
	ProductName string `json:"product_name,omitempty"`
	Error       string `json:"error,omitempty"`
}

// warlinkTagResponse matches a single tag value from WarLink GET /api/{plc}/tags.
type warlinkTagResponse struct {
	PLC   string      `json:"plc"`
	Name  string      `json:"name"`
	Type  string      `json:"type"`
	Value interface{} `json:"value"`
	Error string      `json:"error,omitempty"`
}

// WarLinkPoller polls a WarLink REST API to discover PLCs and read tag values.
type WarLinkPoller struct {
	baseURL  string
	pollRate time.Duration
	client   http.Client

	mu        sync.RWMutex
	connected bool
	lastError error
	plcNames  []string // PLCs discovered from this source

	manager    *Manager
	sourceName string

	cancel context.CancelFunc
	done   chan struct{}
}

// newWarLinkPoller creates a WarLinkPoller for the given source.
func newWarLinkPoller(manager *Manager, sourceName, baseURL string, pollRate time.Duration) *WarLinkPoller {
	if pollRate <= 0 {
		pollRate = 2 * time.Second
	}
	return &WarLinkPoller{
		baseURL:    strings.TrimRight(baseURL, "/"),
		pollRate:   pollRate,
		client:     http.Client{Timeout: 10 * time.Second},
		manager:    manager,
		sourceName: sourceName,
	}
}

// Start launches the polling goroutine.
func (w *WarLinkPoller) Start(ctx context.Context) {
	pollCtx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	w.done = make(chan struct{})
	go w.run(pollCtx)
}

// Stop cancels the poller and waits for it to finish.
func (w *WarLinkPoller) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
	if w.done != nil {
		<-w.done
	}
}

// IsConnected returns true if the last poll succeeded.
func (w *WarLinkPoller) IsConnected() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.connected
}

// LastError returns the last error encountered, or nil.
func (w *WarLinkPoller) LastError() error {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.lastError
}

// PLCNames returns the PLC names discovered by this poller.
func (w *WarLinkPoller) PLCNames() []string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]string, len(w.plcNames))
	copy(out, w.plcNames)
	return out
}

func (w *WarLinkPoller) run(ctx context.Context) {
	defer close(w.done)

	ticker := time.NewTicker(w.pollRate)
	defer ticker.Stop()

	// Poll immediately on start
	w.poll(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.poll(ctx)
		}
	}
}

func (w *WarLinkPoller) poll(ctx context.Context) {
	plcs, err := w.fetchPLCs(ctx)
	if err != nil {
		w.mu.Lock()
		w.connected = false
		w.lastError = err
		w.mu.Unlock()
		log.Printf("plcman: warlink %q: %v", w.sourceName, err)
		return
	}

	w.mu.Lock()
	w.connected = true
	w.lastError = nil
	w.mu.Unlock()

	var discoveredNames []string
	for _, plc := range plcs {
		discoveredNames = append(discoveredNames, plc.Name)

		// Ensure ManagedPLC exists in the manager's flat map
		w.manager.ensurePLC(plc.Name, plc.Status, plc.Error, w.sourceName)

		// Fetch tags for this PLC
		tags, err := w.fetchTags(ctx, plc.Name)
		if err != nil {
			log.Printf("plcman: warlink %q: tags for %q: %v", w.sourceName, plc.Name, err)
			continue
		}

		w.applyTags(plc.Name, tags)
	}

	w.mu.Lock()
	w.plcNames = discoveredNames
	w.mu.Unlock()
}

func (w *WarLinkPoller) fetchPLCs(ctx context.Context) ([]warlinkPLCResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", w.baseURL+"/", nil)
	if err != nil {
		return nil, err
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var plcs []warlinkPLCResponse
	if err := json.NewDecoder(resp.Body).Decode(&plcs); err != nil {
		return nil, fmt.Errorf("decode PLCs: %w", err)
	}
	return plcs, nil
}

func (w *WarLinkPoller) fetchTags(ctx context.Context, plcName string) (map[string]warlinkTagResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", w.baseURL+"/"+plcName+"/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var tags map[string]warlinkTagResponse
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, fmt.Errorf("decode tags: %w", err)
	}
	return tags, nil
}

func (w *WarLinkPoller) applyTags(plcName string, tags map[string]warlinkTagResponse) {
	mp := w.manager.GetPLC(plcName)
	if mp == nil {
		return
	}

	mp.Mu.Lock()
	defer mp.Mu.Unlock()

	for key, tr := range tags {
		// WarLink keys are "PLCName.TagName" — strip the prefix
		tagName := tr.Name
		if tagName == "" {
			// Fallback: strip "PLCName." prefix from map key
			if idx := strings.Index(key, "."); idx >= 0 {
				tagName = key[idx+1:]
			} else {
				tagName = key
			}
		}

		tv := TagValue{
			Name:    tagName,
			TypeStr: tr.Type,
			Value:   tr.Value,
		}
		if tr.Error != "" {
			tv.Error = fmt.Errorf("%s", tr.Error)
		}

		prev, existed := mp.Values[tagName]
		mp.Values[tagName] = tv

		changed := !existed || !valuesEqual(prev.Value, tv.Value)
		if changed {
			w.manager.mu.RLock()
			cb := w.manager.onValueChange
			w.manager.mu.RUnlock()
			if cb != nil {
				cb(plcName, tagName, tv)
			}
		}
	}
}

// parseWarlinkStatus maps WarLink status strings to ConnectionStatus.
func parseWarlinkStatus(s string) ConnectionStatus {
	switch s {
	case "Connected":
		return Connected
	case "Connecting":
		return Connecting
	case "Disconnected":
		return Disconnected
	default:
		return Error
	}
}
