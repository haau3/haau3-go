package haau3

import (
	"errors"
	"net/url"
	"testing"
	"time"
)

// Node 24's URLSearchParams and encodeURIComponent, measured 2026-10-19 over the samples the
// Python SDK's parity test uses, so every SDK writes a request byte for byte alike. Go has no lone
// surrogate; the bytes that would hold one are not UTF-8, and are written as U+FFFD as JavaScript
// writes the surrogate.
var javascriptWrites = []struct{ sample, query, path string }{
	{"Blue Cross & co", "k=Blue+Cross+%26+co", "Blue%20Cross%20%26%20co"},
	{"a~b*c-d_e.f", "k=a%7Eb*c-d_e.f", "a~b*c-d_e.f"},
	{"1992700001,1992700002", "k=1992700001%2C1992700002", "1992700001%2C1992700002"},
	{"name:contains", "k=name%3Acontains", "name%3Acontains"},
	{"x|y/z?w#v", "k=x%7Cy%2Fz%3Fw%23v", "x%7Cy%2Fz%3Fw%23v"},
	{"http://loinc.org", "k=http%3A%2F%2Floinc.org", "http%3A%2F%2Floinc.org"},
	{"café ünïcode", "k=caf%C3%A9+%C3%BCn%C3%AFcode", "caf%C3%A9%20%C3%BCn%C3%AFcode"},
	{"emoji 🙂", "k=emoji+%F0%9F%99%82", "emoji%20%F0%9F%99%82"},
	{"lone \xed\xa0\x80 surrogate", "k=lone+%EF%BF%BD+surrogate", ""},
	{"!'()*", "k=%21%27%28%29*", "!'()*"},
	{"+plus%percent", "k=%2Bplus%25percent", "%2Bplus%25percent"},
	{"", "k=", ""},
}

func TestRequestsAreWrittenByteForByteAsJavaScriptWritesThem(t *testing.T) {
	for _, w := range javascriptWrites {
		if got := encodeQuery(query{{"k", w.sample}}); got != w.query {
			t.Errorf("query %q: %q, want %q", w.sample, got, w.query)
		}
		if w.path != "" || w.sample == "" {
			if got := encodePathValue(w.sample); got != w.path {
				t.Errorf("path %q: %q, want %q", w.sample, got, w.path)
			}
		}
	}
}

func TestQueryValues(t *testing.T) {
	var q query
	q.bool("isVirtual", new(true))
	q.bool("includePast", new(false))
	q.string("state", nil)
	q.int("limit", new(10))
	q.strings("npi", []string{"1", "2"})
	q.strings("none", nil)
	q.string("empty", new(""))
	if got := encodeQuery(q); got != "isVirtual=true&includePast=false&limit=10&npi=1&npi=2&empty=" {
		t.Errorf("got %q", got)
	}
	var required query
	if err := required.requiredString("code", ""); err == nil {
		t.Error("an empty required value was taken")
	}
	if err := required.requiredStrings("npi", nil); err == nil {
		t.Error("an empty required list was taken")
	}
	// url.Values in name order, never Go's own encoding.
	if got := encodeValues(url.Values{"b": {"x y"}, "a": {"1~2", "3"}}); got != "a=1%7E2&a=3&b=x+y" {
		t.Errorf("got %q", got)
	}
}

func TestATimeIsSentWithSecondsAndItsOwnOffset(t *testing.T) {
	var q query
	eastern := time.FixedZone("EDT", -4*60*60)
	q.time("startAfter", new(time.Date(2026, 7, 6, 13, 0, 0, 0, eastern)))
	q.time("startBefore", new(exampleTime("2026-07-06T17:00:00.500Z")))
	q.time("absent", nil)
	if got := encodeQuery(q); got != "startAfter=2026-07-06T13%3A00%3A00-04%3A00&startBefore=2026-07-06T17%3A00%3A00.5Z" {
		t.Errorf("got %q", got)
	}
}

func TestAURLOnAnotherHostIsRefused(t *testing.T) {
	c := testClient(t, record())
	for _, there := range []string{"https://evil.example/v1/fhir/r4", "http://api.haau3.com/v1", "https://api.haau3.com:8443/v1"} {
		_, err := c.urlOnBase(there, "", "GET x")
		var e *Error
		if !errors.As(err, &e) || e.Kind != KindNotSent {
			t.Errorf("%s was not refused: %v", there, err)
		}
	}
	for _, here := range []string{"https://api.haau3.com/v1/fhir/r4/x", "https://API.haau3.com:443/v1", "/v1/x", "v1/x"} {
		if _, err := c.urlOnBase(here, "", "GET x"); err != nil {
			t.Errorf("%s was refused: %v", here, err)
		}
	}
	got, _ := c.urlOnBase("https://api.haau3.com/v1/x?page=2", "a=b", "GET x")
	if got != "https://api.haau3.com/v1/x?page=2&a=b" {
		t.Errorf("got %s", got)
	}
}

func TestABaseURLWithAPathKeepsItsPathAndAsksForFHIR(t *testing.T) {
	rec := record(fhirReply(200, map[string]any{"resourceType": "InsurancePlan", "id": "H1"}))
	c := testClient(t, rec, WithBaseURL("https://proxy.example.test/haau3/"))
	if _, err := c.ReadInsurancePlan(t.Context(), "H1"); err != nil {
		t.Fatal(err)
	}
	req := rec.last()
	if req.URL.String() != "https://proxy.example.test/haau3/v1/fhir/r4/InsurancePlan/H1" {
		t.Errorf("sent to %s", req.URL)
	}
	if req.Header.Get("Accept") != "application/fhir+json" {
		t.Errorf("Accept %q", req.Header.Get("Accept"))
	}
}

// The API reads a path equal to or under /v1/fhir or /v1/sandbox/fhir as FHIR, and nothing else.
func TestACallToEitherFHIRBaseAsksForAndSendsFHIRJSON(t *testing.T) {
	for _, tc := range []struct {
		path string
		fhir bool
	}{
		{"/v1/sandbox/fhir/r4/$process-message", true},
		{"/v1/sandbox/fhir", true},
		{"/v1/fhir/r4/$process-message", true},
		{"/v1/fhir", true},
		// The controls.
		{"/v1/sandbox", false},
		{"/v1/sandbox/fhirish", false},
		{"/v1/scheduling/slots", false},
	} {
		want := "application/json"
		if tc.fhir {
			want = "application/fhir+json"
		}
		for _, base := range []string{"https://api.haau3.com", "https://proxy.example.test/haau3/"} {
			rec := record(fhirReply(200, map[string]any{"resourceType": "Bundle"}))
			c := testClient(t, rec, WithBaseURL(base))
			if _, err := c.Request(t.Context(), RawRequest{Method: "POST", Path: tc.path, Body: map[string]any{"resourceType": "Bundle"}}); err != nil {
				t.Fatal(err)
			}
			req := rec.last()
			if got := req.Header.Get("Accept"); got != want {
				t.Errorf("%s under %s: Accept %q, want %q", tc.path, base, got, want)
			}
			if got := req.Header.Get("Content-Type"); got != want {
				t.Errorf("%s under %s: Content-Type %q, want %q", tc.path, base, got, want)
			}
		}
	}
}

func TestJSONBodiesKeepHTMLCharacters(t *testing.T) {
	got, err := jsonBytes(map[string]string{"div": "<div>a & b</div>"})
	if err != nil || string(got) != `{"div":"<div>a & b</div>"}` {
		t.Errorf("got %s, %v", got, err)
	}
}
