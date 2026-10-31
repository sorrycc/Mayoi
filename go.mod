module mayoi

go 1.27.3

tool (
	github.com/egoist/mygo/cmd/mygo
	honnef.co/go/tools/cmd/staticcheck
)

require (
	github.com/alecthomas/chroma/v2 v2.27.0
	github.com/dop251/goja v0.0.0-20261024144015-2a9269b6b6de
	github.com/ebitengine/purego v0.11.1
	github.com/egoist/mygo v0.7.1
	github.com/fsnotify/fsnotify v1.10.1
	github.com/yuin/goldmark v1.7.13
	golang.org/x/image v0.46.0
	golang.org/x/sys v0.48.0
)

require (
	github.com/BurntSushi/toml v1.4.1-0.20240526193622-a339e1f7089c // indirect
	github.com/dlclark/regexp2/v2 v2.8.1 // indirect
	github.com/go-sourcemap/sourcemap v2.1.3+incompatible // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/google/pprof v0.0.0-20230207041349-798e818bf904 // indirect
	golang.org/x/exp/typeparams v0.0.0-20231108232855-2478ac86f678 // indirect
	golang.org/x/mod v0.41.0 // indirect
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	golang.org/x/tools v0.51.0 // indirect
	honnef.co/go/tools v0.8.2 // indirect
)

replace github.com/egoist/mygo => github.com/sorrycc/mygo v0.4.9-0.20261030090718-782d3eac8a88
