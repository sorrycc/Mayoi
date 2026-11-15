package main

import "golang.org/x/sys/windows"

// systemText is the legacy encoding of the system's ANSI code page,
// which files not in UTF-8 are guessed to be in.
func systemText() *textEncoding {
	if enc := codePageText(windows.GetACP()); enc != nil {
		return enc
	}
	return cp1252Text
}
