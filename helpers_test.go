package haau3

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/haau3/haau3-go/fhir"
)

const (
	testKey   = "haau3_sk_test_key_that_must_never_be_printed"
	requestID = "6b1f3c2e-4d5a-4e6f-8a9b-0c1d2e3f4a5b"
)

// reply is an answer as the API sends one: JSON, with a request id.
type reply struct {
	status int
	body   any // marshalled to JSON; a string or []byte is sent as it is
	header map[string]string
	plain  bool // application/json rather than application/fhir+json
	err    error
	handle func(*http.Request) reply
}

func fhirReply(status int, body any) reply  { return reply{status: status, body: body} }
func plainReply(status int, body any) reply { return reply{status: status, body: body, plain: true} }

// recorder is an http.RoundTripper that answers in turn and keeps every request it was sent.
type recorder struct {
	mu       sync.Mutex
	replies  []reply
	requests []*http.Request
	bodies   [][]byte
}

func record(replies ...reply) *recorder { return &recorder{replies: replies} }

func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
	}
	r.requests = append(r.requests, req)
	r.bodies = append(r.bodies, body)
	given := r.replies[min(len(r.requests), len(r.replies))-1]
	r.mu.Unlock()
	if given.handle != nil {
		req.Body = io.NopCloser(bytes.NewReader(body))
		given = given.handle(req)
	}
	if given.err != nil {
		return nil, given.err
	}
	var content []byte
	switch b := given.body.(type) {
	case nil:
	case string:
		content = []byte(b)
	case []byte:
		content = b
	default:
		content, _ = json.Marshal(b)
	}
	header := http.Header{}
	header.Set("X-Request-Id", requestID)
	header.Set("Content-Type", "application/fhir+json")
	if given.plain {
		header.Set("Content-Type", "application/json")
	}
	for k, v := range given.header {
		header.Set(k, v)
	}
	return &http.Response{
		StatusCode: given.status, Status: fmt.Sprintf("%d %s", given.status, http.StatusText(given.status)),
		Header: header, Body: io.NopCloser(bytes.NewReader(content)), Request: req,
	}, nil
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

func (r *recorder) last() *http.Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.requests[len(r.requests)-1]
}

func (r *recorder) lastBody() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.bodies[len(r.bodies)-1]
}

// testClient is a client whose requests go to rt, with no real waits between attempts.
func testClient(t *testing.T, rt http.RoundTripper, opts ...Option) *Client {
	t.Helper()
	noWaits(t)
	c, err := NewClient(append([]Option{WithAPIKey(testKey), WithHTTPClient(&http.Client{Transport: rt})}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// noWaits records each wait between attempts instead of waiting, and pins the jitter at its middle.
func noWaits(t *testing.T) *[]time.Duration {
	t.Helper()
	waits := &[]time.Duration{}
	oldSleep, oldRandom := sleep, random
	sleep = func(ctx context.Context, d time.Duration) error {
		*waits = append(*waits, d)
		return ctx.Err()
	}
	random = func() float64 { return 0.5 }
	t.Cleanup(func() { sleep, random = oldSleep, oldRandom })
	return waits
}

func asError(t *testing.T, err error) *Error {
	t.Helper()
	// The SDK returns an *Error itself, never wrapped, so a plain assertion is the check.
	e, ok := err.(*Error) //nolint:errorlint // errors.As would accept a wrapped one
	if !ok {
		t.Fatalf("got %T (%v), want an *Error", err, err)
	}
	return e
}

// --- the spec's examples ------------------------------------------------------------------------

// specExample is one operation of the spec, called with the spec's own example, as the generated
// table in operations_gen_test.go writes it.
type specExample struct {
	id, name     string // the operationId, and the Go method
	method, path string
	pathValues   []string
	sent         []pair
	body         string
	answer       string
	call         func(ctx context.Context, c *Client, ex specExample, opts ...Option) (any, error)
	entry        func(ex specExample) entryProbe
}

// entryProbe is a batch entry and a way to read its result, whatever it answers with.
type entryProbe struct {
	entry  Entry
	result func() (any, error)
}

func probe[T any](c *Call[T]) entryProbe {
	return entryProbe{entry: c, result: func() (any, error) { return c.Result() }}
}

func exampleTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic(err)
	}
	return t
}

func exampleBody[T any](raw string) *T {
	v := new(T)
	if err := json.Unmarshal([]byte(raw), v); err != nil {
		panic(err)
	}
	return v
}

func exampleResource(raw string) fhir.Resource {
	r, err := fhir.DecodeResource([]byte(raw))
	if err != nil {
		panic(err)
	}
	return r
}

var placeholderPattern = regexp.MustCompile(`\{\w+\}`)

func templateMatches(template, path string) bool {
	parts := placeholderPattern.Split(template, -1)
	for i := range parts {
		parts[i] = regexp.QuoteMeta(parts[i])
	}
	return regexp.MustCompile("^" + strings.Join(parts, "[^/]+") + "$").MatchString(path)
}

// specAnswer is the spec's example answer for a request to this method and path.
func specAnswer(method, path string) (json.RawMessage, bool) {
	for _, ex := range specExamples {
		if ex.method == method && templateMatches(ex.path, path) {
			return withoutNext(json.RawMessage(ex.answer)), true
		}
	}
	return nil, false
}

// withoutNext drops a `next` link from an example: it points at a live page, and the tests read
// only the example itself.
func withoutNext(raw json.RawMessage) json.RawMessage {
	var v map[string]any
	if json.Unmarshal(raw, &v) != nil {
		return raw
	}
	links, ok := v["link"].([]any)
	if !ok {
		return raw
	}
	v["link"] = slices.DeleteFunc(links, func(l any) bool {
		m, _ := l.(map[string]any)
		return m["relation"] == "next"
	})
	out, _ := json.Marshal(v)
	return out
}

// specHandler answers any request the way the spec's examples say the API does.
func specHandler(req *http.Request) reply {
	path := req.URL.Path
	if req.Method == "POST" && path == "/v1/fhir/r4" {
		var batch struct {
			Entry []struct {
				Request struct{ Method, URL string } `json:"request"`
			} `json:"entry"`
		}
		body, _ := io.ReadAll(req.Body)
		_ = json.Unmarshal(body, &batch)
		entries := []any{}
		for _, e := range batch.Entry {
			relative, _, _ := strings.Cut(e.Request.URL, "?")
			resource, _ := specAnswer(e.Request.Method, "/v1/fhir/r4/"+relative)
			entries = append(entries, map[string]any{"resource": json.RawMessage(resource), "response": map[string]any{"status": "200 OK"}})
		}
		return fhirReply(200, map[string]any{"resourceType": "Bundle", "type": "batch-response", "entry": entries})
	}
	answer, ok := specAnswer(req.Method, path)
	if !ok {
		return fhirReply(404, map[string]any{"resourceType": "OperationOutcome"})
	}
	return reply{status: 200, body: []byte(answer), plain: !strings.HasPrefix(path, "/v1/fhir/")}
}

// sameJSON reports whether two JSON documents hold the same values. An RFC 3339 instant is
// compared as the instant it names: a time.Time writes `2026-10-18T21:00:00.000Z` back as
// `2026-10-18T21:00:00Z`.
func sameJSON(t *testing.T, a, b []byte) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatal(err)
	}
	ja, _ := json.Marshal(instants(x))
	jb, _ := json.Marshal(instants(y))
	return bytes.Equal(ja, jb)
}

var instant = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$`)

func instants(v any) any {
	switch v := v.(type) {
	case map[string]any:
		for k, x := range v {
			v[k] = instants(x)
		}
	case []any:
		for i, x := range v {
			v[i] = instants(x)
		}
	case string:
		if instant.MatchString(v) {
			if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
				return t.UTC().Format(time.RFC3339Nano)
			}
		}
	}
	return v
}

func queryPairs(t *testing.T, raw string) []pair {
	t.Helper()
	var out []pair
	if raw == "" {
		return out
	}
	for _, part := range strings.Split(raw, "&") {
		k, v, _ := strings.Cut(part, "=")
		name, err1 := url.QueryUnescape(k)
		value, err2 := url.QueryUnescape(v)
		if err1 != nil || err2 != nil {
			t.Fatalf("a query part that does not decode: %q", part)
		}
		out = append(out, pair{name, value})
	}
	return out
}

func sortedPairs(p []pair) []pair {
	out := slices.Clone(p)
	slices.SortFunc(out, func(a, b pair) int { return strings.Compare(a.name+"\x00"+a.value, b.name+"\x00"+b.value) })
	return out
}
