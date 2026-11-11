package main

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/egoist/mygo/plugins/terminal"

	"mayoi/internal/agents"
)

// With the terminal backend "hc", the Sessions view knows which sessions
// run in terminals of herdr-core, in this window or elsewhere, and what
// they do: hc ls says which agent each terminal runs and its state (OSC
// 7501, as hc read it), and hc get the session, as the agent reported it
// to hc, or else its process, which Claude Code names its session after
// in sessions/<pid>.json of its config folder.

// hcAgent is an agent that runs in a terminal of herdr-core.
type hcAgent struct {
	addr, cwd string
	harness   agents.Harness
	state     terminal.StatusState
}

// liveKey names a session among those that run: an agent's ids are its
// own, whatever config folder it is of.
func liveKey(h agents.Harness, id string) string { return string(h) + "\x00" + id }

// hcState is the state hc says a terminal's program is in.
func hcState(s string) terminal.StatusState {
	switch s {
	case "working":
		return terminal.StatusWorking
	case "blocked":
		return terminal.StatusBlocked
	case "done":
		return terminal.StatusDone
	case "error", "failed":
		return terminal.StatusError
	}
	return terminal.StatusIdle
}

// hcTerminal is what hc ls says of a terminal.
type hcTerminal struct {
	Address, Agent, Cwd, State string
	Status                     json.RawMessage
}

// statusState is the state of its status: a word in hc ls, an object of
// the app and its state in hc get.
func statusState(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var o struct{ State string }
	json.Unmarshal(raw, &o)
	return o.State
}

// hcGot is what hc get says of the session that runs in a terminal:
// the agent's report of it, and the program in front, whose process
// Claude Code names its session after.
type hcGot struct {
	AgentSession *struct {
		Agent, ID string
	} `json:"agent_session"`
	AgentSessionID string `json:"agent_session_id"`
	Foreground     *struct {
		PID int
	}
	Status json.RawMessage
}

// sessionID is the session hc get says runs, and whether it says.
func (g hcGot) sessionID(h agents.Harness, roots []string) (string, bool) {
	switch {
	case g.AgentSession != nil && g.AgentSession.ID != "":
		return g.AgentSession.ID, true
	case g.AgentSessionID != "":
		return g.AgentSessionID, true
	case h == agents.Claude && g.Foreground != nil:
		id, _, ok := agents.ClaudeSessionOfPID(roots, g.Foreground.PID)
		return id, ok
	}
	return "", false
}

// hcAgents are the sessions that run in the terminals of herdr-core of
// this machine, by liveKey, Claude Code's of the config folders roots.
// It runs hc, and takes a while.
func hcAgents(roots []string) map[string]hcAgent {
	out, err := hcRun("ls", "--json")
	if err != nil {
		return nil
	}
	var ls struct{ Terminals []hcTerminal }
	if json.Unmarshal(out, &ls) != nil {
		return nil
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	running := map[string]hcAgent{}
	for _, t := range ls.Terminals {
		h, ok := agents.HarnessOfApp(t.Agent)
		// Those of other machines run sessions of theirs.
		if !ok || t.State != "live" || strings.Contains(t.Address, "/") {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := hcRun("get", "--json", t.Address)
			if err != nil {
				return
			}
			var g hcGot
			if json.Unmarshal(out, &g) != nil {
				return
			}
			id, ok := g.sessionID(h, roots)
			if !ok {
				return
			}
			state := statusState(t.Status)
			if s := statusState(g.Status); s != "" {
				state = s
			}
			mu.Lock()
			running[liveKey(h, id)] = hcAgent{addr: t.Address, cwd: t.Cwd, harness: h, state: hcState(state)}
			mu.Unlock()
		}()
	}
	wg.Wait()
	return running
}
