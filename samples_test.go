package haau3

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The code samples in openapi/code-samples.json, one per operation, each built and run as it is
// written: a program of its own, with HAAU3_BASE_URL pointing at a stand-in API that answers only
// the operation's example request, exactly as the spec writes it, and refuses anything else.
//
// Against the production API too, on a real key, when asked, as TestLive is:
//
//	HAAU3_LIVE_TEST=1 HAAU3_API_KEY=haau3_sk_... go test -run TestEachSampleRunsAsWritten
//
// There each sample runs with no address set, as a customer runs it, one at a time. What it must
// do is set by the key's mode, read from its prefix: a live key (haau3_sk_live_) is answered
// everywhere but the Sandbox and refused there, a test key (haau3_sk_test_) the reverse, each
// refusal a KindForbidden error whose message gives its reason. The live key comes from an
// organization with no agreement on file, which Pharmacy Benefits refuses, since it is not yet
// open. A key with neither prefix works on both bases, and its run leaves Pharmacy Benefits out.
// CI sets neither variable.

type sampleOverlay struct {
	Overlay string `json:"overlay"`
	Info    struct {
		Version string `json:"version"`
	} `json:"info"`
	Extends string `json:"extends"`
	Actions []struct {
		Target string `json:"target"`
		Update struct {
			Samples []struct {
				Lang, Label, Source string
			} `json:"x-codeSamples"`
		} `json:"update"`
	} `json:"actions"`
}

// specOperation is one operation of the pinned spec, with its example exchange.
type specOperation struct {
	id, method, path string
	pathValues       map[string]string
	query            url.Values
	body, answer     any
	answers, sends   string // the media types the spec gives the answer, and the body when there is one
}

func readJSON(t *testing.T, name string, v any) {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatal(err)
	}
}

func firstExample(media map[string]any) any {
	if v, ok := media["example"]; ok {
		return v
	}
	for _, ex := range media["examples"].(map[string]any) {
		return ex.(map[string]any)["value"] // the samples' examples each have one
	}
	return nil
}

func onlyMedia(content any) map[string]any {
	for _, media := range content.(map[string]any) {
		return media.(map[string]any)
	}
	return nil
}

func onlyMediaType(content any) string {
	for mediaType := range content.(map[string]any) {
		return mediaType
	}
	return ""
}

// specOperations is every operation of the pinned spec, in the spec's order.
func specOperations(t *testing.T) []specOperation {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("openapi", "openapi.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Info  struct{ Version string }
		Paths map[string]map[string]map[string]any
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	order, err := pathOrder(data)
	if err != nil {
		t.Fatal(err)
	}
	var ops []specOperation
	for _, path := range order {
		for _, method := range []string{"get", "post"} {
			op, ok := doc.Paths[path][method]
			if !ok {
				continue
			}
			o := specOperation{id: op["operationId"].(string), method: strings.ToUpper(method), path: path, pathValues: map[string]string{}, query: url.Values{}}
			params, _ := op["parameters"].([]any)
			for _, raw := range params {
				p := raw.(map[string]any)
				example, has := p["example"]
				if _, isRef := p["$ref"]; isRef || !has {
					continue
				}
				switch p["in"] {
				case "path":
					o.pathValues[p["name"].(string)] = fmt.Sprint(example)
				case "query":
					list, isList := example.([]any)
					if !isList {
						list = []any{example}
					}
					for _, v := range list {
						if f, isNumber := v.(float64); isNumber {
							o.query.Add(p["name"].(string), strconv.FormatFloat(f, 'f', -1, 64))
						} else {
							o.query.Add(p["name"].(string), fmt.Sprint(v))
						}
					}
				}
			}
			if body, ok := op["requestBody"].(map[string]any); ok {
				o.body = firstExample(onlyMedia(body["content"]))
				o.sends = onlyMediaType(body["content"])
			}
			answered := op["responses"].(map[string]any)["200"].(map[string]any)["content"]
			o.answer = firstExample(onlyMedia(answered))
			o.answers = onlyMediaType(answered)
			ops = append(ops, o)
		}
	}
	return ops
}

// pathOrder is the spec's paths in the order it writes them, which Go's maps forget.
func pathOrder(data []byte) ([]string, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(strings.NewReader(string(top["paths"])))
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	var paths []string
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return nil, err
		}
		paths = append(paths, key.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return nil, err
		}
	}
	return paths, nil
}

func (o specOperation) target() string {
	return fmt.Sprintf("$.paths['%s'].%s", o.path, strings.ToLower(o.method))
}

// requestKey is a request's method, path and query, in a form two requests can be compared by.
func requestKey(method, path string, query url.Values) string {
	unescaped, err := url.PathUnescape(path)
	if err != nil {
		unescaped = path
	}
	return method + " " + unescaped + " " + query.Encode() // Encode sorts by name
}

func (o specOperation) requestKey() string {
	path := o.path
	for name, value := range o.pathValues {
		path = strings.ReplaceAll(path, "{"+name+"}", value)
	}
	return requestKey(o.method, path, o.query)
}

func batchKeys(t *testing.T, bundle any) []string {
	t.Helper()
	var keys []string
	for _, e := range bundle.(map[string]any)["entry"].([]any) {
		request := e.(map[string]any)["request"].(map[string]any)
		u, err := url.Parse("https://base.invalid/v1/fhir/r4/" + request["url"].(string))
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, requestKey(request["method"].(string), u.Path, u.Query()))
	}
	return keys
}

// expectedOutput is what a sample prints for its example answer, by the rule the generator writes.
func expectedOutput(o specOperation) string {
	answer := o.answer.(map[string]any)
	switch {
	case o.id == "fhirBatch":
		return strings.TrimSuffix(strings.Repeat("ok\n", len(answer["entry"].([]any))), "\n")
	case o.id == "fhirMetadata", answer["resourceType"] == "CapabilityStatement":
		return answer["resourceType"].(string)
	case answer["resourceType"] == "Bundle":
		entries, _ := answer["entry"].([]any)
		return strconv.Itoa(len(entries))
	case answer["resourceType"] == "Parameters":
		var names []string
		for _, p := range answer["parameter"].([]any) {
			names = append(names, p.(map[string]any)["name"].(string))
		}
		return strings.Join(names, "\n")
	case answer["resourceType"] != nil:
		return answer["id"].(string)
	case answer["total"] != nil:
		return strconv.FormatFloat(answer["total"].(float64), 'f', -1, 64)
	}
	for _, v := range answer {
		if list, ok := v.([]any); ok {
			return strconv.Itoa(len(list))
		}
	}
	return ""
}

// standIn answers each operation's example request with its example answer, and refuses anything else.
func standIn(t *testing.T, ops []specOperation) *httptest.Server {
	t.Helper()
	answers := map[string]specOperation{}
	var batch specOperation
	for _, o := range ops {
		if o.id == "fhirBatch" {
			batch = o
			continue
		}
		answers[o.requestKey()] = o
	}
	want := batchKeys(t, batch.body)
	send := func(w http.ResponseWriter, status int, body any, fhir bool) {
		if fhir {
			w.Header().Set("Content-Type", "application/fhir+json")
		} else {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}
	refuse := func(w http.ResponseWriter, why string) {
		send(w, http.StatusBadRequest, map[string]any{"resourceType": "OperationOutcome", "issue": []any{
			map[string]any{"severity": "error", "code": "invalid", "diagnostics": why},
		}}, true)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		var sent any
		if len(data) > 0 {
			_ = json.Unmarshal(data, &sent)
		}
		if r.Header.Get("Authorization") != "Bearer "+testKey {
			refuse(w, "no key")
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/v1/fhir/r4" {
			if sent == nil || !slices.Equal(batchKeys(t, sent), want) {
				refuse(w, "not the example batch")
				return
			}
			send(w, http.StatusOK, batch.answer, true)
			return
		}
		o, ok := answers[requestKey(r.Method, r.URL.EscapedPath(), r.URL.Query())]
		if !ok {
			refuse(w, "not an example request")
			return
		}
		if r.Method == http.MethodPost && !reflect.DeepEqual(sent, o.body) {
			refuse(w, "not the example body")
			return
		}
		send(w, http.StatusOK, o.answer, o.answers == "application/fhir+json")
	}))
	t.Cleanup(server.Close)
	return server
}

func TestTheSamplesAreAnOverlayOfOneGoSamplePerOperation(t *testing.T) {
	var overlay sampleOverlay
	readJSON(t, filepath.Join("openapi", "code-samples.json"), &overlay)
	var spec struct{ Info struct{ Version string } }
	readJSON(t, filepath.Join("openapi", "openapi.json"), &spec)
	if overlay.Overlay != "1.2.0" || overlay.Info.Version != spec.Info.Version || overlay.Extends != "https://api.haau3.com/openapi.json" {
		t.Fatalf("overlay %q for %q extending %q", overlay.Overlay, overlay.Info.Version, overlay.Extends)
	}
	ops := specOperations(t)
	if len(overlay.Actions) != len(ops) {
		t.Fatalf("%d samples for %d operations", len(overlay.Actions), len(ops))
	}
	for i, o := range ops {
		a := overlay.Actions[i]
		if a.Target != o.target() {
			t.Errorf("action %d targets %s, want %s", i, a.Target, o.target())
		}
		if len(a.Update.Samples) != 1 || a.Update.Samples[0].Lang != "go" || a.Update.Samples[0].Label != "Go" {
			t.Errorf("%s: want one Go sample, got %+v", o.id, a.Update.Samples)
			continue
		}
		source := a.Update.Samples[0].Source
		if !strings.Contains(source, "haau3.NewClient() // reads HAAU3_API_KEY") || strings.Contains(source, "WithAPIKey") ||
			strings.Contains(source, "WithBaseURL") || strings.Contains(source, "haau3_sk_") {
			t.Errorf("%s: a sample reads the key and the address from the environment only:\n%s", o.id, source)
		}
	}
}

// Every sample builds as its own program against this module, and prints what its example
// request's answer holds.
func TestEachSampleRunsAsWritten(t *testing.T) {
	if testing.Short() {
		t.Skip("builds 36 programs")
	}
	var overlay sampleOverlay
	readJSON(t, filepath.Join("openapi", "code-samples.json"), &overlay)
	ops := specOperations(t)
	samples := map[string]string{}
	for i, o := range ops {
		samples[o.id] = overlay.Actions[i].Update.Samples[0].Source
	}
	// The control: a sample that reads another plan is refused, and fails.
	read := samples["readInsurancePlan"]
	changed := strings.Replace(read, `ReadInsurancePlan(ctx, "H2406-013-000")`, `ReadInsurancePlan(ctx, "H0000-000-000")`, 1)
	if changed == read {
		t.Fatal("the control did not change the readInsurancePlan sample")
	}
	samples["control"] = changed
	bins := buildSamples(t, samples)

	server := standIn(t, ops)
	// With no environment given, the stand-in's; production's is the key alone.
	run := func(name string, files map[string]any, env ...string) (string, error) {
		dir := t.TempDir()
		for file, content := range files {
			data, _ := json.Marshal(content)
			if err := os.WriteFile(filepath.Join(dir, file), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		cmd := exec.Command(filepath.Join(bins, name))
		cmd.Dir = dir
		cmd.Env = env
		if len(env) == 0 {
			cmd.Env = []string{"HAAU3_API_KEY=" + testKey, "HAAU3_BASE_URL=" + server.URL}
		}
		out, err := cmd.CombinedOutput()
		return strings.TrimRight(string(out), "\n"), err
	}
	// A sample whose body nests resources reads it from request.json; any other ignores the file.
	filesOf := func(o specOperation) map[string]any {
		files := map[string]any{}
		if o.body != nil {
			files["request.json"] = o.body
		}
		return files
	}
	for _, o := range ops {
		t.Run(o.id, func(t *testing.T) {
			out, err := run(o.id, filesOf(o))
			if err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			if want := expectedOutput(o); out != want {
				t.Errorf("printed %q, want %q", out, want)
			}
		})
	}
	t.Run("control", func(t *testing.T) {
		out, err := run("control", nil)
		if err == nil || !strings.Contains(out, "400") {
			t.Fatalf("a sample sending another request was not refused: %v\n%s", err, out)
		}
	})
	sampleAgainstProduction(t, ops, func(o specOperation, key string) (string, error) {
		return run(o.id, filesOf(o), "HAAU3_API_KEY="+key)
	})
}

// sampleAgainstProduction runs every built sample against the production API, when asked, after
// the stand-in's runs: what each must do on the key comes from onThisKey.
func sampleAgainstProduction(t *testing.T, ops []specOperation, run func(specOperation, string) (string, error)) {
	key := os.Getenv("HAAU3_API_KEY")
	if os.Getenv("HAAU3_LIVE_TEST") != "1" || key == "" {
		return
	}
	mode := keyMode(key)
	for _, o := range ops {
		t.Run("production/"+o.id+"/"+mode+" key", func(t *testing.T) {
			answers, refused, ok := onThisKey(mode, o)
			if !ok {
				t.Skip("a key with neither prefix may belong to an organization Pharmacy Benefits is open to")
			}
			out, err := run(o, key)
			if answers {
				if err != nil || out == "" {
					t.Fatalf("%v\n%s", err, out)
				}
				return
			}
			why := forbiddenGeneral
			if refused != "" {
				why = forbiddenReasons[refused]
			}
			if err == nil || !strings.Contains(out, why+" (403)") {
				t.Fatalf("not refused with %q: %v\n%s", why, err, out)
			}
		})
	}
}

// keyMode is a key's mode from its prefix: "live", "test", or "either" for a key with neither.
func keyMode(key string) string {
	switch {
	case strings.HasPrefix(key, "haau3_sk_live_"):
		return "live"
	case strings.HasPrefix(key, "haau3_sk_test_"):
		return "test"
	}
	return "either"
}

// onThisKey is what a sample must do on a key of this mode: answer, or be refused with this code
// ("" for none). ok is false for a sample not run on it.
func onThisKey(mode string, o specOperation) (answers bool, refused string, ok bool) {
	sandbox := strings.HasPrefix(o.path, "/v1/sandbox/")
	switch {
	case mode == "test" && !sandbox:
		return false, "test-key-outside-sandbox", true
	case mode == "live" && sandbox:
		return false, "live-key-in-sandbox", true
	case o.id == "processMessage":
		return false, "", mode == "live"
	}
	return true, "", true
}

// buildSamples builds each sample as a program in a module of its own that requires this one, as
// a customer's would, and returns the directory the programs are in.
func buildSamples(t *testing.T, samples map[string]string) string {
	t.Helper()
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	write := func(name, text string) {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module samples\n\ngo 1.26.0\n\nrequire github.com/haau3/haau3-go v0.0.0\n\nreplace github.com/haau3/haau3-go => "+root+"\n")
	for name, source := range samples {
		write(name+"/main.go", source)
	}
	bins := filepath.Join(dir, "bin") + string(filepath.Separator)
	for _, args := range [][]string{{"vet", "./..."}, {"build", "-o", bins, "./..."}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s on the samples:\n%s", args[0], out)
		}
	}
	return bins
}
