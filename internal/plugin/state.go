package plugin

// State is where a plugin is in its life, one of these at a time. The
// settings turn it off and on; the first call of its code starts it, and
// maxCrashes stops in a row keep it stopped until it is reloaded.
type State int

const (
	// StateIdle: it has code, which the first call of it starts.
	StateIdle State = iota
	// StateStarting: its code is being started.
	StateStarting
	// StateRunning: its code runs.
	StateRunning
	// StateOff: its settings, or safe mode, turn it off.
	StateOff
	// StateCrashed: it stopped too often, and is off until reloaded.
	StateCrashed
	// StateBroken: it could not be read, as Err says: it cannot start.
	StateBroken
	// StateStatic: it only declares, as a theme does, and has no code.
	StateStatic
)

func (s State) String() string {
	switch s {
	case StateStarting:
		return "starting"
	case StateRunning:
		return "running"
	case StateOff:
		return "off"
	case StateCrashed:
		return "crashed"
	case StateBroken:
		return "broken"
	case StateStatic:
		return "static"
	}
	return "idle"
}

// state works out the plugin's state; p.mu is held. Code that runs, or
// starts, says so first, as it does until it is stopped; of the reasons
// it does not run, the person's own choice comes first.
func (p *Plugin) state(enabled bool) State {
	switch {
	case p.in != nil:
		return StateRunning
	case p.starting != nil:
		return StateStarting
	case !enabled:
		return StateOff
	case p.crashes >= maxCrashes:
		return StateCrashed
	case p.loadErr != "":
		return StateBroken
	case !p.HasCode() && !p.Script:
		return StateStatic
	}
	return StateIdle
}
