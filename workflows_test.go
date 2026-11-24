package haau3

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
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

// An edit to a closed pull request checks its title and description alone: its commits were checked
// while it was open, and its branch may be gone. The step's own script runs, with the checker as echo.
func TestAnEditToAClosedPullRequestChecksItsTitleAndDescriptionAlone(t *testing.T) {
	text := workflows(t)["public-text.yml"]
	if !strings.Contains(text, "STATE: ${{ github.event.pull_request.state }}") {
		t.Fatal("the step does not read the pull request's state")
	}
	step := text[strings.LastIndex(text, "run: |\n")+len("run: |\n"):]
	lines := strings.Split(step, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimPrefix(line, "          ")
	}
	script := strings.ReplaceAll(strings.Join(lines, "\n"), "python3 -I scripts/check_public_text.py", "echo check")
	ran := func(env ...string) string {
		cmd := exec.Command("bash", "-c", script)
		// A later value of a variable wins over an earlier one.
		cmd.Env = append([]string{"AUTHOR=someone", "STATE=open", "BASE=b", "HEAD=h", "PATH=/usr/bin:/bin"}, env...)
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	for _, c := range []struct {
		env  []string
		want string
	}{
		{[]string{"STATE=closed"}, "check --no-files --text-env PR_TITLE --text-env PR_BODY\n"},
		// The control: an open pull request's commits are read as well.
		{[]string{"STATE=open"}, "check --commits b..h --text-env PR_TITLE --text-env PR_BODY\n"},
		// And Dependabot's files alone, open or closed.
		{[]string{"AUTHOR=dependabot[bot]", "STATE=closed"}, "check\n"},
	} {
		if got := ran(c.env...); got != c.want {
			t.Errorf("%v: %q, want %q", c.env, got, c.want)
		}
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

// Only the approved job makes a tag, as the release app, never as haau3-bot.
func TestATagIsMadeOnlyInTheApprovedJob(t *testing.T) {
	text := workflows(t)["release.yml"]
	prJob, rest, _ := strings.Cut(text, "  pending:")
	_, tagJob, _ := strings.Cut(rest, "  tag:")
	contentsWrite := regexp.MustCompile(`(?m)^\s+contents: write$`)
	if !strings.Contains(prJob, "skip-github-release: true") || strings.Contains(prJob, "\n    permissions:") {
		t.Error("the release PR job can make a release")
	}
	for _, want := range []string{"environment: release", "contents: write", "token: ${{ github.token }}", "skip-github-pull-request: true",
		"go run ./internal/cmd/generate -check", "go test", "vars.RELEASE_APP_CLIENT_ID", "secrets.RELEASE_APP_PRIVATE_KEY", `ref="refs/tags/v$VERSION"`} {
		if !strings.Contains(tagJob, want) {
			t.Errorf("the tag job lacks %q", want)
		}
	}
	// The release app's token is minted after this repository's code has run, and only here.
	if strings.Index(tagJob, "go test") > strings.Index(tagJob, "RELEASE_APP_PRIVATE_KEY") || strings.Count(text, "RELEASE_APP_PRIVATE_KEY") != 1 {
		t.Error("the release app's key is reachable before the tests, or outside the approved job")
	}
	if strings.Index(tagJob, `ref="refs/tags/v$VERSION"`) > strings.Index(tagJob, "skip-github-pull-request") {
		t.Error("the GitHub Release is made before its tag")
	}
	if strings.Contains(tagJob, "HAAU3_BOT") || len(contentsWrite.FindAllString(text, -1)) != 1 || strings.Count(text, "environment:") != 1 {
		t.Error("something but the approved job can write")
	}
	if strings.Contains(text, "id-token") {
		t.Error("a Go release needs no OIDC token")
	}
}

// The proxy can miss a tag made seconds before it is asked (v0.4.2, 2026-11-24), so the release asks
// it again for ten minutes before it fails. The step's own script runs, with a stand-in `go` that
// misses a set number of times and no wait.
func TestTheReleaseAsksTheModuleProxyAgainBeforeItFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the step runs on ubuntu-latest, and its stand-in go is a shell script")
	}
	text := workflows(t)["release.yml"]
	_, step, _ := strings.Cut(text, "- name: The module proxy has it\n")
	_, body, _ := strings.Cut(step, "run: |\n")
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "          ") {
			break
		}
		lines = append(lines, strings.TrimPrefix(line, "          "))
	}
	script := strings.Join(lines, "\n")
	if strings.Index(script, "sleep 30") > strings.Index(script, "go list -m") {
		t.Fatal("the step asks the proxy before it waits")
	}
	fast := strings.ReplaceAll(script, "sleep 30", "sleep 0")
	dir := t.TempDir()
	count := filepath.Join(dir, "count")
	stub := "#!/bin/sh\nn=$(cat \"$COUNT\" 2>/dev/null || echo 0)\nn=$((n+1))\necho \"$n\" > \"$COUNT\"\n[ \"$n\" -gt \"$MISSES\" ] || exit 1\necho \"github.com/haau3/haau3-go v$VERSION\"\n"
	if err := os.WriteFile(filepath.Join(dir, "go"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(misses string) (string, int, error) {
		_ = os.Remove(count)
		cmd := exec.Command("bash", "-e", "-c", fast)
		cmd.Env = []string{"PATH=" + dir + ":/usr/bin:/bin", "VERSION=9.9.9", "COUNT=" + count, "MISSES=" + misses}
		out, err := cmd.CombinedOutput()
		asked, _ := os.ReadFile(count)
		n, _ := strconv.Atoi(strings.TrimSpace(string(asked)))
		return string(out), n, err
	}
	out, asked, err := run("2")
	if err != nil || asked != 3 || !strings.Contains(out, "attempt 2 of 20") || !strings.HasSuffix(out, "github.com/haau3/haau3-go v9.9.9\n") {
		t.Errorf("two misses, then the proxy has it: asked %d times, %v\n%s", asked, err, out)
	}
	out, asked, err = run("99")
	if err == nil || asked != 20 || !strings.Contains(out, "::error::The module proxy does not have v9.9.9 after ten minutes.") {
		t.Errorf("the proxy never has it: asked %d times, %v\n%s", asked, err, out)
	}
}
