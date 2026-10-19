// The tools CI runs, pinned here so the SDK's own go.mod requires nothing. golangci-lint runs from
// its own action, at the version ci.yml names, as its authors recommend.
module github.com/haau3/haau3-go/tools

go 1.26.0

tool golang.org/x/vuln/cmd/govulncheck

require (
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/telemetry v0.0.0-20261010193011-f71dc92da45e // indirect
	golang.org/x/tools v0.52.0 // indirect
	golang.org/x/vuln v1.9.0 // indirect
)
