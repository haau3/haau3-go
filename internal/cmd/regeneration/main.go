// Command regeneration describes a regeneration pull request for .github/workflows/regenerate.yml,
// from what oasdiff found between the pinned spec and the live one, and whether the generated code
// changed:
//
//   - a change oasdiff grades ERR or WARN (with openapi/oasdiff-levels.txt) can break callers: the
//     PR is labelled `breaking` and titled `feat!:`, which release-please releases as a minor while
//     the SDK is 0.y.z;
//   - any other change it reports is `feat:`, a patch;
//   - generated code that changed with nothing reported (descriptions, which become doc comments)
//     is `fix:`, a patch;
//   - generated code that did not change is `chore:`, which releases nothing: the new spec is
//     pinned and rides with the next release. A breaking change still labels such a PR, because
//     the API changed for its callers even though the SDK did not.
//
// The PR is squash-merged with its description as the commit body, and release-please reads that
// body too. So the description never starts a paragraph the way a Conventional Commit does, and
// never names a breaking-change footer or Release-As.
//
//	GENERATED_CHANGED=true go run ./internal/cmd/regeneration <pinned spec> <changes> <body file>
//
// <pinned spec> is the spec main had before fetchspec, and <changes> is what
// `oasdiff changelog --format json` reported between it and the new openapi/openapi.json. It prints
// `title=`, `breaking=` and `releases=` lines for $GITHUB_OUTPUT and writes the description to
// <body file>. HAAU3_SPEC_URL names where the spec was read from.
package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

const (
	// warn is oasdiff's level for a warning: 3 is ERR, 2 is WARN, 1 is INFO.
	warn = 2
	// maxBody leaves room in GitHub's 65,536-character limit on a PR description for its own text.
	maxBody = 60_000
	listed  = 200
)

// change is one change as `oasdiff changelog --format json` reports it.
type change struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	Level     int    `json:"level"`
	Operation string `json:"operation"`
	Path      string `json:"path"`
}

type regeneration struct {
	fromVersion, toVersion, etag, specURL string
	changes                               []change
	generatedChanged                      bool
}

type decision struct {
	title    string
	breaking bool // whether the PR carries the `breaking` label
	releases bool // whether merging it leads to a release
	body     string
}

func count(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

var space = regexp.MustCompile(`\s+`)

// describe is one change on one line: where, what, and oasdiff's rule.
func describe(c change) string {
	where := ""
	if c.Operation != "" && c.Path != "" {
		where = fmt.Sprintf("`%s %s`: ", c.Operation, c.Path)
	}
	return fmt.Sprintf("%s%s (`%s`)", where, strings.TrimSpace(space.ReplaceAllString(c.Text, " ")), c.ID)
}

func list(heading string, changes []change) []string {
	if len(changes) == 0 {
		return nil
	}
	var lines []string
	for i, c := range changes {
		if i == listed {
			lines = append(lines, fmt.Sprintf("- and %d more", len(changes)-listed))
			break
		}
		lines = append(lines, "- "+describe(c))
	}
	return []string{"### " + heading, strings.Join(lines, "\n")}
}

func decide(r regeneration) decision {
	var breaking, other []change
	for _, c := range r.changes {
		if c.Level >= warn {
			breaking = append(breaking, c)
		} else {
			other = append(other, c)
		}
	}
	kind := "fix"
	switch {
	case !r.generatedChanged:
		kind = "chore"
	case len(breaking) > 0:
		kind = "feat!"
	case len(r.changes) > 0:
		kind = "feat"
	}
	var effect string
	switch {
	case !r.generatedChanged && len(breaking) > 0:
		effect = "Merging this pins the new spec and releases nothing, because the generated code did not change. " +
			fmt.Sprintf("oasdiff still found %s that can break callers of the API, listed below.", count(len(breaking), "change"))
	case !r.generatedChanged:
		effect = "Merging this pins the new spec and releases nothing, because the generated code did not change."
	case len(breaking) > 0:
		effect = fmt.Sprintf("Merging this releases a minor version: oasdiff found %s that can break callers, listed below.", count(len(breaking), "change"))
	case len(r.changes) > 0:
		effect = "Merging this releases a patch version: the generated code changed, and nothing below breaks callers."
	default:
		effect = "Merging this releases a patch version: only descriptions or examples changed, and the generated doc comments with them."
	}
	sections := []string{
		fmt.Sprintf("The API's spec changed from `%s` to `%s`, read from %s with ETag `%s`.", r.fromVersion, r.toVersion, r.specURL, r.etag),
		effect,
		"If a check fails, this spec needs a change by hand (a method the README calls is gone, say). " +
			"Push the fix to this branch: haau3-bot never overwrites a commit it did not make.",
		"The title becomes the line in CHANGELOG.md, so rewrite it in plain words if you like, keeping its " +
			"prefix. Taking the `!` out of `feat!:` releases a patch instead of a minor.",
	}
	sections = append(sections, list(fmt.Sprintf("Changes that can break callers (%d)", len(breaking)), breaking)...)
	sections = append(sections, list(fmt.Sprintf("Other changes (%d)", len(other)), other)...)
	body := strings.Join(sections, "\n\n") + "\n"
	if len(body) > maxBody {
		// The lists come last, so only their tail is lost.
		body = body[:strings.LastIndex(body[:maxBody], "\n")] + "\n- and more, cut short to fit\n"
	}
	return decision{
		title:    fmt.Sprintf("%s: the API's spec at %s", kind, r.toVersion),
		breaking: len(breaking) > 0, releases: r.generatedChanged, body: body,
	}
}

func version(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var doc struct {
		Info struct {
			Version string `json:"version"`
		} `json:"info"`
	}
	return doc.Info.Version, json.Unmarshal(data, &doc)
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "regeneration:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 3 {
		return fmt.Errorf("usage: regeneration <pinned spec> <changes> <body file>")
	}
	from, err := version(args[0])
	if err != nil {
		return err
	}
	to, err := version("openapi/openapi.json")
	if err != nil {
		return err
	}
	etag, err := os.ReadFile("openapi/etag")
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(args[1])
	if err != nil {
		return err
	}
	var changes []change
	if text := strings.TrimSpace(string(raw)); text != "" {
		if err := json.Unmarshal([]byte(text), &changes); err != nil {
			return err
		}
	}
	d := decide(regeneration{
		fromVersion: from, toVersion: to, etag: strings.TrimSpace(string(etag)),
		specURL:          cmp.Or(os.Getenv("HAAU3_SPEC_URL"), "https://api.haau3.com/openapi.json"),
		changes:          changes,
		generatedChanged: os.Getenv("GENERATED_CHANGED") == "true",
	})
	if err := os.WriteFile(args[2], []byte(d.body), 0o644); err != nil {
		return err
	}
	fmt.Printf("title=%s\nbreaking=%t\nreleases=%t\n", d.title, d.breaking, d.releases)
	return nil
}
