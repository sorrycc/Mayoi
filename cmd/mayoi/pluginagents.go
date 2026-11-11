package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"mayoi/internal/agents"
	"mayoi/internal/plugin"
)

// The agents plugins add, as plugin.json's contributes.agents declares
// them, join those Mayoi knows in internal/agents: the Sessions view lists
// their sessions, marks them with their icons and resumes them alike. The
// sessions of one are of the files a glob matches, or of what a function
// of the plugin returns, which the scanner runs off the main thread and
// waits for no longer than agents.ListWait. A plugin turned off takes its
// agents with it, and its sessions go as a reading drops them.

// syncAgents has the agents be those Mayoi knows and those of the plugins
// on, once those change, and has the Sessions view read again; on the
// main thread.
func (h *pluginHost) syncAgents() {
	var sig strings.Builder
	var more []agents.Agent
	for _, p := range h.enabled() {
		for _, c := range p.Contributes.Agents {
			data, _ := json.Marshal(c)
			fmt.Fprintf(&sig, "%s\x00%s\x00%s\n", p.Name, p.Dir, data)
			more = append(more, pluginAgent(p, c))
		}
	}
	if sig.String() == h.agentsSig {
		return
	}
	h.agentsSig = sig.String()
	agents.Use(more)
	agentScanner.OnLate(func() { h.onMain(h.readSessionsAgain) })
	h.readSessionsAgain()
}

// readSessionsAgain has each window read its sessions again now.
func (h *pluginHost) readSessionsAgain() {
	for _, a := range h.apps() {
		a.readSessionsNow()
	}
}

// pluginAgent is the agent c of plugin p declares.
func pluginAgent(p *plugin.Plugin, c plugin.AgentContrib) agents.Agent {
	ag := agents.Agent{
		ID: agents.Harness(c.ID), Name: c.Name, App: cmp.Or(c.App, c.ID),
		Icon: c.Icon, Tint: c.Tint, Resume: agents.ResumeTemplate(c.Resume),
	}
	if f := c.Sessions; f != nil {
		return agents.WithPattern(ag, agents.Pattern{Glob: f.Glob, Format: f.Format,
			ID: f.ID, Title: f.Title, Folder: f.Folder, Updated: f.Updated, Prompt: f.Prompt})
	}
	l := &agentLog{p: p, name: c.Name}
	ag.List = func(ctx context.Context) ([]agents.Session, error) {
		ss, err := listPluginSessions(ctx, p, c.ID)
		l.note(err)
		return ss, err
	}
	return ag
}

// agentLog logs in the plugin's log why listing an agent's sessions
// failed, once until it fails otherwise or works again, rather than at
// every reading.
type agentLog struct {
	p    *plugin.Plugin
	name string
	mu   sync.Mutex
	last string
}

func (l *agentLog) note(err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
		if errors.Is(err, context.DeadlineExceeded) {
			msg = fmt.Sprintf("it took longer than %v", agents.ListLimit)
		}
	}
	l.mu.Lock()
	same := msg == l.last
	l.last = msg
	l.mu.Unlock()
	if !same && msg != "" {
		l.p.Log("error", fmt.Sprintf("could not list the sessions of %s: %s", l.name, msg))
	}
}

// pluginSession is a session as a plugin's function gives it.
type pluginSession struct {
	ID      json.RawMessage `json:"id"`
	Title   string          `json:"title"`
	Folder  string          `json:"folder"`
	Updated json.RawMessage `json:"updated"`
	Prompt  string          `json:"prompt"`
}

// listPluginSessions asks plugin p, starting it first, for the sessions
// of its agent id.
func listPluginSessions(ctx context.Context, p *plugin.Plugin, id string) ([]agents.Session, error) {
	res, err := p.Request(ctx, "agentSessions", map[string]any{"id": id})
	if err != nil {
		return nil, err
	}
	var got []pluginSession
	if err := json.Unmarshal(res, &got); err != nil {
		return nil, fmt.Errorf("its sessions are not a list of sessions: %v", err)
	}
	out := make([]agents.Session, 0, len(got))
	for _, s := range got {
		out = append(out, agents.SessionOf(s.ID, s.Title, s.Folder, s.Updated, s.Prompt))
	}
	return out, nil
}
