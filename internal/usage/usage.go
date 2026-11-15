// Package usage sends anonymous counts of Mayoi's use, how many run it
// each day and on which version and system, to Umami at
// umami.sorrycc.dev, as events of a website that stands for the app.
//
//	c := usage.New("https://umami.sorrycc.dev", usage.Website, "0.21.2")
//	err := c.Send(ctx, "active", map[string]any{"version": "0.21.2"})
//
// An event carries no account, path or file: Umami counts people by a
// hash of their address and the user agent that changes every month.
package usage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"
)

// URL is the Umami that takes the events.
const URL = "https://umami.sorrycc.dev"

// Website is the id of Umami's website for the Mayoi app.
const Website = "58063f56-bb85-4342-99f9-4f34a5c0f00a"

// hostname is the website's domain, which Umami shows the events under.
const hostname = "app.usemayoi.com"

// Client sends events to an Umami.
type Client struct {
	BaseURL, Website string
	// UserAgent tells Umami the system; it drops what looks like a bot,
	// as Go's own user agent does.
	UserAgent  string
	HTTPClient *http.Client
}

// New returns a Client for Mayoi at version.
func New(baseURL, website, version string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"), Website: website,
		UserAgent:  UserAgent(runtime.GOOS, runtime.GOARCH, version),
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// UserAgent is a browser's user agent for the system, which Umami reads
// the operating system from, with Mayoi's version at the end.
func UserAgent(goos, arch, version string) string {
	system := "X11; Linux x86_64"
	switch {
	case goos == "darwin":
		system = "Macintosh; Intel Mac OS X 10_15_7"
	case goos == "windows":
		system = "Windows NT 10.0; Win64; x64"
	case arch == "arm64":
		system = "X11; Linux aarch64"
	}
	return fmt.Sprintf("Mozilla/5.0 (%s) Mayoi/%s", system, version)
}

// Payload is what Umami's /api/send takes of an event.
type Payload struct {
	Website  string         `json:"website"`
	Hostname string         `json:"hostname"`
	URL      string         `json:"url"`
	Title    string         `json:"title"`
	Language string         `json:"language,omitempty"`
	Name     string         `json:"name,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
}

// Event is the payload of the event name with data, in language, such as
// "zh-Hans".
func (c *Client) Event(name, language string, data map[string]any) Payload {
	return Payload{Website: c.Website, Hostname: hostname, URL: "/", Title: "Mayoi", Language: language, Name: name, Data: data}
}

// View is the payload of a page view of /version, which Umami's
// dashboard counts as a visitor, by system, country and version, as it
// doesn't events.
func (c *Client) View(version, language string) Payload {
	return Payload{Website: c.Website, Hostname: hostname, URL: "/" + version, Title: "Mayoi " + version, Language: language}
}

// Send sends the event p.
func (c *Client) Send(ctx context.Context, p Payload) error {
	body, err := json.Marshal(map[string]any{"type": "event", "payload": p})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/api/send", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("usage: HTTP %d", resp.StatusCode)
	}
	return nil
}
