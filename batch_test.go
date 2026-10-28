package haau3

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/haau3/haau3-go/fhir"
)

func batchResponse(entries ...any) map[string]any {
	return map[string]any{"resourceType": "Bundle", "type": "batch-response", "entry": entries}
}

func TestTheBundleSentAndTheResultsInOrder(t *testing.T) {
	rec := record(fhirReply(200, batchResponse(
		map[string]any{"resource": plan, "response": map[string]any{"status": "200 OK"}},
		map[string]any{"resource": map[string]any{"resourceType": "Parameters"}, "response": map[string]any{"status": "200"}},
		map[string]any{"response": map[string]any{"status": "404 Not Found", "outcome": outcome}},
	)))
	p := Calls.ReadInsurancePlan("H1")
	lookup := Calls.CodeSystemLookup(CodeSystemLookupParams{System: new("http://loinc.org"), Code: "2345-7", Property: []string{"CLASSTYPE"}})
	practitioner := Calls.ReadPractitioner("a/b c")
	if err := testClient(t, rec).Batch(context.Background(), []Entry{p, lookup, practitioner}); err != nil {
		t.Fatal(err)
	}
	req := rec.last()
	if req.Method != "POST" || req.URL.Path != "/v1/fhir/r4" || req.Header.Get("Content-Type") != "application/fhir+json" {
		t.Errorf("%s %s %v", req.Method, req.URL.Path, req.Header)
	}
	want := `{"resourceType":"Bundle","type":"batch","entry":[` +
		`{"request":{"method":"GET","url":"InsurancePlan/H1"}},` +
		`{"request":{"method":"GET","url":"CodeSystem/$lookup?code=2345-7&system=http%3A%2F%2Floinc.org&property=CLASSTYPE"}},` +
		`{"request":{"method":"GET","url":"Practitioner/a%2Fb%20c"}}]}`
	if string(rec.lastBody()) != want {
		t.Errorf("sent %s", rec.lastBody())
	}
	if got, err := p.Result(); err != nil || *got.ID != "H1" {
		t.Errorf("%v %v", got, err)
	}
	if got, err := lookup.Result(); err != nil || got == nil {
		t.Errorf("%v %v", got, err)
	}
	_, err := practitioner.Result()
	e := asError(t, err)
	if e.Kind != KindRefused || e.Status != 404 || e.Outcome() == nil || e.RequestID != requestID {
		t.Errorf("%#v", e)
	}
	if e.Error() != "GET /v1/fhir/r4/Practitioner/{id} (entry 3 of 3) was refused (404)." {
		t.Errorf("%q", e.Error())
	}
}

func TestAPostEntryCarriesItsBodyAsTheResource(t *testing.T) {
	rec := record(fhirReply(200, batchResponse(map[string]any{"resource": map[string]any{"resourceType": "Parameters"}, "response": map[string]any{"status": "200"}})))
	body := &fhir.Parameters{Parameter: []fhir.ParametersParameter{{Name: new("code"), ValueCode: new("2345-7")}}}
	if err := testClient(t, rec).Batch(context.Background(), []Entry{Calls.CodeSystemLookupPost(body)}); err != nil {
		t.Fatal(err)
	}
	want := `{"resourceType":"Bundle","type":"batch","entry":[{"request":{"method":"POST","url":"CodeSystem/$lookup"},` +
		`"resource":{"resourceType":"Parameters","parameter":[{"name":"code","valueCode":"2345-7"}]}}]}`
	if string(rec.lastBody()) != want {
		t.Errorf("sent %s", rec.lastBody())
	}
}

func TestEachEntryIsJudgedAsTheCallAloneIs(t *testing.T) {
	for _, c := range []struct {
		status string
		body   any
		kind   Kind
		reason Reason
	}{
		{"201 Created", plan, 0, ""},
		{"401", outcome, KindRefused, ""},
		{"403", nil, KindRefused, ""},
		{"410", nil, KindRefused, ""},
		{"429", nil, KindNoAnswer, ReasonRateLimited},
		{"500", nil, KindNoAnswer, ReasonServerError},
		{"503", nil, KindNoAnswer, ReasonUnavailable},
		{"302", nil, KindNoAnswer, ReasonUnreadable},
		{"200", nil, KindNoAnswer, ReasonUnreadable},
		{"200", map[string]any{"resourceType": "Bundle"}, KindNoAnswer, ReasonUnreadable},
	} {
		entry := map[string]any{"response": map[string]any{"status": c.status}}
		if c.body != nil {
			entry["resource"] = c.body
		}
		rec := record(fhirReply(200, batchResponse(entry)))
		p := Calls.ReadInsurancePlan("H1")
		if err := testClient(t, rec).Batch(context.Background(), []Entry{p}); err != nil {
			t.Fatal(err)
		}
		_, err := p.Result()
		if c.kind == 0 {
			if err != nil {
				t.Errorf("%s: %v", c.status, err)
			}
			continue
		}
		e := asError(t, err)
		if e.Kind != c.kind || e.Reason != c.reason || rec.count() != 1 {
			t.Errorf("%s: %#v, %d requests", c.status, e, rec.count())
		}
	}
}

func TestAnEntryWithNoStatusIsUnreadable(t *testing.T) {
	rec := record(fhirReply(200, batchResponse(map[string]any{"resource": plan}, map[string]any{"resource": plan, "response": map[string]any{"status": "OK"}})))
	a, b := Calls.ReadInsurancePlan("A"), Calls.ReadInsurancePlan("B")
	if err := testClient(t, rec).Batch(context.Background(), []Entry{a, b}); err != nil {
		t.Fatal(err)
	}
	for n, call := range []*Call[*fhir.InsurancePlan]{a, b} {
		_, err := call.Result()
		if want := fmt.Sprintf("GET /v1/fhir/r4/InsurancePlan/{id} (entry %d of 2) answered with no status.", n+1); err == nil || err.Error() != want {
			t.Errorf("%v", err)
		}
	}
}

func TestAnAnswerThatCannotBePairedIsNoAnswerForTheWholeBatch(t *testing.T) {
	for _, c := range []struct {
		body any
		said string
	}{
		{map[string]any{"resourceType": "Bundle", "type": "searchset", "entry": []any{}}, "POST /v1/fhir/r4 answered a Bundle that is not a batch-response."},
		{batchResponse(), "POST /v1/fhir/r4 answered 0 entries for a batch of 1."},
		{batchResponse(map[string]any{}, map[string]any{}), "POST /v1/fhir/r4 answered 2 entries for a batch of 1."},
		{map[string]any{"resourceType": "Bundle", "type": "batch-response", "entry": map[string]any{"not": "a list"}}, "POST /v1/fhir/r4 answered a batch-response whose entries are not a list."},
	} {
		p := Calls.ReadInsurancePlan("H1")
		err := testClient(t, record(fhirReply(200, c.body))).Batch(context.Background(), []Entry{p})
		e := asError(t, err)
		if e.Error() != c.said || e.Reason != ReasonUnreadable || e.Status != 200 {
			t.Errorf("%#v %q", e, e.Error())
		}
		// Every entry carries the batch's error too.
		if _, entryErr := p.Result(); !errors.Is(entryErr, err) {
			t.Errorf("the entry says %v", entryErr)
		}
	}
}

func TestTheBatchItselfIsJudgedAndRetriedAsAnyCall(t *testing.T) {
	if err := testClient(t, record(fhirReply(401, nil))).Batch(context.Background(), []Entry{Calls.ReadInsurancePlan("H1")}); !errors.Is(err, ErrAuth) {
		t.Errorf("%v", err)
	}
	if err := testClient(t, record(fhirReply(403, nil))).Batch(context.Background(), []Entry{Calls.ReadInsurancePlan("H1")}); !errors.Is(err, ErrForbidden) || errors.Is(err, ErrAuth) {
		t.Errorf("%v", err)
	}
	if err := testClient(t, record(fhirReply(413, nil))).Batch(context.Background(), []Entry{Calls.ReadInsurancePlan("H1")}); !errors.Is(err, ErrRefused) {
		t.Errorf("%v", err)
	}
	rec := record(fhirReply(503, nil), fhirReply(200, batchResponse(map[string]any{"resource": plan, "response": map[string]any{"status": "200"}})))
	var info ResponseInfo
	if err := testClient(t, rec).Batch(context.Background(), []Entry{Calls.ReadInsurancePlan("H1")}, WithResponseInfo(&info)); err != nil || rec.count() != 2 || info.Attempts != 2 {
		t.Errorf("%v %d %+v", err, rec.count(), info)
	}
}

func TestAnEmptyBatch(t *testing.T) {
	rec := record(fhirReply(200, batchResponse()))
	if err := testClient(t, rec).Batch(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if string(rec.lastBody()) != `{"resourceType":"Bundle","type":"batch","entry":[]}` {
		t.Errorf("%s", rec.lastBody())
	}
}

func TestAnEntryThatCannotBeSentIsRefusedBeforeSending(t *testing.T) {
	rec := record(fhirReply(200, batchResponse()))
	c := testClient(t, rec)
	for said, entries := range map[string][]Entry{
		"POST /v1/fhir/r4 was not sent: entry 2 of the batch was not made by haau3.Calls.":                                                          {Calls.ReadInsurancePlan("H1"), nil},
		"POST /v1/fhir/r4 was not sent: entry 1 of the batch, GET /v1/fhir/r4/CodeSystem/$lookup, cannot be: its query parameter code is required.": {Calls.CodeSystemLookup(CodeSystemLookupParams{})},
		"POST /v1/fhir/r4 was not sent: entry 1 of the batch, POST /v1/fhir/r4/CodeSystem/$lookup, cannot be: it takes a body, and was given none.": {Calls.CodeSystemLookupPost(nil)},
	} {
		err := c.Batch(context.Background(), entries)
		if e := asError(t, err); e.Kind != KindNotSent || e.Error() != said {
			t.Errorf("%q", err)
		}
	}
	if rec.count() != 0 {
		t.Errorf("%d requests", rec.count())
	}
}

func TestACallNamesItsOperationNeverAValue(t *testing.T) {
	c := Calls.CodeSystemLookupPost(&fhir.Parameters{ID: new("PRIVATE-VALUE")})
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		if got := fmt.Sprintf(verb, c); got != "haau3.Call(POST /v1/fhir/r4/CodeSystem/$lookup)" {
			t.Errorf("%s: %s", verb, got)
		}
	}
	if _, err := c.Result(); err == nil || err.Error() != "POST /v1/fhir/r4/CodeSystem/$lookup has not been sent: pass it to Client.Batch first." {
		t.Errorf("%v", err)
	}
}

func TestAnEntryThatDoesNotReadAsItsTypeSaysWhichEntry(t *testing.T) {
	rec := record(fhirReply(200, batchResponse(map[string]any{"resource": map[string]any{"resourceType": "InsurancePlan", "name": 7}, "response": map[string]any{"status": "200"}})))
	p := Calls.ReadInsurancePlan("H1")
	if err := testClient(t, rec).Batch(context.Background(), []Entry{p}); err != nil {
		t.Fatal(err)
	}
	_, err := p.Result()
	if err == nil || err.Error() != "GET /v1/fhir/r4/InsurancePlan/{id} (entry 1 of 1) answered an InsurancePlan that could not be read: name is not a string." {
		t.Errorf("%v", err)
	}
}
