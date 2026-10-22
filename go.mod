module mayoi

go 1.27.1

tool github.com/egoist/mygo/cmd/mygo

require (
	github.com/alecthomas/chroma/v2 v2.27.0
	github.com/ebitengine/purego v0.11.1
	github.com/egoist/mygo v0.6.0
	github.com/fsnotify/fsnotify v1.10.1
	github.com/yuin/goldmark v1.7.13
	golang.org/x/image v0.46.0
	golang.org/x/sys v0.48.0
)

require (
	github.com/dlclark/regexp2/v2 v2.2.1 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	golang.org/x/text v0.43.0 // indirect
)

replace github.com/egoist/mygo => github.com/xierenyuan/mygo v0.0.0-20261022141109-c3c7250eb76a
