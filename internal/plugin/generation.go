package plugin

import (
	"context"
	"encoding/json"
	"errors"
)

// Generation identifies a runtime, including its activation, until it stops.
func (p *Plugin) Generation() uint64 { return p.generation.Load() }

// RequestGeneration invokes the captured runtime without restarting a stopped plugin.
func (p *Plugin) RequestGeneration(ctx context.Context, generation uint64, kind string, payload any) (json.RawMessage, error) {
	for {
		p.mu.Lock()
		in, starting := p.in, p.starting
		valid := generation != 0 && generation == p.generation.Load()
		p.mu.Unlock()
		if !valid || in == nil && starting == nil {
			return nil, errors.New("plugin action expired")
		}
		if in != nil {
			return in.request(ctx, kind, payload)
		}
		// Activation can publish actions before the runtime is ready for commands.
		select {
		case <-starting:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
