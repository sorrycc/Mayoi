package main

import (
	"net/url"
	"runtime"
)

// reportProblem opens a new issue of Mayoi's repository in the browser,
// titled title, with details and what any report needs: the version and
// the system. The person reads it and sends it, or not.
func (a *app) reportProblem(title, details string) {
	a.services.OpenURL(reportURL(title, details))
}

// reportURL is the address of a new issue of title, its body details and
// the version and system Mayoi runs on.
func reportURL(title, details string) string {
	body := "Mayoi " + appVersion() + " on " + runtime.GOOS + "/" + runtime.GOARCH + "\n\n"
	if details != "" {
		body += details + "\n\n"
	}
	body += "What did you do, and what did you expect to happen?\n"
	q := url.Values{"title": {title}, "body": {body}}
	return repoURL + "/issues/new?" + q.Encode()
}

// reportFromMenu is Help → Report a Problem…: an issue with nothing filled
// in but the version and the system.
func (a *app) reportFromMenu() { a.reportProblem("", "") }
