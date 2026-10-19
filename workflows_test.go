package haau3

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The workflows: what runs where, what is pinned, and the traps already met.

func workflows(t *testing.T) map[string]string {
	t.Helper()
	paths, err := filepath.Glob(".github/workflows/*.yml")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, p := range paths {
		out[filepath.Base(p)] = readFile(t, p)
	}
	return out
}

func TestThereAreFourWorkflows(t *testing.T) {
	var names []string
	for name := range workflows(t) {
		names = append(names, name)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"ci.yml", "public-text.yml", "regenerate.yml", "release.yml"}) {
		t.Errorf("%v", names)
	}
}

func TestEveryActionIsPinnedByItsCommit(t *testing.T) {
	pinned := regexp.MustCompile(`^[\w.-]+/[\w.-]+@[0-9a-f]{40}$`)
	for name, text := range workflows(t) {
		for _, m := range regexp.MustCompile(`uses: (\S+)`).FindAllStringSubmatch(text, -1) {
			if !pinned.MatchString(m[1]) {
				t.Errorf("%s: %s", name, m[1])
			}
		}
	}
}

func TestNoFromJSONInAStepEnvAndNoGrepInAPipe(t *testing.T) {
	env := regexp.MustCompile(`\n\s+env:\n((?:\s{8,}.*\n)+)`)
	for name, text := range workflows(t) {
		// GitHub evaluates a step's env before its `if`, so fromJSON of an empty output fails the job.
		for _, m := range env.FindAllStringSubmatch(text, -1) {
			if strings.Contains(m[1], "fromJSON") {
				t.Errorf("%s: fromJSON in a step's env", name)
			}
		}
		// Under pipefail, a grep that stops early can fail the command feeding it.
		if regexp.MustCompile(`\|\s*grep\b`).MatchString(text) {
			t.Errorf("%s: grep in a pipe", name)
		}
	}
}

func TestCIRunsEverySupportedGoAndTheRequiredCheckNames(t *testing.T) {
	ci := workflows(t)["ci.yml"]
	for _, want := range []string{
		`go: ["1.26", "1.27"]`, "name: Tests on Go ${{ matrix.go }}", "name: Tests on Windows",
		"name: Generated code, lint and examples", "name: Known vulnerabilities", "go run ./internal/cmd/generate -check",
		"version: v2.15.0", "govulncheck", "working-directory: examples",
	} {
		if !strings.Contains(ci, want) {
			t.Errorf("ci.yml lacks %q", want)
		}
	}
	if !strings.Contains(readFile(t, "go.mod"), "\ngo 1.26.0\n") {
		t.Error("go.mod's go line is not the lowest Go CI tests")
	}
	if !strings.Contains(workflows(t)["public-text.yml"], "name: Public text") {
		t.Error("the public-text check is not named Public text")
	}
}

func TestThePublicTextCheckRunsOnEditsAndNeverOnPullRequestTarget(t *testing.T) {
	text := workflows(t)["public-text.yml"]
	if !strings.Contains(text, "types: [opened, edited, reopened, synchronize]") || strings.Contains(text, "pull_request_target") ||
		!strings.Contains(text, "--text-env PR_TITLE --text-env PR_BODY") || !strings.Contains(text, "scripts/test_check_public_text.py") {
		t.Error(text)
	}
}

func TestRegenerationMintsTheTokenAfterTheGeneratorAndChecksOasdiff(t *testing.T) {
	text := workflows(t)["regenerate.yml"]
	if strings.Index(text, "go generate ./...") > strings.Index(text, "create-github-app-token") {
		t.Error("haau3-bot's token is minted before the generator runs")
	}
	for _, want := range []string{`OASDIFF_VERSION: "1.33.0"`, `OASDIFF_SHA256: "43a4e328e2d13ba1552d760aa68d2485c75c5621f309f6ff64ae895188345247"`} {
		if !strings.Contains(text, want) {
			t.Errorf("regenerate.yml lacks %q", want)
		}
	}
	if strings.Index(text, "sha256sum --check --strict") > strings.Index(text, `"$RUNNER_TEMP/oasdiff" changelog`) {
		t.Error("oasdiff runs before it is checked")
	}
	// It commits exactly what the generator writes.
	for _, f := range []string{"fhir/r4_gen.go", "types_gen.go", "operations_gen.go", "operations_gen_test.go", "api.md"} {
		if !strings.Contains(text, f) {
			t.Errorf("regenerate.yml does not commit %s", f)
		}
	}
}

// Only the approved job makes a tag, and with this workflow's own token, never haau3-bot's.
func TestATagIsMadeOnlyInTheApprovedJob(t *testing.T) {
	text := workflows(t)["release.yml"]
	prJob, rest, _ := strings.Cut(text, "  pending:")
	_, tagJob, _ := strings.Cut(rest, "  tag:")
	contentsWrite := regexp.MustCompile(`(?m)^\s+contents: write$`)
	if !strings.Contains(prJob, "skip-github-release: true") || strings.Contains(prJob, "\n    permissions:") {
		t.Error("the release PR job can make a release")
	}
	for _, want := range []string{"environment: release", "contents: write", "token: ${{ github.token }}", "skip-github-pull-request: true", "go run ./internal/cmd/generate -check", "go test"} {
		if !strings.Contains(tagJob, want) {
			t.Errorf("the tag job lacks %q", want)
		}
	}
	if strings.Contains(tagJob, "HAAU3_BOT") || len(contentsWrite.FindAllString(text, -1)) != 1 || strings.Count(text, "environment:") != 1 {
		t.Error("something but the approved job can write")
	}
	if strings.Contains(text, "id-token") {
		t.Error("a Go release needs no OIDC token")
	}
}
