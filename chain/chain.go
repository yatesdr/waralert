package chain

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"waralert/config"
)

// TagReader is an alias for config.TagReader.
type TagReader = config.TagReader

// ActionExecutor executes an action block.
type ActionExecutor interface {
	Execute(block config.BlockConfig, tagReader config.TagReader) error
}

// Status represents the current state of a chain.
type Status int

const (
	Disabled     Status = iota // Chain is not running
	Armed                      // Waiting for trigger condition (rising edge)
	Firing                     // Walking blocks and executing actions
	WaitingClear               // Waiting for first gate to go false after firing
	Cooldown                   // Cooldown period before re-arming
	Error                      // Chain encountered an error
)

// String returns the human-readable name of the status.
func (s Status) String() string {
	switch s {
	case Disabled:
		return "disabled"
	case Armed:
		return "armed"
	case Firing:
		return "firing"
	case WaitingClear:
		return "waiting_clear"
	case Cooldown:
		return "cooldown"
	case Error:
		return "error"
	default:
		return "unknown"
	}
}

// Chain is the runtime for a single alert chain. It monitors its first gate for
// a rising edge, then walks all blocks sequentially (evaluating gates, firing actions).
type Chain struct {
	config config.ChainConfig

	tagReader      TagReader
	actionExecutor ActionExecutor
	logFn          func(string, ...interface{})

	mu                  sync.RWMutex
	status              Status
	lastAggregateResult bool // previous first-gate result for edge detection
	fireCount           int
	lastFire            time.Time
	lastError           string
	cooldownUntil       time.Time
	gateTimers          map[int]time.Time // block index → time conditions first became continuously true

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewChain creates a new Chain from configuration. It starts in Disabled status.
func NewChain(
	cfg config.ChainConfig,
	tagReader TagReader,
	actionExecutor ActionExecutor,
	logFn func(string, ...interface{}),
) *Chain {
	if logFn == nil {
		logFn = func(string, ...interface{}) {}
	}
	return &Chain{
		config:         cfg,
		tagReader:      tagReader,
		actionExecutor: actionExecutor,
		logFn:          logFn,
		status:         Disabled,
		gateTimers:     make(map[int]time.Time),
	}
}

// Start begins the chain monitor loop. If the chain config is not enabled, it
// remains in Disabled status. Calling Start on an already-running chain is a no-op.
func (c *Chain) Start() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ctx != nil {
		return // already running
	}
	if !c.config.Enabled {
		c.status = Disabled
		return
	}

	c.status = Armed
	c.lastAggregateResult = false
	c.ctx, c.cancel = context.WithCancel(context.Background())

	c.wg.Add(1)
	go c.monitorLoop()
}

// Stop halts the chain monitor loop and sets status to Disabled.
func (c *Chain) Stop() {
	c.mu.Lock()
	cancelFn := c.cancel
	c.mu.Unlock()

	if cancelFn != nil {
		cancelFn()
	}
	c.wg.Wait()

	c.mu.Lock()
	c.ctx = nil
	c.cancel = nil
	c.status = Disabled
	c.mu.Unlock()
}

// Reset clears fire count, last fire time, error state, and re-arms the chain.
func (c *Chain) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.fireCount = 0
	c.lastFire = time.Time{}
	c.lastError = ""
	c.lastAggregateResult = false
	c.cooldownUntil = time.Time{}
	c.gateTimers = make(map[int]time.Time)
	if c.ctx != nil {
		c.status = Armed
	}
}

// TestFireResult holds the outcome of a manual test fire.
type TestFireResult struct {
	BlocksRun    int      `json:"blocks_run"`
	ActionsFired int      `json:"actions_fired"`
	GatesPassed  int      `json:"gates_passed"`
	GatesFailed  int      `json:"gates_failed"`
	Errors       []string `json:"errors,omitempty"`
}

// TestFire manually triggers a chain walk regardless of gate state.
// The chain must be running (not Disabled).
func (c *Chain) TestFire() (*TestFireResult, error) {
	c.mu.Lock()
	if c.ctx == nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("chain %q is not running", c.config.Name)
	}
	c.mu.Unlock()

	c.logFn("chain %q: test fire triggered", c.config.Name)
	result := c.walkBlocksForTest()

	c.mu.Lock()
	c.fireCount++
	c.lastFire = time.Now()
	c.mu.Unlock()

	return result, nil
}

// walkBlocksForTest walks all blocks and returns structured results.
func (c *Chain) walkBlocksForTest() *TestFireResult {
	result := &TestFireResult{}

	for i, block := range c.config.Blocks {
		select {
		case <-c.ctx.Done():
			return result
		default:
		}

		result.BlocksRun++

		switch block.Type {
		case "gate":
			gate := NewGate(block)
			gateResult, err := gate.Evaluate(c.tagReader)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("gate %q: %v", block.Name, err))
				return result
			}
			if !gateResult {
				result.GatesFailed++
				result.Errors = append(result.Errors, fmt.Sprintf("gate %q evaluated false, stopped", block.Name))
				return result
			}
			result.GatesPassed++

		case "action":
			if c.actionExecutor != nil {
				// Prepend test prefix to all outgoing content.
				const testPrefix = "[TEST ONLY - INITIATED FROM WEBUI] "
				if block.Message != "" {
					block.Message = testPrefix + block.Message
				}
				if block.Subject != "" {
					block.Subject = testPrefix + block.Subject
				}
				if block.Body != "" {
					block.Body = testPrefix + block.Body
				}
				if err := c.actionExecutor.Execute(block, c.tagReader); err != nil {
					c.logFn("chain %q: block %d action %q error: %v", c.config.Name, i, block.Name, err)
					result.Errors = append(result.Errors, fmt.Sprintf("action %q: %v", block.Name, err))
					c.mu.Lock()
					c.lastError = fmt.Sprintf("block %d action %q: %v", i, block.Name, err)
					c.mu.Unlock()
				}
				result.ActionsFired++
			}
		}
	}

	return result
}

// GetStatus returns the current chain status.
func (c *Chain) GetStatus() Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

// ChainStats holds runtime statistics for a chain.
type ChainStats struct {
	Status    Status
	FireCount int
	LastFire  time.Time
	LastError string
}

// GetStats returns a snapshot of the chain's runtime statistics.
func (c *Chain) GetStats() ChainStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return ChainStats{
		Status:    c.status,
		FireCount: c.fireCount,
		LastFire:  c.lastFire,
		LastError: c.lastError,
	}
}

// GateProgress holds a snapshot of gate evaluation state for display.
type GateProgress struct {
	Passing      int
	Total        int
	RemainingSec int // >0 if first gate conditions are true but duration timer is running; -1 otherwise
}

// GetGateProgress evaluates all gate blocks against live tags and returns
// how many are currently passing, plus the first gate's duration timer state.
func (c *Chain) GetGateProgress() GateProgress {
	c.mu.RLock()
	cfg := c.config
	gateTimers := c.gateTimers
	c.mu.RUnlock()

	var passing, total int
	remainingSec := -1

	for i, block := range cfg.Blocks {
		if block.Type != "gate" {
			continue
		}
		total++
		gate := NewGate(block)
		result, err := gate.Evaluate(c.tagReader)
		if err != nil || !result {
			continue
		}
		// Gate conditions are true right now
		if block.DurationMin > 0 {
			// Check if the runtime timer is tracking this gate
			started, exists := gateTimers[i]
			if exists {
				elapsed := time.Since(started)
				dur := time.Duration(block.DurationMin) * time.Minute
				if elapsed >= dur {
					passing++
				} else {
					// Timer still running — report remaining for the first gate only
					if remainingSec < 0 {
						remainingSec = int(math.Ceil((dur - elapsed).Seconds()))
					}
				}
			}
			// If no timer entry exists, the chain's monitor loop hasn't started tracking yet
		} else {
			passing++
		}
	}

	return GateProgress{Passing: passing, Total: total, RemainingSec: remainingSec}
}

// GetConfig returns a copy of the chain's configuration.
func (c *Chain) GetConfig() config.ChainConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.config
}

// monitorLoop runs on a 100ms ticker and dispatches to the appropriate state handler.
func (c *Chain) monitorLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.checkChain()
		}
	}
}

// checkChain dispatches to the handler for the current state.
func (c *Chain) checkChain() {
	c.mu.RLock()
	status := c.status
	c.mu.RUnlock()

	switch status {
	case Armed:
		c.checkArmed()
	case WaitingClear:
		c.checkWaitingClear()
	case Cooldown:
		c.checkCooldown()
	}
}

// checkArmed evaluates the first gate and detects a rising edge to trigger firing.
func (c *Chain) checkArmed() {
	firstGate, firstGateIdx := c.findFirstGate()
	if firstGate == nil {
		c.mu.Lock()
		c.status = Error
		c.lastError = "no gate block found in chain"
		c.mu.Unlock()
		return
	}

	result, err := c.evaluateGateWithDuration(firstGateIdx, firstGate)
	if err != nil {
		c.mu.Lock()
		c.status = Error
		c.lastError = fmt.Sprintf("first gate evaluation error: %v", err)
		c.mu.Unlock()
		return
	}

	c.mu.Lock()
	prev := c.lastAggregateResult
	c.lastAggregateResult = result
	c.mu.Unlock()

	// Rising edge: false -> true
	if result && !prev {
		// Debounce: if configured, wait and re-check
		if c.config.DebounceSec > 0 {
			select {
			case <-time.After(time.Duration(c.config.DebounceSec) * time.Second):
			case <-c.ctx.Done():
				return
			}
			// Re-evaluate after debounce period
			recheck, err := c.evaluateGateWithDuration(firstGateIdx, firstGate)
			if err != nil || !recheck {
				// Debounce failed: condition cleared during debounce
				c.mu.Lock()
				c.lastAggregateResult = false
				c.mu.Unlock()
				return
			}
		}

		c.logFn("chain %q: rising edge detected, firing", c.config.Name)

		c.mu.Lock()
		c.status = Firing
		c.mu.Unlock()

		c.walkBlocks()

		c.mu.Lock()
		c.fireCount++
		c.lastFire = time.Now()
		c.status = WaitingClear
		c.mu.Unlock()
	}
}

// checkWaitingClear waits for the first gate to go false before entering cooldown.
func (c *Chain) checkWaitingClear() {
	firstGate, _ := c.findFirstGate()
	if firstGate == nil {
		return
	}

	result, err := firstGate.Evaluate(c.tagReader)
	if err != nil {
		c.mu.Lock()
		c.status = Error
		c.lastError = fmt.Sprintf("waiting_clear gate evaluation error: %v", err)
		c.mu.Unlock()
		return
	}

	c.mu.Lock()
	c.lastAggregateResult = result
	c.mu.Unlock()

	if !result {
		c.mu.Lock()
		if c.config.CooldownSec > 0 {
			c.cooldownUntil = time.Now().Add(time.Duration(c.config.CooldownSec) * time.Second)
			c.status = Cooldown
			c.logFn("chain %q: cleared, entering cooldown for %ds", c.config.Name, c.config.CooldownSec)
		} else {
			c.status = Armed
			c.logFn("chain %q: cleared, re-armed", c.config.Name)
		}
		c.mu.Unlock()
	}
}

// checkCooldown checks if the cooldown period has elapsed and re-arms.
func (c *Chain) checkCooldown() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if time.Now().After(c.cooldownUntil) {
		c.status = Armed
		c.lastAggregateResult = false
		c.logFn("chain %q: cooldown complete, re-armed", c.config.Name)
	}
}

// walkBlocks sequentially walks all blocks in the chain config.
// For gate blocks: evaluate and stop if false.
// For action blocks: execute via ActionExecutor.
func (c *Chain) walkBlocks() {
	for i, block := range c.config.Blocks {
		// Check for cancellation between blocks
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		switch block.Type {
		case "gate":
			gate := NewGate(block)
			result, err := gate.Evaluate(c.tagReader)
			if err != nil {
				c.logFn("chain %q: block %d gate %q error: %v", c.config.Name, i, block.Name, err)
				c.mu.Lock()
				c.lastError = fmt.Sprintf("block %d gate %q: %v", i, block.Name, err)
				c.mu.Unlock()
				return
			}
			if !result {
				c.logFn("chain %q: block %d gate %q evaluated false, stopping walk", c.config.Name, i, block.Name)
				return
			}

		case "action":
			if c.actionExecutor != nil {
				if err := c.actionExecutor.Execute(block, c.tagReader); err != nil {
					c.logFn("chain %q: block %d action %q error: %v", c.config.Name, i, block.Name, err)
					c.mu.Lock()
					c.lastError = fmt.Sprintf("block %d action %q: %v", i, block.Name, err)
					c.mu.Unlock()
					// Continue walking remaining blocks despite action error
				}
			}

		default:
			c.logFn("chain %q: block %d unknown type %q, skipping", c.config.Name, i, block.Type)
		}
	}
}

// findFirstGate locates the first gate block in the chain configuration.
func (c *Chain) findFirstGate() (*Gate, int) {
	for i, block := range c.config.Blocks {
		if block.Type == "gate" {
			return NewGate(block), i
		}
	}
	return nil, -1
}

// evaluateGateWithDuration evaluates a gate with optional duration (TON timer).
// If the gate's DurationMin is set, the gate only returns true after its conditions
// have been continuously true for at least that duration.
func (c *Chain) evaluateGateWithDuration(blockIndex int, gate *Gate) (bool, error) {
	result, err := gate.Evaluate(c.tagReader)
	if err != nil {
		delete(c.gateTimers, blockIndex)
		return false, err
	}

	durationMin := c.config.Blocks[blockIndex].DurationMin
	if durationMin <= 0 {
		// No duration configured — pass through raw result.
		if !result {
			delete(c.gateTimers, blockIndex)
		}
		return result, nil
	}

	if !result {
		// Conditions went false — reset timer.
		delete(c.gateTimers, blockIndex)
		return false, nil
	}

	// Conditions are true with a duration requirement.
	started, exists := c.gateTimers[blockIndex]
	if !exists {
		c.gateTimers[blockIndex] = time.Now()
		return false, nil // just started, not yet elapsed
	}

	if time.Since(started) >= time.Duration(durationMin)*time.Minute {
		return true, nil // duration elapsed — gate passes
	}
	return false, nil // still waiting
}
