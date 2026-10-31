package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// The tests of cmd/mayoi spend most of their time waiting, for the git
// and the shells they start and for their background work, one after
// another. splitTests runs them in worker processes of this binary beside
// each other instead: each process has its own globals (the system the
// keys are for, the language, the environment), so the tests need not be
// written to run beside each other. The workers take the tests from one
// queue, the slowest the last runs measured first, a few at a time, so
// that none is left with the slow ones at the end.
//
// MAYOI_TEST_JOBS=<n> sets the number of workers (default: the cores), 1
// runs the tests in this process. MAYOI_TEST_SLOWEST=<n> lists the n
// slowest tests at the end.
const (
	workerEnv  = "MAYOI_TEST_WORKER"
	jobsEnv    = "MAYOI_TEST_JOBS"
	slowestEnv = "MAYOI_TEST_SLOWEST"
)

// splitAtLeast is how many tests are worth the workers: fewer, as one a
// test run from an editor, run in this process, where a debugger is.
const splitAtLeast = 4

// noSplit are the flags that ask for what one process does: a list, a
// profile or a benchmark of it.
var noSplit = []string{"test.list", "test.bench", "test.fuzz", "test.coverprofile", "test.cpuprofile",
	"test.memprofile", "test.blockprofile", "test.mutexprofile", "test.trace"}

// notPassed are the flags the workers are not given as they are: each
// runs the tests it is given, verbosely, to say how long each took, and
// the log go test caches by would be written over by each.
var notPassed = []string{"test.run", "test.v", "test.testlogfile"}

// splitTests runs the tests in workers when that is worth it, and reports
// whether it did, with the code to exit with.
func splitTests() (code int, split bool) {
	if os.Getenv(workerEnv) != "" {
		return 0, false
	}
	flag.Parse()
	jobs := runtime.NumCPU()
	if n, err := strconv.Atoi(os.Getenv(jobsEnv)); err == nil {
		jobs = n
	}
	set := map[string]string{}
	flag.Visit(func(f *flag.Flag) { set[f.Name] = f.Value.String() })
	run := set["test.run"]
	// A test's subtests are its own to run.
	if jobs < 2 || strings.Contains(run, "/") || slices.ContainsFunc(noSplit, func(f string) bool { return set[f] != "" }) {
		return 0, false
	}
	names, err := listTests(run)
	if err != nil || len(names) < splitAtLeast {
		return 0, false
	}
	verbose := set["test.v"]
	// The workers say each test's result, so go test -json knows of them.
	mode := "true"
	if verbose == "test2json" {
		mode = verbose
	}
	var args []string
	for name, v := range set {
		if !slices.Contains(notPassed, name) {
			args = append(args, "-"+name+"="+v)
		}
	}
	slices.Sort(args)
	args = append(args, "-test.v="+mode)
	env := append(os.Environ(), workerEnv+"=1")
	if dir, err := makeFixtureRepo(); err == nil {
		defer os.RemoveAll(dir)
		env = append(env, fixtureRepoEnv+"="+dir)
	}
	s := &splitRun{
		queue:    byTime(names, readTestTimes()),
		failFast: set["test.failfast"] == "true",
		jobs:     jobs,
		took:     map[string]float64{},
		retried:  map[string]bool{},
	}
	for _, q := range s.queue {
		s.left += q.est
	}
	var wg sync.WaitGroup
	for range min(jobs, len(names)) {
		wg.Go(func() {
			for batch := s.take(); batch != nil; batch = s.take() {
				cmd := exec.Command(os.Args[0], append(slices.Clone(args), "-test.run="+runOf(batch))...)
				cmd.Args = append(cmd.Args, flag.Args()...)
				cmd.Env = env
				var out bytes.Buffer
				cmd.Stdout, cmd.Stderr = &out, &out
				err := cmd.Run()
				s.done(batch, out.String(), err, verbose == "true" || verbose == "test2json")
			}
		})
	}
	wg.Wait()
	writeTestTimes(s.took, run == "")
	if n, _ := strconv.Atoi(os.Getenv(slowestEnv)); n > 0 {
		fmt.Printf("the %d slowest tests:\n", n)
		for _, q := range byTime(slices.Collect(maps.Keys(s.took)), s.took)[:min(n, len(s.took))] {
			fmt.Printf("%.2f %s\n", q.est, q.name)
		}
	}
	prefix := ""
	if mode == "test2json" {
		prefix = "\x16"
	}
	if s.failed {
		fmt.Println(prefix + "FAIL")
		return 1, true
	}
	fmt.Println(prefix + "PASS")
	return 0, true
}

// splitRun is the queue of the tests the workers take from, and what
// they said.
type splitRun struct {
	mu       sync.Mutex
	queue    []queuedTest
	left     float64 // the seconds the queue is thought to take
	jobs     int
	failFast bool
	failed   bool
	took     map[string]float64 // each test's seconds, as it said
	retried  map[string]bool
}

type queuedTest struct {
	name string
	est  float64 // seconds
}

// take returns the next tests for a worker, nil once there are none:
// enough to be worth a process started, and few enough, as the queue
// runs out, to leave none of the workers long at work alone.
func (s *splitRun) take() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failFast && s.failed {
		return nil
	}
	target := min(max(s.left/float64(4*s.jobs), 0.5), 5)
	var batch []string
	sum := 0.0
	for len(s.queue) > 0 && (batch == nil || sum+s.queue[0].est <= target) {
		batch = append(batch, s.queue[0].name)
		sum += s.queue[0].est
		s.queue = s.queue[1:]
	}
	s.left -= sum
	return batch
}

// resultLine is a test's result, as -test.v says it, in go test -json's
// frame or not.
var resultLine = regexp.MustCompile(`(?m)^\x16?--- (?:PASS|FAIL|SKIP): (\S+) \(([0-9.]+)s\)$`)

// done takes what a worker said of its tests, and shows it: all of it
// with -v, else as go test does, the tests that failed only.
func (s *splitRun) done(batch []string, out string, err error, verbose bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range resultLine.FindAllStringSubmatch(out, -1) {
		if sec, err := strconv.ParseFloat(m[2], 64); err == nil {
			s.took[m[1]] = sec
		}
	}
	if !verbose {
		out = failures(out)
	}
	out = withoutSummary(out)
	if err != nil {
		s.failed = true
		// The tests a worker died before, of a panic, run again, once:
		// go test would stop at it.
		var missed []string
		for _, name := range batch {
			if _, ok := s.took[name]; ok {
				continue
			}
			if s.retried[name] {
				missed = append(missed, name)
				continue
			}
			s.retried[name] = true
			s.queue = append([]queuedTest{{name, unknownTestSeconds}}, s.queue...)
			s.left += unknownTestSeconds
		}
		if len(missed) > 0 {
			out += fmt.Sprintf("%v; did not finish: %s\n", err, strings.Join(missed, " "))
		}
	}
	os.Stdout.WriteString(out)
}

// failures keeps of verbose output what go test shows without -v: the
// tests that failed, with what they said, and what came after the last
// test, such as a panic. A test's subtests say theirs after it.
func failures(out string) string {
	var b, test strings.Builder
	failed := false // the last test said it failed
	for _, line := range strings.SplitAfter(out, "\n") {
		switch {
		case strings.HasPrefix(line, "=== RUN   ") && !strings.Contains(line, "/"):
			test.Reset()
			failed = false
		case strings.HasPrefix(line, "=== "):
		case strings.HasPrefix(line, "--- FAIL"):
			b.WriteString(test.String())
			b.WriteString(line)
			test.Reset()
			failed = true
		case strings.HasPrefix(line, "--- "):
			test.Reset()
			failed = false
		case failed:
			b.WriteString(line)
		case strings.HasPrefix(strings.TrimLeft(line, " "), "--- "):
		default:
			test.WriteString(line)
		}
	}
	b.WriteString(test.String())
	return b.String()
}

// withoutSummary drops the PASS or FAIL a worker ends with: the run ends
// with one for all of them.
func withoutSummary(out string) string {
	body := strings.TrimRight(out, "\n")
	i := strings.LastIndexByte(body, '\n') + 1
	switch strings.TrimPrefix(body[i:], "\x16") {
	case "PASS", "FAIL":
		return body[:i]
	}
	return out
}

// runOf is the -test.run that runs just the tests named.
func runOf(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = regexp.QuoteMeta(n)
	}
	return "^(" + strings.Join(quoted, "|") + ")$"
}

// listTests returns the top-level tests run selects, by a worker that
// lists them.
func listTests(run string) ([]string, error) {
	if run == "" {
		run = "."
	}
	cmd := exec.Command(os.Args[0], "-test.list", run)
	cmd.Env = append(os.Environ(), workerEnv+"=1")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Fields(string(out)) {
		if strings.HasPrefix(line, "Test") {
			names = append(names, line)
		}
	}
	return names, nil
}

// unknownTestSeconds is what a test no run measured is thought to take.
const unknownTestSeconds = 1

// byTime orders the tests the slowest first, by took, and those of the
// same time as they were given.
func byTime(names []string, took map[string]float64) []queuedTest {
	q := make([]queuedTest, len(names))
	for i, n := range names {
		est, ok := took[n]
		if !ok {
			est = unknownTestSeconds
		}
		q[i] = queuedTest{n, est}
	}
	slices.SortStableFunc(q, func(a, b queuedTest) int {
		if a.est != b.est {
			if a.est > b.est {
				return -1
			}
			return 1
		}
		return strings.Compare(a.name, b.name)
	})
	return q
}

// testTimesPath is where the seconds each test last took are kept, for
// the next run to start the slowest first.
func testTimesPath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "mayoi", "test-times.json")
}

func readTestTimes() map[string]float64 {
	took := map[string]float64{}
	if p := testTimesPath(); p != "" {
		if b, err := os.ReadFile(p); err == nil {
			json.Unmarshal(b, &took)
		}
	}
	return took
}

// writeTestTimes keeps the seconds the tests took, with those of the
// tests not run this time but by a run of all, which drops those gone.
func writeTestTimes(took map[string]float64, all bool) {
	p := testTimesPath()
	if p == "" || len(took) == 0 {
		return
	}
	kept := readTestTimes()
	if all {
		kept = map[string]float64{}
	}
	for n, sec := range took {
		kept[n] = sec
	}
	b, err := json.Marshal(kept)
	if err != nil {
		return
	}
	os.MkdirAll(filepath.Dir(p), 0o755)
	tmp := fmt.Sprintf("%s.%d", p, os.Getpid())
	if os.WriteFile(tmp, b, 0o644) == nil {
		os.Rename(tmp, p)
	}
}

func TestWorkersOutputAsGoTestShows(t *testing.T) {
	out := "=== RUN   TestA\n=== RUN   TestA/x\n    a_test.go:1: fine\n--- PASS: TestA (0.10s)\n    --- PASS: TestA/x (0.00s)\n" +
		"=== RUN   TestB\n=== RUN   TestB/sub\n    b_test.go:2: broke\n--- FAIL: TestB (0.20s)\n    --- FAIL: TestB/sub (0.00s)\n" +
		"=== RUN   TestC\n--- SKIP: TestC (0.00s)\n" +
		"=== RUN   TestD\npanic: oops\nFAIL\n"
	want := "    b_test.go:2: broke\n--- FAIL: TestB (0.20s)\n    --- FAIL: TestB/sub (0.00s)\npanic: oops\n"
	if got := withoutSummary(failures(out)); got != want {
		t.Errorf("failures:\n%s\nwant:\n%s", got, want)
	}
	if got := withoutSummary(failures("=== RUN   TestE\n=== RUN   TestE/x\n--- PASS: TestE (0.10s)\n    --- PASS: TestE/x (0.00s)\nPASS\n")); got != "" {
		t.Errorf("a test passed shows %q", got)
	}
	if got := withoutSummary("\x16=== RUN   TestA\n\x16--- PASS: TestA (0.00s)\n\x16PASS\n"); got != "\x16=== RUN   TestA\n\x16--- PASS: TestA (0.00s)\n" {
		t.Errorf("json summary kept: %q", got)
	}
	m := resultLine.FindAllStringSubmatch("\x16--- PASS: TestA (1.50s)\n    --- PASS: TestA/b (0.10s)\n--- FAIL: TestB (2.00s)\n", -1)
	if len(m) != 2 || m[0][1] != "TestA" || m[0][2] != "1.50" || m[1][1] != "TestB" {
		t.Errorf("results %q", m)
	}
}

// The slowest go first, alone, and the quick ones together.
func TestWorkersQueue(t *testing.T) {
	q := byTime([]string{"TestQ1", "TestSlow", "TestQ2", "TestNew", "TestQ3"},
		map[string]float64{"TestQ1": 0.1, "TestSlow": 30, "TestQ2": 0.1, "TestQ3": 0.2})
	s := &splitRun{queue: q, jobs: 1}
	for _, x := range q {
		s.left += x.est
	}
	var got [][]string
	for b := s.take(); b != nil; b = s.take() {
		got = append(got, b)
	}
	want := [][]string{{"TestSlow"}, {"TestNew"}, {"TestQ3", "TestQ1", "TestQ2"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("batches %q, want %q", got, want)
	}
	if !regexp.MustCompile(runOf([]string{"TestA", "TestB"})).MatchString("TestB") || regexp.MustCompile(runOf([]string{"TestA"})).MatchString("TestAB") {
		t.Error("run of the batch matches other tests")
	}
}
