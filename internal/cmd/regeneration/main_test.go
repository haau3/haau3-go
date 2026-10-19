package main

// What a regeneration PR says and does, over oasdiff 1.33.0's real reports (testdata/oasdiff/,
// each run with openapi/oasdiff-levels.txt over two versions of the spec):
//
//   - preview-2-to-3: a path removed and another added;
//   - descriptions-only: two versions that differ only in descriptions;
//   - additions: an optional property added;
//   - method-renamed: preview.3 edited to rename an operation and drop an optional response property;
//   - security-scheme-removed: a security scheme removed.

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) []change {
	t.Helper()
	data, err := os.ReadFile("testdata/oasdiff/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var changes []change
	if err := json.Unmarshal(data, &changes); err != nil {
		t.Fatal(err)
	}
	return changes
}

func regen(changes []change, generatedChanged bool) regeneration {
	return regeneration{
		fromVersion: "1.0.0-preview.2", toVersion: "1.0.0-preview.3", etag: `W/"4l7nD8I7mwiHJI4hZ8QHmqfFP1T"`,
		specURL: "https://api.haau3.com/openapi.json", changes: changes, generatedChanged: generatedChanged,
	}
}

func TestBreakingWhenAPathIsRemoved(t *testing.T) {
	d := decide(regen(fixture(t, "preview-2-to-3"), true))
	if d.title != "feat!: the API's spec at 1.0.0-preview.3" || !d.breaking || !d.releases {
		t.Errorf("%+v", d)
	}
	for _, want := range []string{"releases a minor version", "`GET /v1/scheduling/actors`: api path removed without deprecation", "deleted the `query` request parameter `actorKey`"} {
		if !strings.Contains(d.body, want) {
			t.Errorf("the body lacks %q", want)
		}
	}
}

func TestTheThreeRaisedRulesAndAWarningCountAsBreaking(t *testing.T) {
	renamed := fixture(t, "method-renamed")
	var got []string
	for _, c := range renamed {
		got = append(got, fmt.Sprintf("%s %d", c.ID, c.Level))
	}
	if !slices.Equal(got, []string{"api-operation-id-removed 3", "response-optional-property-removed 3"}) {
		t.Errorf("%v", got)
	}
	if !strings.HasPrefix(decide(regen(renamed, true)).title, "feat!: ") {
		t.Error("a renamed method is not breaking")
	}
	var warning []change
	for _, c := range fixture(t, "preview-2-to-3") {
		if c.ID == "request-parameter-removed" {
			warning = append(warning, c)
		}
	}
	if len(warning) != 1 || warning[0].Level != 2 || !decide(regen(warning, true)).breaking {
		t.Errorf("%v", warning)
	}
}

func TestAdditionsAreAFeatureAndDescriptionsAFix(t *testing.T) {
	d := decide(regen(fixture(t, "additions"), true))
	if d.title != "feat: the API's spec at 1.0.0-preview.3" || d.breaking || !d.releases || !strings.Contains(d.body, "releases a patch version") || strings.Contains(d.body, "can break callers (") {
		t.Errorf("%+v", d)
	}
	if len(fixture(t, "descriptions-only")) != 0 {
		t.Error("descriptions-only reports changes")
	}
	if decide(regen(nil, true)).title != "fix: the API's spec at 1.0.0-preview.3" {
		t.Error("a descriptions-only change is not a fix")
	}
}

func TestNothingIsReleasedWhenTheGeneratedCodeDidNotChange(t *testing.T) {
	quiet := decide(regen(nil, false))
	if quiet.title != "chore: the API's spec at 1.0.0-preview.3" || quiet.breaking || quiet.releases {
		t.Errorf("%+v", quiet)
	}
	broke := decide(regen(fixture(t, "preview-2-to-3"), false))
	if broke.title != "chore: the API's spec at 1.0.0-preview.3" || !broke.breaking || broke.releases || !strings.Contains(broke.body, "releases nothing") {
		t.Errorf("%+v", broke)
	}
}

func TestARemovedSecuritySchemeIsNotBreaking(t *testing.T) {
	changes := fixture(t, "security-scheme-removed")
	var ids []string
	for _, c := range changes {
		ids = append(ids, c.ID)
	}
	slices.Sort(ids)
	if !slices.Equal(ids, []string{"api-global-security-removed", "api-security-component-removed"}) || decide(regen(changes, true)).breaking {
		t.Errorf("%v", ids)
	}
}

func TestTheBodyNamesBothVersionsTheETagAndTheSource(t *testing.T) {
	body := decide(regen(fixture(t, "additions"), true)).body
	for _, want := range []string{"from `1.0.0-preview.2` to `1.0.0-preview.3`", "ETag `W/\"4l7nD8I7mwiHJI4hZ8QHmqfFP1T\"`", "https://api.haau3.com/openapi.json"} {
		if !strings.Contains(body, want) {
			t.Errorf("the body lacks %q", want)
		}
	}
}

func TestTheLevelsFile(t *testing.T) {
	data, err := os.ReadFile("../../../openapi/oasdiff-levels.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"api-operation-id-removed err", "response-optional-property-removed err", "api-schema-removed err",
		"api-version-decreased none", "api-version-not-bumped none", "api-major-version-not-bumped none",
	}
	if got := strings.Split(strings.TrimSpace(string(data)), "\n"); !slices.Equal(got, want) {
		t.Errorf("%v", got)
	}
}

func TestTheBodyNeverReadsAsAConventionalCommit(t *testing.T) {
	commit := regexp.MustCompile(`(?m)^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\(.*\))?!?: `)
	footer := regexp.MustCompile(`(?i)BREAKING[ -]CHANGE|Release-As`)
	for _, name := range []string{"preview-2-to-3", "additions", "descriptions-only", "method-renamed", "security-scheme-removed"} {
		for _, generated := range []bool{true, false} {
			body := decide(regen(fixture(t, name), generated)).body
			if commit.MatchString(body) || footer.MatchString(body) || strings.Contains(body, "—") {
				t.Errorf("%s: %s", name, body)
			}
		}
	}
}

func TestTheBodyFitsGitHubsLimit(t *testing.T) {
	var many []change
	for i := range 5000 {
		c := change{ID: "endpoint-added", Text: fmt.Sprintf("the response's `property%d` changed %s", i, strings.Repeat("in a long way ", 20)), Level: 1, Operation: "GET", Path: fmt.Sprintf("/v1/fhir/r4/Resource%d", i)}
		if i%2 == 1 {
			c.ID, c.Level = "response-property-type-changed", 3
		}
		many = append(many, c)
	}
	body := decide(regen(many, true)).body
	if len(body) > maxBody+100 || !strings.Contains(body, "Push the fix to this branch") || !strings.Contains(body, "Changes that can break callers (2500)") {
		t.Errorf("%d characters", len(body))
	}
}
