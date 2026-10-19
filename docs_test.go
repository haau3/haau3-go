package haau3

import (
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func readFile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestEveryNameTheREADMEUsesExists(t *testing.T) {
	readme := readFile(t, "README.md")
	client := reflect.TypeFor[*Client]()
	called := regexp.MustCompile(`\bclient\.(\w+)\(`).FindAllStringSubmatch(readme, -1)
	if len(called) < 10 {
		t.Fatalf("only %d calls found", len(called))
	}
	for _, m := range called {
		if _, ok := client.MethodByName(m[1]); !ok {
			t.Errorf("the README calls client.%s, which does not exist", m[1])
		}
	}
	calls := reflect.TypeFor[BatchCalls]()
	for _, m := range regexp.MustCompile(`\bhaau3\.Calls\.(\w+)\(`).FindAllStringSubmatch(readme, -1) {
		if _, ok := calls.MethodByName(m[1]); !ok {
			t.Errorf("the README calls haau3.Calls.%s, which does not exist", m[1])
		}
	}
	// Every haau3.X the README names is exported by this package: a type, a function, a var or a const.
	exported := packageNames(t)
	for _, m := range regexp.MustCompile(`\bhaau3\.([A-Z]\w*)`).FindAllStringSubmatch(readme, -1) {
		if !slices.Contains(exported, m[1]) {
			t.Errorf("the README names haau3.%s, which does not exist", m[1])
		}
	}
}

// packageNames is every name this package declares at its top level, from its own source.
func packageNames(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	decl := regexp.MustCompile(`(?m)^(?:func|type|var|const)\s+(?:\([^)]*\)\s*)?([A-Z]\w*)|^\t([A-Z]\w*)(?:\s*$|\s+(?:=|[A-Za-z*\[]))`)
	var names []string
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		for _, m := range decl.FindAllStringSubmatch(readFile(t, e.Name()), -1) {
			names = append(names, m[1]+m[2])
		}
	}
	return names
}

func TestTheREADMECallsOneOperationOfEachService(t *testing.T) {
	readme := readFile(t, "README.md")
	byName := map[string]string{}
	tags := map[string]bool{}
	api := readFile(t, "api.md")
	section := ""
	for _, line := range strings.Split(api, "\n") {
		if strings.HasPrefix(line, "## ") {
			section = strings.TrimPrefix(line, "## ")
		}
		if m := regexp.MustCompile("^\\| \\[`(\\w+)\\(").FindStringSubmatch(line); m != nil {
			byName[m[1]] = section
			if section != "FHIR base" {
				tags[section] = true
			}
		}
	}
	shown := map[string]bool{}
	for _, m := range regexp.MustCompile(`\bclient\.(\w+)\(`).FindAllStringSubmatch(readme, -1) {
		if tag, ok := byName[m[1]]; ok {
			shown[tag] = true
		}
	}
	for tag := range tags {
		if !shown[tag] {
			t.Errorf("the README calls nothing of %s", tag)
		}
	}
}

func TestTheREADMEOpensWithThePreviewSentenceAndTheAgentSection(t *testing.T) {
	preview := "The haau3 platform API is in preview: it may change as we build, usually to add features or improve performance."
	for _, name := range []string{"README.md", "doc.go"} {
		if !strings.Contains(readFile(t, name), preview) {
			t.Errorf("%s lacks the preview sentence", name)
		}
	}
	readme := readFile(t, "README.md")
	for _, want := range []string{"https://docs.haau3.com/llms.txt", "claude mcp add --transport http haau3 https://docs.haau3.com/mcp",
		"SMART Scheduling Links", "Medicare Advantage", "ACA Marketplace", "National Provider Directory", "RxNorm", "LOINC",
		"https://www.npmjs.com/package/@haau3/sdk", "https://pypi.org/project/haau3/"} {
		if !strings.Contains(readme, want) {
			t.Errorf("the README lacks %q", want)
		}
	}
	doc := readFile(t, "doc.go")
	for _, want := range []string{"SMART Scheduling Links", "Medicare Advantage", "ACA Marketplace", "medication reconciliation",
		"National Provider Directory", "RxNorm", "LOINC", "https://docs.haau3.com/llms.txt", "https://docs.haau3.com/mcp"} {
		if !strings.Contains(doc, want) {
			t.Errorf("the package doc lacks %q", want)
		}
	}
}

func TestTheREADMESaysWhatIsCheckedAndNoMore(t *testing.T) {
	readme := strings.Join(strings.Fields(readFile(t, "README.md")), " ")
	for _, want := range []string{"The check is of the resource type, and of each element's JSON type", "The SDK does not validate each element against FHIR R4"} {
		if !strings.Contains(readme, want) {
			t.Errorf("the README lacks %q", want)
		}
	}
	if strings.Contains(readme, "always the type it says") {
		t.Error("the README claims more than the SDK checks")
	}
}

func TestAPIMDListsEveryOperationOnceWithItsDocsPage(t *testing.T) {
	api := readFile(t, "api.md")
	rows := regexp.MustCompile("(?m)^\\| \\[`(\\w+)\\(.*?\\)`\\]\\((https://docs\\.haau3\\.com/api-reference/[\\w/-]+)\\) \\| `(GET|POST) (\\S+)` \\|").FindAllStringSubmatch(api, -1)
	var names, want []string
	pages := map[string]bool{}
	for _, r := range rows {
		names = append(names, r[1])
		pages[r[2]] = true
	}
	for _, ex := range specExamples {
		want = append(want, ex.name)
	}
	slices.Sort(names)
	slices.Sort(want)
	if !slices.Equal(names, want) || len(pages) != len(rows) {
		t.Errorf("api.md lists %v", names)
	}
	if !strings.Contains(api, "(`"+SpecVersion+"`)") || !strings.Contains(api, "except `FHIRBatch` and `ReconcileMedications`:\n29 of the 36") {
		t.Error("api.md's opening is not what the spec says")
	}
}

func TestNoEmDashInWhatThisRepositoryWrites(t *testing.T) {
	for _, name := range []string{"README.md", "CONTRIBUTING.md", "SECURITY.md", "doc.go", "options.go", "errors.go", "client.go", "fhir/resource.go"} {
		if strings.Contains(readFile(t, name), "—") {
			t.Errorf("%s has an em dash", name)
		}
	}
}
