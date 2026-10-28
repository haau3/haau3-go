package haau3

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/haau3/haau3-go/fhir"
)

var (
	outcome = map[string]any{"resourceType": "OperationOutcome", "issue": []any{map[string]any{"severity": "error", "code": "not-found"}}}
	problem = map[string]any{
		"type": "https://docs.haau3.com/errors#invalid-request", "title": "Invalid request", "status": 400,
		"detail": "d", "instance": "urn:uuid:x",
	}
)

func readPlan(t *testing.T, replies ...reply) error {
	t.Helper()
	_, err := testClient(t, record(replies...)).ReadInsurancePlan(context.Background(), "secret-id")
	if err == nil {
		t.Fatal("no error")
	}
	return err
}

func TestA401Or403IsAnAuthError(t *testing.T) {
	for _, status := range []int{401, 403} {
		e := asError(t, readPlan(t, fhirReply(status, outcome)))
		if e.Kind != KindAuth || e.Status != status || e.RequestID != requestID || !errors.Is(e, ErrAuth) {
			t.Errorf("%d: %#v", status, e)
		}
		why := "the API key was not accepted"
		if status == 403 {
			// A 403 means the key was accepted: there is nothing to rotate.
			why = "the API key was accepted, and its organization may not make this request"
		}
		if want := fmt.Sprintf("GET /v1/fhir/r4/InsurancePlan/{id} was refused: %s (%d).", why, status); e.Error() != want {
			t.Errorf("%q", e.Error())
		}
		if e.Outcome() == nil || *e.Outcome().Issue[0].Code != "not-found" {
			t.Errorf("no outcome")
		}
	}
}

func TestAnother4xxIsARefusalCarryingTheOutcome(t *testing.T) {
	for _, status := range []int{400, 404, 410, 413, 422} {
		e := asError(t, readPlan(t, fhirReply(status, outcome)))
		if e.Kind != KindRefused || e.Status != status || e.Attempts != 1 || e.Outcome() == nil || e.Problem() != nil {
			t.Errorf("%d: %#v", status, e)
		}
		if want := fmt.Sprintf("GET /v1/fhir/r4/InsurancePlan/{id} was refused (%d).", status); e.Error() != want || strings.Contains(e.Error(), "secret-id") {
			t.Errorf("%q", e.Error())
		}
	}
}

func TestSchedulingErrorsCarryAProblem(t *testing.T) {
	_, err := testClient(t, record(plainReply(400, problem))).SearchSlots(context.Background(), SearchSlotsParams{State: new("ZZ")})
	e := asError(t, err)
	if e.Kind != KindRefused || e.Problem() == nil || e.Problem().Detail != "d" || e.Outcome() != nil {
		t.Errorf("%#v", e)
	}
	if e.Error() != "GET /v1/scheduling/slots was refused (400)." {
		t.Errorf("%q", e.Error())
	}
}

func TestA429Or5xxIsNoAnswer(t *testing.T) {
	for status, reason := range map[int]Reason{429: ReasonRateLimited, 500: ReasonServerError, 501: ReasonServerError, 502: ReasonUnavailable, 503: ReasonUnavailable, 504: ReasonTimeout} {
		e := asError(t, readPlan(t, fhirReply(status, outcome)))
		if e.Kind != KindNoAnswer || e.Reason != reason || e.Status != status || e.Attempts != 2 || e.Outcome() == nil || !errors.Is(e, ErrNoAnswer) {
			t.Errorf("%d: %#v", status, e)
		}
		if want := fmt.Sprintf("GET /v1/fhir/r4/InsurancePlan/{id} got no answer (%s, %d) after 2 attempts.", reason, status); e.Error() != want {
			t.Errorf("%q", e.Error())
		}
	}
}

func TestA2xxThatIsNotJSONIsUnreadable(t *testing.T) {
	e := asError(t, readPlan(t, reply{status: 200, body: "<html>", header: map[string]string{"Content-Type": "text/html"}}))
	if e.Kind != KindNoAnswer || e.Reason != ReasonUnreadable || e.Status != 200 {
		t.Errorf("%#v", e)
	}
	if e.Error() != "GET /v1/fhir/r4/InsurancePlan/{id} answered 200 with a body that is not JSON." {
		t.Errorf("%q", e.Error())
	}
	if e := asError(t, readPlan(t, fhirReply(200, "{not json"))); e.Reason != ReasonUnreadable {
		t.Errorf("%#v", e)
	}
}

func TestAFHIRAnswerOfAnotherTypeIsUnreadable(t *testing.T) {
	for body, said := range map[string]string{
		`{"resourceType":"Bundle"}`:  "a Bundle",
		`{"resourceType":"Account"}`: "an Account",
		`{"resourceType":"x y"}`:     "something that is not a FHIR resource",
		`{"resourceType":7}`:         "something that is not a FHIR resource",
		`["InsurancePlan"]`:          "something that is not a FHIR resource",
		`{"id":"no resource type"}`:  "something that is not a FHIR resource",
	} {
		e := asError(t, readPlan(t, fhirReply(200, body)))
		if want := "GET /v1/fhir/r4/InsurancePlan/{id} answered " + said + " where an InsurancePlan was expected."; e.Error() != want {
			t.Errorf("%s: %q", body, e.Error())
		}
	}
}

func TestAnAnswerThatDoesNotReadAsItsTypeSaysWhereNeverAValue(t *testing.T) {
	body := `{"resourceType":"InsurancePlan","name":["PRIVATE-VALUE"]}`
	e := asError(t, readPlan(t, fhirReply(200, body)))
	if e.Reason != ReasonUnreadable || strings.Contains(e.Error(), "PRIVATE-VALUE") {
		t.Errorf("%#v %q", e, e.Error())
	}
	if e.Error() != "GET /v1/fhir/r4/InsurancePlan/{id} answered an InsurancePlan that could not be read: name is not a string." {
		t.Errorf("%q", e.Error())
	}
	// Inside a contained resource, the place is named through it.
	nested := `{"resourceType":"InsurancePlan","contained":[{"resourceType":"Organization","active":"PRIVATE-VALUE"}]}`
	e = asError(t, readPlan(t, fhirReply(200, nested)))
	if !strings.HasSuffix(e.Error(), "could not be read: contained[0].active is not true or false.") || strings.Contains(e.Error(), "PRIVATE") {
		t.Errorf("%q", e.Error())
	}
}

func TestASchedulingTimeIsATimeAndAMalformedOneIsUnreadable(t *testing.T) {
	slot := func(start string) map[string]any {
		return map[string]any{"slots": []any{map[string]any{"id": "s", "publisher": "p", "scheduleId": "x", "status": "free",
			"start": start, "end": "2026-10-18T21:30:00.000Z"}}, "total": 1}
	}
	found, err := testClient(t, record(plainReply(200, slot("2026-10-18T17:00:00.000-04:00")))).SearchSlots(context.Background(), SearchSlotsParams{})
	if err != nil || !found.Slots[0].Start.Equal(time.Date(2026, 10, 18, 21, 0, 0, 0, time.UTC)) {
		t.Fatalf("%v %v", found, err)
	}
	_, err = testClient(t, record(plainReply(200, slot("PRIVATE-VALUE")))).SearchSlots(context.Background(), SearchSlotsParams{})
	e := asError(t, err)
	if e.Reason != ReasonUnreadable || e.Error() != "GET /v1/scheduling/slots answered a body that could not be read: a time is not RFC 3339." {
		t.Errorf("%q", e.Error())
	}
}

func TestAnEmptySearchIsAnAnswerNotAnError(t *testing.T) {
	c := testClient(t, record(fhirReply(200, map[string]any{"resourceType": "Bundle", "type": "searchset", "total": 0, "entry": []any{}})))
	b, err := c.SearchInsurancePlan(context.Background(), SearchInsurancePlanParams{CoverageAreaState: new("NC")})
	if err != nil || b == nil || *b.Total != 0 {
		t.Errorf("%v %v", b, err)
	}
	c = testClient(t, record(plainReply(200, map[string]any{"slots": []any{}, "total": 0})))
	s, err := c.SearchSlots(context.Background(), SearchSlotsParams{})
	if err != nil || s.Total != 0 || len(s.Slots) != 0 {
		t.Errorf("%v %v", s, err)
	}
}

func TestTheLogSafeFormIsTheFieldsThatAreSetInOrder(t *testing.T) {
	e := &Error{
		Kind: KindNoAnswer, Reason: ReasonRateLimited, Status: 429, RetryAfter: 30 * time.Second, hasRetryAfter: true,
		Attempts: 2, Duration: 1234 * time.Millisecond, RequestID: "r", CorrelationID: "c", message: "m",
		outcome: func() *fhir.OperationOutcome { return &fhir.OperationOutcome{ID: new("PRIVATE")} },
	}
	got, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"name":"NoAnswerError","message":"m","kind":"no-answer","status":429,"reason":"rate-limited","retryAfter":30,"attempts":2,"durationMs":1234,"requestId":"r","correlationId":"c"}`
	if string(got) != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if got, _ := json.Marshal(notSent("plain")); string(got) != `{"name":"Haau3Error","message":"plain","kind":"not-sent"}` {
		t.Errorf("got %s", got)
	}
	if e.GoString() != `&haau3.Error{Kind: haau3.KindNoAnswer, Status: 429, Reason: "rate-limited", RetryAfter: 30000000000, Attempts: 2, Duration: 1234000000, RequestID: "r", CorrelationID: "c"}` {
		t.Errorf("%s", e.GoString())
	}
}

func TestSentinelsMatchTheirKindOnly(t *testing.T) {
	e := &Error{Kind: KindRefused}
	wrapped := fmt.Errorf("reading the plan: %w", e)
	if !errors.Is(wrapped, ErrRefused) || errors.Is(wrapped, ErrNoAnswer) || errors.Is(wrapped, ErrAuth) || errors.Is(wrapped, ErrNotSent) {
		t.Error("a sentinel matched another kind")
	}
	var got *Error
	if !errors.As(wrapped, &got) || got != e {
		t.Error("errors.As did not find the error")
	}
}
