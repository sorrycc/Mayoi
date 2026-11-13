package main

import (
	"os"

	"mayoi/internal/bg"
)

type quickPathKey struct {
	root, query string
	epoch       uint64
	open        bool
}

type quickPathResult struct {
	name string
	at   [2]int
	err  error
}

type quickPathProbe struct {
	key     quickPathKey
	epoch   uint64
	name    string
	pending bool
	read    bg.Refresher[quickPathKey]
	result  quickPathResult
	stat    func(string) (os.FileInfo, error) // nil uses os.Stat
}

func (a *app) quickPathKey() quickPathKey {
	return quickPathKey{a.root, a.quick.query, a.quick.probe.epoch, a.quick.open}
}

// There is at most one Stat worker per app. A blocked system call cannot
// be cancelled; further edits replace the desired key without spawning
// workers or retaining a queue of paths. Only the newest key runs next.
func (a *app) refreshQuickPath() {
	p := &a.quick.probe
	key := a.quickPathKey()
	if p.key != key {
		p.epoch++
		key = a.quickPathKey()
		p.key, p.result = key, quickPathResult{}
		a.quick.path, a.quick.pathAt = "", [2]int{}
		p.name = ""
		if key.open {
			p.name = quickPathName(key.root, key.query)
		}
		p.pending = p.name != ""
		p.read.Invalidate()
		a.quick.stale = true
	}
	if !p.pending || p.read.Running() || !p.read.Start(key) {
		return
	}
	name, stat := p.name, p.stat
	if stat == nil {
		stat = os.Stat
	}
	bg.Go(func() {
		defer recoverCrash()
		result := probeQuickPath(name, stat)
		a.post(func() {
			if p.read.Done(key) && a.quickPathKey() == key && p.key == key {
				p.result, p.pending = result, false
				a.quick.stale = true
			}
		})
	})
}

func probeQuickPath(name string, stat func(string) (os.FileInfo, error)) quickPathResult {
	if name == "" {
		return quickPathResult{}
	}
	file := func(path string) (bool, error) {
		info, err := stat(path)
		if os.IsNotExist(err) {
			err = nil
		}
		return info != nil && info.Mode().IsRegular(), err
	}
	if ok, err := file(name); ok {
		return quickPathResult{name: name}
	} else if rest, line, col, suffix := cutLineSuffix(name); suffix {
		if ok, err := file(rest); ok {
			return quickPathResult{name: rest, at: [2]int{line, col}}
		} else {
			return quickPathResult{err: err}
		}
	} else {
		return quickPathResult{err: err}
	}
}
