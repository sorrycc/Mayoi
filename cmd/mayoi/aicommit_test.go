package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"mayoi/internal/mon"
)

// fakeGateway is Mon's AI gateway, answering each request with the
// pieces of answer, after a thinking block, or with status and body when
// status is set.
type fakeGateway struct {
	*httptest.Server
	answer []string
	status int
	body   string
	header http.Header
	// asked is the last request.
	asked struct {
		Model    string        `json:"model"`
		System   string        `json:"system"`
		Stream   bool          `json:"stream"`
		Messages []mon.Message `json:"messages"`
	}
	// hold, when set, keeps the answer back until it is closed.
	hold chan struct{}
}

func newFakeGateway(t *testing.T) *fakeGateway {
	f := &fakeGateway{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ai/v1/messages" || r.Header.Get("Authorization") != "Bearer mon_good" {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"no"}}`)
			return
		}
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &f.asked)
		if f.status != 0 {
			for k, v := range f.header {
				w.Header()[k] = v
			}
			w.WriteHeader(f.status)
			io.WriteString(w, f.body)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		send := func(data string) {
			fmt.Fprintf(w, "event: x\ndata: %s\n\n", data)
			w.(http.Flusher).Flush()
		}
		send(`{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"Let me see"}}`)
		for i, s := range f.answer {
			if i == 1 && f.hold != nil {
				select {
				case <-f.hold:
				case <-r.Context().Done():
					return
				}
			}
			q, _ := json.Marshal(s)
			send(`{"type":"content_block_delta","delta":{"type":"text_delta","text":` + string(q) + `}}`)
		}
		send(`{"type":"message_stop"}`)
	}))
	t.Cleanup(f.Close)
	return f
}

// signInTo has the harness's account signed in with token, Mon being f.
func (hs *harness) signInTo(f *fakeGateway, token string) {
	c := mon.New(f.URL)
	c.Token = token
	hs.h.signedIn, hs.h.aiClient = "Ada", c
}

// settleWriting draws frames until the AI stops writing.
func (hs *harness) settleWriting() {
	// The model's answer, from a server of the test's own, is not
	// background work settle waits for.
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		hs.settle()
		if hs.a.git.writing == nil {
			return
		}
	}
	hs.t.Fatal("the AI never stopped writing")
}

func TestWriteCommitMessage(t *testing.T) {
	hs := newHarness(t, true)
	hs.gitRepo()
	a := hs.a
	a.run("view.scm")
	hs.settleGit()

	// Signed out, there is no button.
	if _, ok := hs.tt.Find("Write the Message with AI"); ok {
		t.Fatal("the button shows while signed out")
	}
	f := newFakeGateway(t)
	hs.signInTo(f, "mon_good")
	hs.settle()
	if _, ok := hs.tt.Find("Write the Message with AI"); !ok {
		t.Fatal("no button while signed in")
	}

	// Nothing changed.
	a.writeMessage()
	hs.settleWriting()
	if !hs.tt.HasText("There are no changes to commit.") {
		t.Errorf("no word of no changes: %q", hs.tt.Texts())
	}

	// The unstaged changes, as nothing is staged, and the notes in the box.
	os.WriteFile(hs.path("main.go"), []byte("package main\n\nfunc main() { println(1) }\n"), 0o644)
	hs.settleGit()
	f.answer = []string{"```\nPrint one", " from main\n```\n"}
	a.git.message = "mention the number"
	if err := hs.tt.Click("Write the Message with AI"); err != nil {
		t.Fatal(err)
	}
	hs.settleWriting()
	if a.git.message != "Print one from main" {
		t.Errorf("message %q", a.git.message)
	}
	user := f.asked.Messages[0].Content
	for _, want := range []string{"Branch: main", "- first", "Notes from the author:\nmention the number", "The changes to commit:", "+func main() { println(1) }"} {
		if !strings.Contains(user, want) {
			t.Errorf("the prompt lacks %q:\n%s", want, user)
		}
	}
	if f.asked.Model != aiModel || !f.asked.Stream || f.asked.System != aiSystem {
		t.Errorf("asked %+v", f.asked)
	}

	// The aiModel setting names another model.
	a.settings.AIModel = "other"
	a.writeMessage()
	hs.settleWriting()
	if f.asked.Model != "other" {
		t.Errorf("model %q, want the setting's", f.asked.Model)
	}
	a.settings.AIModel = ""

	// Stopped as it writes, it keeps what came; the box takes no typing,
	// and commits wait, meanwhile.
	f.hold = make(chan struct{})
	f.answer = []string{"Half", " and more"}
	a.git.message = ""
	a.writeMessage()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		hs.settle()
		if a.git.message == "Half" {
			break
		}
	}
	if a.git.message != "Half" || a.git.writing == nil {
		t.Fatalf("while writing: %q", a.git.message)
	}
	a.commit(false)
	hs.settle()
	if a.confirm != nil || a.git.busy != "" {
		t.Error("a commit started while the AI writes")
	}
	if err := hs.tt.Click("Stop Writing"); err != nil {
		t.Fatal(err)
	}
	close(f.hold)
	f.hold = nil
	hs.settle()
	if a.git.writing != nil || a.git.message != "Half" {
		t.Errorf("after stopping: writing %v, message %q", a.git.writing != nil, a.git.message)
	}

	// Over a limit, it says when to try again, and the box stays.
	a.git.message = ""
	f.status, f.body = http.StatusTooManyRequests, `{"type":"error","error":{"type":"rate_limit_error","message":"10 requests a minute"}}`
	f.header = http.Header{"Retry-After": {"12"}}
	a.writeMessage()
	hs.settleWriting()
	if !hs.tt.HasText("try again in 12 seconds") || a.git.message != "" {
		t.Errorf("over the limit: %q, message %q", hs.tt.Texts(), a.git.message)
	}
	f.status = 0

	// A token Mon refuses signs out.
	hs.signInTo(f, "mon_revoked")
	a.writeMessage()
	hs.settleWriting()
	if hs.h.aiSignOuts != 1 {
		t.Errorf("signed out %d times", hs.h.aiSignOuts)
	}
}

func TestCleanMessage(t *testing.T) {
	for in, want := range map[string]string{
		"  Fix it\n":                  "Fix it",
		"```\nFix it\n\nBody\n```":    "Fix it\n\nBody",
		"```text\nFix it\n```\n":      "Fix it",
		"Use ``` in docs":             "Use ``` in docs",
		"Fix it\n\n- one\n- two\n\n ": "Fix it\n\n- one\n- two",
	} {
		if got := cleanMessage(in); got != want {
			t.Errorf("cleanMessage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAIFailed(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	for _, c := range []struct {
		err  error
		want string
	}{
		{&mon.RateLimitError{RetryAfter: 3 * time.Hour}, "today's AI tokens"},
		{&mon.RateLimitError{Message: "over"}, "limit is reached: over"},
		{fmt.Errorf("x: %w", errors.New("boom")), "Couldn't write the message: x: boom"},
	} {
		a.errors = nil
		a.aiFailed(c.err)
		if len(a.errors) != 1 || !strings.Contains(a.errors[0], c.want) {
			t.Errorf("aiFailed(%v): %q, want %q", c.err, a.errors, c.want)
		}
	}
}
