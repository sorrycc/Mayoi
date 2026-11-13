package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/plugins/terminal"

	"mayoi/internal/agents"
)

// With the terminal backend "hc", the Sessions view knows which sessions
// run in terminals of herdr-core, in this window or elsewhere, and what
// they do: hc ls says which agent each terminal runs and its state (OSC
// 7501, as hc read it), and hc get the session, as the agent reported it
// to hc, or else its process, which Claude Code names its session after
// in sessions/<pid>.json of its config folder. An agent whose session
// neither names, as Qoder and Codex, runs the newest session of its
// folder.

// hcAgent is an agent that runs in a terminal of herdr-core, or, term
// set, in a terminal here.
type hcAgent struct {
	addr, cwd string
	harness   agents.Harness
	state     terminal.StatusState
	term      *session
	// since is when the agent was first seen to run, which a session it
	// runs but nothing names was written after; zero for any.
	since time.Time
}

// agentKey names an agent that runs among the terminals, over readings.
func (ag hcAgent) agentKey() string {
	if ag.term != nil {
		return fmt.Sprintf("term:%p:%s", ag.term, ag.harness)
	}
	return "hc:" + ag.addr + ":" + string(ag.harness)
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
// this machine, by liveKey, Claude Code's of the config folders roots,
// and loose, the agents that run there in sessions hc does not name.
// It runs hc, and takes a while.
func hcAgents(roots []string) (running map[string]hcAgent, loose []hcAgent) {
	out, err := hcRun("ls", "--json")
	if err != nil {
		return nil, nil
	}
	var ls struct{ Terminals []hcTerminal }
	if json.Unmarshal(out, &ls) != nil {
		return nil, nil
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	running = map[string]hcAgent{}
	at := map[string]int{}
	for i, t := range ls.Terminals {
		at[t.Address] = i
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
			state := statusState(t.Status)
			if s := statusState(g.Status); s != "" {
				state = s
			}
			ag := hcAgent{addr: t.Address, cwd: t.Cwd, harness: h, state: hcState(state)}
			id, ok := g.sessionID(h, roots)
			mu.Lock()
			if ok {
				running[liveKey(h, id)] = ag
			} else {
				loose = append(loose, ag)
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	// In the order hc lists them, whatever order hc get answered in.
	slices.SortFunc(loose, func(x, y hcAgent) int { return at[x.addr] - at[y.addr] })
	return running, loose
}

// placeLoose adds to running each agent of loose, whose session nothing
// names, as running the session it most likely does: the newest of ss of
// its agent and its folder that none of running does, and that was written
// since the agent began: a new session writes nothing until its first
// prompt. It returns running, made anew.
func placeLoose(running map[string]hcAgent, loose []hcAgent, ss []agents.Session) map[string]hcAgent {
	out := make(map[string]hcAgent, len(running)+len(loose))
	maps.Copy(out, running)
	for _, ag := range loose {
		if ag.cwd == "" {
			continue
		}
		dir := filepath.Clean(ag.cwd)
		var best *agents.Session
		for i := range ss {
			s := &ss[i]
			if s.Harness != ag.harness || s.Dir == "" || filepath.Clean(s.Dir) != dir {
				continue
			}
			if _, taken := out[liveKey(s.Harness, s.ID)]; taken || s.Updated.Before(ag.since) {
				continue
			}
			if best == nil || s.Updated.After(best.Updated) {
				best = s
			}
		}
		if best != nil {
			out[liveKey(best.Harness, best.ID)] = ag
		}
	}
	return out
}
