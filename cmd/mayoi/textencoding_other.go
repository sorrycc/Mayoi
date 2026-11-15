//go:build !windows

package main

import "github.com/egoist/mygo/i18n"

// systemText is the legacy encoding of the app's language, which files
// not in UTF-8 are guessed to be in.
func systemText() *textEncoding { return localeText(i18n.Locale()) }
