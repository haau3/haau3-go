package haau3

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/haau3/haau3-go/fhir"
)

func TestTheKeyComesFromTheEnvironment(t *testing.T) {
	t.Setenv("HAAU3_API_KEY", "from-the-environment")
	rec := record(fhirReply(200, plan))
	c, err := NewClient(WithHTTPClient(&http.Client{Transport: rec}))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = c.ReadInsurancePlan(context.Background(), "H1")
	if rec.last().Header.Get("Authorization") != "Bearer from-the-environment" {
		t.Error(rec.last().Header.Get("Authorization"))
	}
}

func TestNoKeyIsRefused(t *testing.T) {
	t.Setenv("HAAU3_API_KEY", "")
	for _, opts := range [][]Option{nil, {WithAPIKey("")}} {
		_, err := NewClient(opts...)
		if e := asError(t, err); e.Kind != KindNotSent || e.Error() != "No API key: pass haau3.WithAPIKey, or set HAAU3_API_KEY." {
			t.Errorf("%q", err)
		}
	}
	// A key you pass as empty is not replaced by the environment's.
	t.Setenv("HAAU3_API_KEY", "from-the-environment")
	if _, err := NewClient(WithAPIKey("")); err == nil {
		t.Error("an empty key was taken")
	}
	if _, err := testClient(t, record()).ReadInsurancePlan(context.Background(), "H1", WithAPIKey("")); err == nil {
		t.Error("an empty per-call key was taken")
	}
}

func TestABrowserIsRefused(t *testing.T) {
	old := goos
	goos = "js"
	defer func() { goos = old }()
	_, err := NewClient(WithAPIKey(testKey))
	if e := asError(t, err); e.Kind != KindNotSent || e.Error() != "haau3 runs on a server only: an API key in a browser is public. Call the API from your server." {
		t.Errorf("%v", err)
	}
}

func TestABadBaseURLIsRefusedWhenTheClientIsMade(t *testing.T) {
	for _, bad := range []string{"api.haau3.com", "ftp://api.haau3.com", "https://", "https://api.haau3.com:port", "https://api.haau3.com/?q=1", "::"} {
		_, err := NewClient(WithAPIKey(testKey), WithBaseURL(bad))
		if e := asError(t, err); e.Kind != KindNotSent || !strings.HasPrefix(e.Error(), "WithBaseURL takes an http or https URL with a host") {
			t.Errorf("%s: %v", bad, err)
		}
	}
	c, err := NewClient(WithAPIKey(testKey), WithBaseURL("http://localhost:8080///"))
	if err != nil || c.BaseURL() != "http://localhost:8080" {
		t.Errorf("%v %v", c, err)
	}
}

func TestAnOptionInTheWrongPlaceIsRefused(t *testing.T) {
	if _, err := NewClient(WithAPIKey(testKey), WithCorrelationID("6b1f3c2e-4d5a-4e6f-8a9b-0c1d2e3f4a5b")); err == nil {
		t.Error("NewClient took WithCorrelationID")
	}
	if _, err := NewClient(WithAPIKey(testKey), WithResponseInfo(&ResponseInfo{})); err == nil {
		t.Error("NewClient took WithResponseInfo")
	}
	if _, err := NewClient(WithAPIKey(testKey), WithTimeout(0)); err == nil {
		t.Error("NewClient took a zero timeout")
	}
	rec := record(fhirReply(200, plan))
	c := testClient(t, rec)
	for _, opt := range []Option{WithBaseURL("https://other.example"), WithHTTPClient(http.DefaultClient), WithMaxRetries(-1)} {
		_, err := c.ReadInsurancePlan(context.Background(), "H1", opt)
		if e := asError(t, err); e.Kind != KindNotSent {
			t.Errorf("%v", err)
		}
	}
	if rec.count() != 0 {
		t.Errorf("%d requests", rec.count())
	}
}

func TestTheVersionMatchesTheReleaseManifest(t *testing.T) {
	data, err := os.ReadFile(".release-please-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `".": "`+Version+`"`) {
		t.Errorf("Version is %s; the manifest says %s", Version, data)
	}
}

func TestTheUserAgentNamesTheLanguageAndTheRuntime(t *testing.T) {
	if !regexp.MustCompile(`^haau3-go/\d+\.\d+\.\d+ \(go 1\.\d+\)$`).MatchString(userAgent) {
		t.Errorf("%q", userAgent)
	}
	if !strings.Contains(userAgent, "(go "+strings.Join(strings.SplitN(strings.TrimPrefix(runtime.Version(), "go"), ".", 3)[:2], ".")+")") {
		t.Errorf("%q does not name %s", userAgent, runtime.Version())
	}
	for v, want := range map[string]string{"go1.26.9": "1.26", "go1.27.0": "1.27", "go1.28rc1": "1.28", "devel go1.28-abc": "devel"} {
		if got := goVersion(v); got != want {
			t.Errorf("%s: %s, want %s", v, got, want)
		}
	}
}

func TestTheKeyIsNeverShown(t *testing.T) {
	c := testClient(t, record())
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x"} {
		if got := fmt.Sprintf(verb, c); strings.Contains(got, testKey) || got != "haau3.Client(https://api.haau3.com)" {
			t.Errorf("%s: %s", verb, got)
		}
	}
	// Even a printer that reads fields itself sees only a function.
	if got := fmt.Sprintf("%+v", *c); strings.Contains(got, testKey) {
		t.Errorf("%s", got)
	}
}

func TestRequestRefusesAnotherHostAndSendsNothing(t *testing.T) {
	rec := record(fhirReply(200, plan))
	_, err := testClient(t, rec).Request(context.Background(), RawRequest{Method: "GET", Path: "https://evil.example/v1/scheduling/publishers"})
	if e := asError(t, err); e.Kind != KindNotSent || !strings.Contains(e.Error(), "refusing to send the API key to https://evil.example") {
		t.Errorf("%v", err)
	}
	if rec.count() != 0 {
		t.Errorf("%d requests", rec.count())
	}
}

func page(next string, ids ...string) map[string]any {
	entries := []any{}
	for _, id := range ids {
		entries = append(entries, map[string]any{"resource": map[string]any{"resourceType": "InsurancePlan", "id": id}, "search": map[string]any{"mode": "match"}})
	}
	b := map[string]any{"resourceType": "Bundle", "type": "searchset", "entry": entries}
	if next != "" {
		b["link"] = []any{map[string]any{"relation": "self", "url": "https://api.haau3.com/v1/fhir/r4/InsurancePlan"}, map[string]any{"relation": "next", "url": next}}
	}
	return b
}

func firstPage(t *testing.T, raw map[string]any) *fhir.Bundle {
	t.Helper()
	data, _ := jsonBytes(raw)
	b, err := decodeAs[fhir.Bundle](data)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPagesFollowsNextLinks(t *testing.T) {
	rec := record(fhirReply(200, page("https://api.haau3.com/v1/fhir/r4/InsurancePlan?page=3", "B")), fhirReply(200, page("", "C")))
	c := testClient(t, rec)
	var ids []string
	for p, err := range c.Pages(context.Background(), firstPage(t, page("https://api.haau3.com/v1/fhir/r4/InsurancePlan?page=2", "A"))) {
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, *p.Entry[0].Resource.(*fhir.InsurancePlan).ID)
	}
	if strings.Join(ids, ",") != "A,B,C" || rec.count() != 2 || rec.requests[0].URL.RawQuery != "page=2" {
		t.Errorf("%v, %d requests", ids, rec.count())
	}
}

func TestPagesRefusesANextLinkToAnotherHost(t *testing.T) {
	rec := record()
	var errs []error
	for _, err := range testClient(t, rec).Pages(context.Background(), firstPage(t, page("https://evil.example/next", "A"))) {
		errs = append(errs, err)
	}
	if len(errs) != 2 || errs[0] != nil || !errors.Is(errs[1], ErrNotSent) || rec.count() != 0 {
		t.Errorf("%v, %d requests", errs, rec.count())
	}
}

func TestPagesStopsAtANextLinkItAlreadyFollowed(t *testing.T) {
	loop := "https://api.haau3.com/v1/fhir/r4/InsurancePlan?page=2"
	rec := record(fhirReply(200, page(loop, "B")))
	var last error
	n := 0
	for _, err := range testClient(t, rec).Pages(context.Background(), firstPage(t, page(loop, "A"))) {
		n++
		last = err
	}
	e := asError(t, last)
	if n != 3 || rec.count() != 1 || e.Reason != ReasonUnreadable {
		t.Errorf("%d items, %d requests, %#v", n, rec.count(), e)
	}
	if e.Error() != "GET /v1/fhir/r4/InsurancePlan answered with a next link to a page already read, so paging stopped." {
		t.Errorf("%q", e.Error())
	}
}

func TestEachPageIsCheckedAsABundle(t *testing.T) {
	rec := record(fhirReply(200, plan))
	var last error
	for _, err := range testClient(t, rec).Pages(context.Background(), firstPage(t, page("https://api.haau3.com/v1/fhir/r4/InsurancePlan?page=2", "A"))) {
		last = err
	}
	if e := asError(t, last); e.Error() != "GET /v1/fhir/r4/InsurancePlan answered an InsurancePlan where a Bundle was expected." {
		t.Errorf("%q", e.Error())
	}
}

func TestBreakingOutOfPagesSendsNoMore(t *testing.T) {
	rec := record(fhirReply(200, page("", "B")))
	for _, err := range testClient(t, rec).Pages(context.Background(), firstPage(t, page("https://api.haau3.com/v1/fhir/r4/InsurancePlan?page=2", "A"))) {
		if err != nil {
			t.Fatal(err)
		}
		break
	}
	if rec.count() != 0 {
		t.Errorf("%d requests", rec.count())
	}
}

func TestResourcesAreTheMatchesOfEveryPage(t *testing.T) {
	second := page("", "C")
	second["entry"] = append(second["entry"].([]any),
		map[string]any{"resource": map[string]any{"resourceType": "Organization", "id": "included"}, "search": map[string]any{"mode": "include"}},
		map[string]any{"resource": map[string]any{"resourceType": "OperationOutcome", "id": "outcome"}, "search": map[string]any{"mode": "outcome"}},
		map[string]any{"resource": map[string]any{"resourceType": "InsurancePlan", "id": "D"}},
		map[string]any{"search": map[string]any{"mode": "match"}},
	)
	rec := record(fhirReply(200, second))
	var ids []string
	for r, err := range testClient(t, rec).Resources(context.Background(), firstPage(t, page("https://api.haau3.com/v1/fhir/r4/InsurancePlan?page=2", "A", "B"))) {
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, r.ResourceType()+"/"+*r.(*fhir.InsurancePlan).ID)
	}
	if strings.Join(ids, ",") != "InsurancePlan/A,InsurancePlan/B,InsurancePlan/C,InsurancePlan/D" {
		t.Errorf("%v", ids)
	}
}

func TestResourcesEndsWithAnError(t *testing.T) {
	rec := record(fhirReply(404, outcome))
	var got []string
	for r, err := range testClient(t, rec).Resources(context.Background(), firstPage(t, page("https://api.haau3.com/v1/fhir/r4/InsurancePlan?page=2", "A"))) {
		if err != nil {
			got = append(got, "error:"+asError(t, err).Kind.String())
			continue
		}
		got = append(got, *r.(*fhir.InsurancePlan).ID)
	}
	if strings.Join(got, ",") != "A,error:refused" {
		t.Errorf("%v", got)
	}
}

func TestOneClientServesManyGoroutines(t *testing.T) {
	rec := record(fhirReply(200, plan))
	c := testClient(t, rec)
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			if _, err := c.ReadInsurancePlan(context.Background(), "H1"); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if rec.count() != 32 {
		t.Errorf("%d requests", rec.count())
	}
}

func TestCorrelationIDIsSentAndComesBack(t *testing.T) {
	id := "0f8fad5b-d9cb-469f-a165-70867728950e"
	rec := record(reply{status: 200, body: plan, header: map[string]string{"X-Correlation-Id": id}})
	c := testClient(t, rec)
	var info ResponseInfo
	if _, err := c.ReadInsurancePlan(context.Background(), "H1", WithHeader("X-Request-Id", "raw"), WithCorrelationID(id), WithResponseInfo(&info)); err != nil {
		t.Fatal(err)
	}
	if rec.last().Header.Get("X-Request-Id") != id || info.CorrelationID != id {
		t.Errorf("%v %+v", rec.last().Header, info)
	}
	// On every error the API answered too.
	rec = record(reply{status: 404, body: outcome, header: map[string]string{"X-Correlation-Id": id}})
	_, err := testClient(t, rec).ReadInsurancePlan(context.Background(), "H1", WithCorrelationID(strings.ToUpper(id)))
	if e := asError(t, err); e.CorrelationID != id {
		t.Errorf("%#v", e)
	}
}

func TestAnythingButAUUIDIsRefusedBeforeSending(t *testing.T) {
	rec := record(fhirReply(200, plan))
	c := testClient(t, rec)
	for _, bad := range []string{"", "job-42", "0f8fad5b-d9cb-469f-a165-70867728950", "0f8fad5bd9cb469fa16570867728950e", "{0f8fad5b-d9cb-469f-a165-70867728950e}"} {
		_, err := c.ReadInsurancePlan(context.Background(), "H1", WithCorrelationID(bad))
		want := "GET /v1/fhir/r4/InsurancePlan/{id} was not sent: WithCorrelationID takes a UUID, which is all the API keeps (it drops anything else)."
		if e := asError(t, err); e.Kind != KindNotSent || e.Error() != want {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if rec.count() != 0 {
		t.Errorf("%d requests", rec.count())
	}
}

func TestRequestReturnsAnyStatusWithItsText(t *testing.T) {
	rec := record(reply{status: 404, body: outcome})
	raw, err := testClient(t, rec).Request(context.Background(), RawRequest{Method: "GET", Path: "/v1/fhir/r4/InsurancePlan/x", Query: url.Values{"_elements": {"id,name"}}})
	if err != nil {
		t.Fatal(err)
	}
	if raw.Status != 404 || raw.StatusText != "Not Found" || raw.RequestID != requestID || raw.Attempts != 1 || !strings.Contains(string(raw.Body()), "not-found") {
		t.Errorf("%+v", raw)
	}
	if rec.last().URL.RawQuery != "_elements=id%2Cname" || rec.last().Header.Get("Accept") != "application/fhir+json" {
		t.Errorf("%s %v", rec.last().URL.RawQuery, rec.last().Header)
	}
	if fmt.Sprintf("%v %+v %#v", raw, raw, raw) != "haau3.RawResponse(404 Not Found) haau3.RawResponse(404 Not Found) haau3.RawResponse(404 Not Found)" {
		t.Errorf("%v", raw)
	}
}

func TestRequestSendsAStringAsItIsAndAnythingElseAsJSON(t *testing.T) {
	rec := record(plainReply(200, map[string]any{}))
	c := testClient(t, rec)
	_, _ = c.Request(context.Background(), RawRequest{Method: "POST", Path: "/v1/x", Body: `{"as":"it is"}`})
	if string(rec.lastBody()) != `{"as":"it is"}` || rec.last().Header.Get("Content-Type") != "application/json" {
		t.Errorf("%s %v", rec.lastBody(), rec.last().Header)
	}
	_, _ = c.Request(context.Background(), RawRequest{Method: "POST", Path: "/v1/fhir/r4", Body: map[string]any{"resourceType": "Bundle", "a": "<b>"}})
	if string(rec.lastBody()) != `{"a":"<b>","resourceType":"Bundle"}` || rec.last().Header.Get("Content-Type") != "application/fhir+json" {
		t.Errorf("%s %v", rec.lastBody(), rec.last().Header)
	}
	if _, err := c.Request(context.Background(), RawRequest{Method: "DELETE", Path: "/v1/x"}); err == nil {
		t.Error("DELETE was sent")
	}
}

func TestMetadataAnswersEitherOfItsTwoTypes(t *testing.T) {
	for _, t2 := range []string{"CapabilityStatement", "TerminologyCapabilities"} {
		r, err := testClient(t, record(fhirReply(200, map[string]any{"resourceType": t2}))).FHIRMetadata(context.Background(), FHIRMetadataParams{})
		if err != nil || r.ResourceType() != t2 {
			t.Errorf("%v %v", r, err)
		}
	}
}

func TestReconcileMedicationsTakesABundleOrParametersOnly(t *testing.T) {
	rec := record(fhirReply(200, map[string]any{"resourceType": "Bundle", "type": "collection"}))
	c := testClient(t, rec)
	if _, err := c.ReconcileMedications(context.Background(), &fhir.Bundle{Type: new("collection")}); err != nil {
		t.Error(err)
	}
	_, err := c.ReconcileMedications(context.Background(), &fhir.Patient{})
	if e := asError(t, err); e.Error() != "POST /v1/fhir/r4/$reconcile-medications was not sent: it takes a Bundle or a Parameters as its body, not a Patient." {
		t.Errorf("%q", err)
	}
	if _, err := c.ReconcileMedications(context.Background(), nil); !errors.Is(err, ErrNotSent) {
		t.Errorf("%v", err)
	}
	if rec.count() != 1 {
		t.Errorf("%d requests", rec.count())
	}
}
