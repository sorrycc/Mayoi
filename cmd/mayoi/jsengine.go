package main

import (
	"errors"
	"strings"
	"time"

	"github.com/MoeForge/moejs"
)

// jsEngine runs a script of the app's own, Pele or MathJax, in a moejs
// runtime of its own, whose functions the app then calls one at a time.
// Their bundles change the builtins, as polyfills do, so the runtime has
// its own, writable.
type jsEngine struct {
	rt *moejs.Runtime
}

// newJSEngine runs source, with globals set first: host functions as
// moejs.NativeFunc.
func newJSEngine(name, source string, globals map[string]any) (*jsEngine, error) {
	s, err := moejs.CompileScript(name, source)
	if err != nil {
		return nil, err
	}
	rt := moejs.NewRuntime(moejs.Options{MutableIntrinsics: true, DisableDynamicCode: true})
	for k, v := range globals {
		if err := rt.SetGlobal(k, v); err != nil {
			return nil, err
		}
	}
	if _, err := rt.RunScript(s); err != nil {
		return nil, err
	}
	return &jsEngine{rt: rt}, nil
}

// eval runs a script that ends with an expression, and returns it.
func (e *jsEngine) eval(name, source string) (moejs.Value, error) {
	s, err := moejs.CompileScript(name, source)
	if err != nil {
		return moejs.Undefined(), err
	}
	return e.rt.RunScript(s)
}

// errJSTimeout is the interrupt of a call that took too long.
var errJSTimeout = errors.New("timeout")

// callString calls fn with args, stopping it after timeout, and returns
// the string it returns.
func (e *jsEngine) callString(fn moejs.Value, timeout time.Duration, args ...any) (string, error) {
	vals := make([]moejs.Value, len(args))
	for i, a := range args {
		v, err := e.rt.FromGo(a)
		if err != nil {
			return "", err
		}
		vals[i] = v
	}
	stop := time.AfterFunc(timeout, func() { e.rt.Interrupt(errJSTimeout) })
	v, err := e.rt.CallFunction(fn, moejs.Undefined(), vals...)
	stop.Stop()
	e.rt.ClearInterrupt()
	// What the call made, which the runtime would keep until the next.
	e.rt.ReleaseCallData()
	if err != nil {
		return "", err
	}
	out, err := e.rt.ToGo(v)
	if err != nil {
		return "", err
	}
	s, _ := out.(string)
	return s, nil
}

// timedOut reports whether err is a call's timeout.
func timedOut(err error) bool {
	var ie *moejs.InterruptedError
	return errors.As(err, &ie)
}

// thrown is what a script threw, as its message and the snippet of the
// source it gives, if any; the error itself otherwise.
func (e *jsEngine) thrown(err error) (msg, snippet string) {
	v, ok := e.rt.ThrownValue(err)
	if !ok || !v.IsObject() {
		return err.Error(), ""
	}
	str := func(key string) string {
		f, err := e.rt.Get(v, key)
		if err != nil || f.IsUndefined() {
			return ""
		}
		out, _ := e.rt.ToGo(f)
		s, _ := out.(string)
		return s
	}
	msg = str("message")
	if msg == "" {
		msg = err.Error()
	}
	return msg, str("snippet")
}

// oneLine is s with its runs of spaces and newlines as one space.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
