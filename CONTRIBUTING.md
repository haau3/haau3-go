# Contributing

Issues are welcome: a bug, a confusing name, a missing example.

## Working on the SDK

```sh
go test ./...
go vet ./...
go run ./internal/cmd/generate -check
(cd examples && go test ./...)
go tool -modfile=tools/go.mod govulncheck ./...
python3 -I scripts/test_check_public_text.py
```

The SDK needs Go 1.26 or newer, and requires no module outside Go's standard library: CI fails if
it ever does. The examples and the tools are modules of their own, in `examples/` and `tools/`, so
what they require never reaches a customer's build. CI tests on Go 1.26 and 1.27, on Linux and on
Windows, and lints with [golangci-lint](https://golangci-lint.run) (`.golangci.yml`).

## Generated code

These are written by `internal/cmd/generate` from `openapi/openapi.json`, the pinned copy of
`https://api.haau3.com/openapi.json`, and from `hl7/fhir.schema.json.zip`, HL7's JSON Schema for
FHIR R4 (4.0.1), pinned by its sha256. Never edit them by hand:

- `operations_gen.go`: one method per operation, each operation's params struct, and each entry of
  a batch;
- `types_gen.go`: the API's own components, such as Scheduling's records and `Problem`;
- `fhir/r4_gen.go`: every FHIR R4 type;
- `operations_gen_test.go`: each operation called with the spec's own example;
- `api.md`.

To take a new spec:

```sh
go run ./internal/cmd/fetchspec
go generate ./...
```

CI generates again (`go run ./internal/cmd/generate -check`) and fails if the result differs from
what is committed.

`Calls` offers every operation on the FHIR base but the ones a batch cannot carry, which the
generator names in `notInABatch` with the spec's own words. It stops if one of those leaves the
spec, or if two names would be one in Go.

The FHIR types come from HL7's own schema rather than a Go FHIR package, because none is both
maintained and widely used for R4. A place that holds any resource, such as `Bundle.entry.resource`,
holds a `fhir.Resource`, read as the type its `resourceType` names.

## When the API changes

`.github/workflows/regenerate.yml` fetches the spec every morning, and can be run by hand
(`gh workflow run regenerate.yml`). When the spec changed, it regenerates the SDK and opens one pull
request as haau3-bot, or updates the one already open. It never overwrites a commit it did not
make, so a fix pushed to its branch stays.

[oasdiff](https://github.com/oasdiff/oasdiff) compares the pinned spec with the new one, and a change
it grades as an error or a warning can break callers: the PR is labelled `breaking` and titled
`feat!:`. `openapi/oasdiff-levels.txt` changes six of oasdiff's grades (the file takes no comments,
so the reasons are here):

- three changes break a typed caller although they do not break an HTTP one, so each is an error: a
  changed operationId (`api-operation-id-removed`) renames a method, a removed optional response
  property (`response-optional-property-removed`) stops code that reads it compiling, and a
  removed component (`api-schema-removed`) removes an exported type;
- three rules compare `info.version` as a release number and misread `1.0.0-preview.N`, so they are
  off (`api-version-decreased`, `api-version-not-bumped`, `api-major-version-not-bumped`).

If the generated code did not change, the PR is titled `chore:` and releases nothing. To run oasdiff
yourself, against the spec on `main`:

```sh
oasdiff changelog old.json openapi/openapi.json --severity-levels openapi/oasdiff-levels.txt
```

## Pull requests and releases

Pull requests are squash-merged, and a PR's title becomes its commit on `main`.
[release-please](https://github.com/googleapis/release-please) reads those titles and keeps one
release PR open with the next version and its changelog:

- `feat: ...` and `fix: ...` release a patch, 0.1.1 to 0.1.2.
- `feat!: ...` releases a minor, 0.1.1 to 0.2.0. While the SDK is `0.y.z`, a minor is the only
  release that may change what a method takes or answers. Raising the lowest Go the SDK supports is
  one.
- Any other title (`docs: ...`, `chore: ...`, `README: ...`) releases nothing.

The title is the line a reader sees in `CHANGELOG.md`, so it says what changed for someone using the
SDK. Like every file, commit message and PR description here, it says only what this repository
shows: CI's "Public text" check (`python3 -I scripts/check_public_text.py`) fails on anything that
refers elsewhere, such as an issue number without its repository. It is Python, using only the
standard library, because Go's regular expressions lack the lookbehind four of its rules use.

The version lives in one place, `Version` in `version.go`, which release-please writes. A Go version
is public the moment its tag exists: the module proxy fetches it on first request, and Go's checksum
database pins its contents from then on, so a tag is never moved or remade. Merging the release PR
publishes nothing. `release.yml` then waits in the `release` environment until a maintainer approves
it, tests that version once more, and makes the `v` tag as the release app: a GitHub App that can
only write contents, whose key only that approved job can read, and the one identity that may
create, move or delete a `v*` tag here. The GitHub Release for the tag follows.

## Tests

The tests answer the SDK with an `http.RoundTripper` of their own, and a few run against local
servers that refuse, hang or answer slowly. `TestLive` calls the production API, and runs only with
`HAAU3_LIVE_TEST=1` and a key in `HAAU3_API_KEY`.

`privacy_test.go` plants a marker in every body and renders each error every way it is commonly
printed: each of `fmt`'s verbs, a wrapped and a joined error, `slog`'s text and JSON handlers,
`json.Marshal` and a recovered panic. A body is held in a function, never a field that a printer or
`json.Marshal` can reach, and a type that holds one prints itself without it.

`readme_test.go` compiles every Go block in the README against this module, and runs its test.
