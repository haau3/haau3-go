package haau3

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/haau3/haau3-go/fhir"
)

// Get and Post: any operation by its path, judged as every generated method's answer is. Each value is
// encoded into the path, a message names the path as written and never a value, and a path that does
// not match its values is refused before anything is sent.

const (
	practitionerPath = "/v1/fhir/r4/Practitioner/{id}"
	// pathValue is a value a message must never repeat: what a caller put in the path.
	pathValue = "VALUE-NEVER-IN-A-MESSAGE"
)

func outcomeWith(code, apiCode string) map[string]any {
	details := map[string]any{"text": "The API's own words."}
	if apiCode != "" {
		details["coding"] = []map[string]string{{"system": "https://haau3.com/fhir/CodeSystem/api-error", "code": apiCode}}
	}
	return map[string]any{"resourceType": "OperationOutcome", "issue": []map[string]any{{"severity": "error", "code": code, "details": details}}}
}

// printedError is every way an error commonly reaches a log line.
func printedError(err error) string {
	data, _ := json.Marshal(err)
	return strings.Join([]string{err.Error(), fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err), string(data)}, "\n")
}

func TestGetSendsThePathWithEachValueEncodedTheQueryAndTheKey(t *testing.T) {
	rec := record(fhirReply(200, map[string]any{"resourceType": "Practitioner", "id": "x"}))
	c := testClient(t, rec)
	var practitioner fhir.Practitioner
	err := c.Get(context.Background(), practitionerPath, CallParams{
		PathParams: map[string]string{"id": "a b/c"},
		Query:      url.Values{"npi": {"1", "2"}, "_format": {"json"}},
	}, &practitioner)
	if err != nil {
		t.Fatal(err)
	}
	if practitioner.ID == nil || *practitioner.ID != "x" {
		t.Fatalf("decoded %+v", practitioner)
	}
	sent := rec.last()
	if sent.Method != "GET" {
		t.Fatalf("method %s", sent.Method)
	}
	if got := sent.URL.EscapedPath(); got != "/v1/fhir/r4/Practitioner/a%20b%2Fc" {
		t.Fatalf("path %s", got)
	}
	if got := sent.URL.Query()["npi"]; strings.Join(got, ",") != "1,2" {
		t.Fatalf("npi %v", got)
	}
	if got := sent.Header.Get("Authorization"); got != "Bearer "+markerKey {
		t.Fatalf("authorization %q", got)
	}
	if got := sent.Header.Get("Accept"); got != "application/fhir+json" {
		t.Fatalf("accept %q", got)
	}
	if len(rec.lastBody()) != 0 {
		t.Fatalf("a GET sent a body")
	}
}

func TestGetTakesAPathWithNoNameAndJSONOutsideTheFHIRBase(t *testing.T) {
	rec := record(plainReply(200, map[string]any{"publishers": []any{}}))
	var got map[string]any
	if err := testClient(t, rec).Get(context.Background(), "/v1/scheduling/publishers", CallParams{}, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["publishers"]; !ok {
		t.Fatalf("decoded %v", got)
	}
	if accept := rec.last().Header.Get("Accept"); accept != "application/json" {
		t.Fatalf("accept %q", accept)
	}
}

func TestGetWithANilResReadsNothingAndWithResponseInfoFillsIn(t *testing.T) {
	rec := record(fhirReply(200, map[string]any{"resourceType": "CapabilityStatement"}))
	var info ResponseInfo
	err := testClient(t, rec).Get(context.Background(), "/v1/fhir/r4/metadata", CallParams{Expects: []string{"CapabilityStatement"}}, nil, WithResponseInfo(&info))
	if err != nil {
		t.Fatal(err)
	}
	if info.RequestID != requestID || info.Status != 200 || info.Attempts != 1 {
		t.Fatalf("info %+v", info)
	}
}

func TestGetPassesEveryPerCallOptionOn(t *testing.T) {
	const correlation = "0b6f2d4e-8c1a-4e3b-9f5d-2a7c6e1b4d90"
	rec := record(fhirReply(503, outcomeWith("transient", "")))
	err := testClient(t, rec).Get(context.Background(), "/v1/fhir/r4/metadata", CallParams{}, nil,
		WithCorrelationID(correlation), WithHeader("X-Extra", "1"), WithMaxRetries(0))
	if err == nil {
		t.Fatal("a 503 was not an error")
	}
	if rec.count() != 1 || rec.last().Header.Get("X-Request-Id") != correlation || rec.last().Header.Get("X-Extra") != "1" {
		t.Fatalf("%d requests, headers %v", rec.count(), rec.last().Header)
	}
}

func TestGetSendsNoKeyWithNoneInTheSandbox(t *testing.T) {
	rec := record(fhirReply(200, map[string]any{"resourceType": "CapabilityStatement"}))
	c := testClient(t, rec, WithAPIKey(""), WithEnvironmentSandbox())
	if err := c.Get(context.Background(), "/v1/fhir/r4/metadata", CallParams{}, nil); err != nil {
		t.Fatal(err)
	}
	if host := rec.last().URL.Host; host != "sandbox.haau3.com" {
		t.Fatalf("host %s", host)
	}
	if auth := rec.last().Header.Get("Authorization"); auth != "" {
		t.Fatalf("sent a key with none")
	}
}

func TestPostSendsTheBodyAsJSONOrAsItIs(t *testing.T) {
	rec := record(fhirReply(200, map[string]any{"resourceType": "Parameters"}))
	c := testClient(t, rec)
	body := map[string]any{"resourceType": "Parameters", "parameter": []any{}}
	var got fhir.Parameters
	if err := c.Post(context.Background(), "/v1/fhir/r4/$structure-dosage", CallParams{Body: body, Expects: []string{"Parameters"}}, &got); err != nil {
		t.Fatal(err)
	}
	if rec.last().Method != "POST" || rec.last().URL.Path != "/v1/fhir/r4/$structure-dosage" {
		t.Fatalf("%s %s", rec.last().Method, rec.last().URL.Path)
	}
	if ct := rec.last().Header.Get("Content-Type"); ct != "application/fhir+json" {
		t.Fatalf("content type %q", ct)
	}
	var sent map[string]any
	if err := json.Unmarshal(rec.lastBody(), &sent); err != nil || sent["resourceType"] != "Parameters" {
		t.Fatalf("body %s", rec.lastBody())
	}

	if err := c.Post(context.Background(), "/v1/fhir/r4", CallParams{Body: `{"resourceType":"Bundle"}`}, nil); err != nil {
		t.Fatal(err)
	}
	if string(rec.lastBody()) != `{"resourceType":"Bundle"}` {
		t.Fatalf("a string body was not sent as it is: %s", rec.lastBody())
	}
}

func TestGetAndPostReturnTheErrorsEveryOperationsOwnMethodReturns(t *testing.T) {
	cases := []struct {
		name   string
		reply  reply
		kind   Kind
		reason Reason
	}{
		{"401", fhirReply(401, outcomeWith("login", "")), KindAuth, ""},
		{"403", fhirReply(403, outcomeWith("forbidden", "")), KindForbidden, ""},
		{"404", fhirReply(404, outcomeWith("not-found", "")), KindRefused, ""},
		{"429", reply{status: 429, body: outcomeWith("throttled", ""), header: map[string]string{"Retry-After": "12"}}, KindNoAnswer, ReasonRateLimited},
		{"503", fhirReply(503, outcomeWith("transient", "")), KindNoAnswer, ReasonUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := testClient(t, record(tc.reply)).Get(context.Background(), practitionerPath,
				CallParams{PathParams: map[string]string{"id": pathValue}}, nil, WithMaxRetries(0))
			e := asError(t, err)
			if e.Kind != tc.kind || e.Reason != tc.reason || e.RequestID != requestID {
				t.Fatalf("kind %v, reason %q, request id %q", e.Kind, e.Reason, e.RequestID)
			}
			if tc.name == "429" && e.RetryAfter != 12*time.Second {
				t.Fatalf("retry after %v", e.RetryAfter)
			}
			if !strings.Contains(e.Error(), practitionerPath) {
				t.Fatalf("the message does not name the path as written: %s", e.Error())
			}
			if printed := printedError(err); strings.Contains(printed, pathValue) || strings.Contains(printed, markerKey) {
				t.Fatalf("a printed error repeats a value or the key")
			}
		})
	}
}

func TestA403CodedNotOpenToCustomersSaysSo(t *testing.T) {
	err := testClient(t, record(fhirReply(403, outcomeWith("forbidden", "not-open-to-customers")))).
		Post(context.Background(), "/v1/fhir/r4/$process-message", CallParams{Body: map[string]any{}}, nil)
	e := asError(t, err)
	if e.Kind != KindForbidden || e.Code != "not-open-to-customers" {
		t.Fatalf("kind %v, code %q", e.Kind, e.Code)
	}
	if want := "POST /v1/fhir/r4/$process-message was refused: the operation is not yet open to customers (403)."; e.Error() != want {
		t.Fatalf("message %q", e.Error())
	}
}

func TestAnAnswerOfAnotherTypeThanExpectsNamesIsUnreadable(t *testing.T) {
	err := testClient(t, record(fhirReply(200, map[string]any{"resourceType": "OperationOutcome"}))).Get(context.Background(),
		practitionerPath, CallParams{PathParams: map[string]string{"id": "x"}, Expects: []string{"Practitioner"}}, nil)
	if e := asError(t, err); e.Kind != KindNoAnswer || e.Reason != ReasonUnreadable {
		t.Fatalf("kind %v, reason %q", e.Kind, e.Reason)
	}
	// Without Expects, any answer is taken (the control).
	var got map[string]any
	if err := testClient(t, record(fhirReply(200, map[string]any{"resourceType": "OperationOutcome"}))).Get(context.Background(),
		practitionerPath, CallParams{PathParams: map[string]string{"id": "x"}}, &got); err != nil {
		t.Fatal(err)
	}
}

func TestWhatDoesNotMatchIsRefusedBeforeAnythingIsSent(t *testing.T) {
	var notAPointer fhir.Practitioner
	cases := []struct {
		name    string
		method  string
		path    string
		params  CallParams
		res     any
		message string
	}{
		{"a {name} with no value", "GET", practitionerPath, CallParams{}, nil, `PathParams has no value for \{id\}`},
		{"a value with no {name}", "GET", "/v1/fhir/r4/metadata", CallParams{PathParams: map[string]string{"id": pathValue}}, nil, `the path has no \{id\} for PathParams' value`},
		{"an empty value", "GET", practitionerPath, CallParams{PathParams: map[string]string{"id": ""}}, nil, `its path value id is empty`},
		{"a body on a GET", "GET", "/v1/fhir/r4/metadata", CallParams{Body: "{}"}, nil, `a GET takes no body`},
		{"a res that is not a pointer", "GET", "/v1/fhir/r4/metadata", CallParams{}, notAPointer, `res must be a pointer`},
		{"a URL on another host", "GET", "https://elsewhere.example/v1/fhir/r4/metadata", CallParams{}, nil, `elsewhere\.example`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := record(fhirReply(200, map[string]any{"resourceType": "Practitioner"}))
			c := testClient(t, rec)
			var err error
			if tc.method == "GET" {
				err = c.Get(context.Background(), tc.path, tc.params, tc.res)
			} else {
				err = c.Post(context.Background(), tc.path, tc.params, tc.res)
			}
			e := asError(t, err)
			if e.Kind != KindNotSent {
				t.Fatalf("kind %v", e.Kind)
			}
			if !regexp.MustCompile(tc.message).MatchString(e.Error()) {
				t.Fatalf("message %q", e.Error())
			}
			if strings.Contains(printedError(err), pathValue) {
				t.Fatalf("a printed error repeats a value")
			}
			if rec.count() != 0 {
				t.Fatalf("%d requests were sent", rec.count())
			}
		})
	}
}
