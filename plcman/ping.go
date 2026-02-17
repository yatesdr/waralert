package plcman

import (
	"context"
	"fmt"
	"log"
	"net"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"waralert/config"
)

// PingPoller monitors a host via ICMP ping or TCP port check.
type PingPoller struct {
	host       string
	port       int // 0 = ICMP, >0 = TCP
	interval   time.Duration
	manager    *Manager
	sourceName string

	mu        sync.RWMutex
	connected bool
	lastError error

	cancel context.CancelFunc
	done   chan struct{}
}

// newPingPoller creates a PingPoller for the given source.
func newPingPoller(manager *Manager, sourceName, host string, port int, interval time.Duration) *PingPoller {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &PingPoller{
		host:       host,
		port:       port,
		interval:   interval,
		manager:    manager,
		sourceName: sourceName,
	}
}

// Start launches the polling goroutine.
func (p *PingPoller) Start(ctx context.Context) {
	pollCtx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	p.done = make(chan struct{})
	go p.run(pollCtx)
}

// Stop cancels the poller and waits for it to finish.
func (p *PingPoller) Stop() {
	if p.cancel != nil {
		p.cancel()
	}
	if p.done != nil {
		<-p.done
	}
}

// IsConnected returns true if the last check succeeded (host is up).
func (p *PingPoller) IsConnected() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.connected
}

// LastError returns the last error encountered, or nil.
func (p *PingPoller) LastError() error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.lastError
}

func (p *PingPoller) run(ctx context.Context) {
	defer close(p.done)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	// Poll immediately on start
	p.poll(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.poll(ctx)
		}
	}
}

func (p *PingPoller) poll(ctx context.Context) {
	var up bool
	var err error

	if p.port > 0 {
		up, err = p.tcpCheck(ctx)
	} else {
		up, err = p.icmpCheck(ctx)
	}

	p.mu.Lock()
	prevConnected := p.connected
	p.connected = up
	p.lastError = err
	p.mu.Unlock()

	// Update ManagedPLC entry
	statusStr := "Disconnected"
	errStr := ""
	if up {
		statusStr = "Connected"
	} else if err != nil {
		statusStr = "Error"
		errStr = err.Error()
	}
	p.manager.ensurePLC(p.sourceName, statusStr, errStr, p.sourceName)

	// Update synthetic tag value
	mp := p.manager.GetPLC(p.sourceName)
	if mp != nil {
		mp.Mu.Lock()
		tv := TagValue{
			Name:    "online",
			TypeStr: "BOOL",
			Value:   up,
		}
		prev, existed := mp.Values["online"]
		mp.Values["online"] = tv
		mp.Mu.Unlock()

		changed := !existed || !valuesEqual(prev.Value, tv.Value)
		if changed {
			p.manager.mu.RLock()
			cb := p.manager.onValueChange
			p.manager.mu.RUnlock()
			if cb != nil {
				cb(p.sourceName, "online", tv)
			}
		}
	}

	if up != prevConnected {
		state := "UP"
		if !up {
			state = "DOWN"
		}
		log.Printf("plcman: ping %q: %s", p.sourceName, state)
	}
}

func (p *PingPoller) tcpCheck(ctx context.Context) (bool, error) {
	addr := fmt.Sprintf("%s:%d", p.host, p.port)
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return false, err
	}
	conn.Close()
	return true, nil
}

func (p *PingPoller) icmpCheck(ctx context.Context) (bool, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(timeoutCtx, "ping", "-n", "1", "-w", "2000", p.host)
	} else {
		cmd = exec.CommandContext(timeoutCtx, "ping", "-c", "1", "-W", "2", p.host)
	}

	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("ping %s: %w", p.host, err)
	}
	return true, nil
}

// ensurePingPLC creates the initial ManagedPLC entry for a ping source.
func ensurePingPLC(manager *Manager, sourceName string) {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	if _, exists := manager.plcs[sourceName]; !exists {
		manager.plcs[sourceName] = &ManagedPLC{
			Config: config.PLCConfig{Name: sourceName},
			Values: make(map[string]TagValue),
			Status: Disconnected,
		}
	}
	manager.plcSources[sourceName] = sourceName
}
