// The Android binding lives in its own module on purpose.
//
// gomobile bind requires golang.org/x/mobile in the module graph, and the
// light node proper is deliberately a minimal-dependency client - adding a
// binding toolchain to the module that produces the server and desktop
// binaries would put it in their dependency graph for nothing. Keeping it here
// means `go build ./...` at the repo root still builds exactly what it did.
module github.com/qorechain/qorechain-lightnode/mobile

go 1.26.1

require github.com/qorechain/qorechain-lightnode v0.0.0

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/mobile v0.0.0-20261011034528-197770444867 // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/tools v0.52.0 // indirect
	modernc.org/libc v1.70.0 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
	modernc.org/sqlite v1.47.0 // indirect
)

replace github.com/qorechain/qorechain-lightnode => ../

tool golang.org/x/mobile/cmd/gobind
