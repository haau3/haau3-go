package haau3

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"strings"
	"testing"

	"github.com/haau3/haau3-go/fhir"
)

// The SDK never prints a body the API answered with, a body it sent, or the key: not through any
// of fmt's verbs, a wrapped or joined error, slog, json.Marshal or a recovered panic. Each case
// below plants a marker where a patient's data would be, renders what a customer's code and its
// libraries render, and looks for the marker.

const marker = "PHI-MARKER-0c7e5f2a-never-printed"

var markedOutcome = map[string]any{"resourceType": "OperationOutcome", "issue": []any{map[string]any{
	"severity": "error", "code": "invalid", "diagnostics": marker,
}}}

// renderings is every way an error, or anything holding a body, is commonly written out.
func renderings(t *testing.T, v any) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%d"} {
		out[verb] = fmt.Sprintf(verb, v)
	}
	if err, ok := v.(error); ok {
		out["Error()"] = err.Error()
		wrapped := fmt.Errorf("handling the request: %w", err)
		out["wrapped %v"] = fmt.Sprintf("%v", wrapped)
		out["wrapped %+v"] = fmt.Sprintf("%+v", wrapped)
		out["wrapped %#v"] = fmt.Sprintf("%#v", wrapped)
		out["joined"] = fmt.Sprintf("%+v", errors.Join(err, errors.New("and another")))
		for name, value := range map[string]any{"error": err, "wrapped": wrapped} {
			var text, js bytes.Buffer
			slog.New(slog.NewTextHandler(&text, nil)).Error("failed", "err", value)
			slog.New(slog.NewJSONHandler(&js, nil)).Error("failed", "err", value)
			out["slog text "+name] = text.String()
			out["slog json "+name] = js.String()
		}
		var std bytes.Buffer
		log.New(&std, "", 0).Printf("failed: %v", err)
		out["log"] = std.String()
		out["recovered panic"] = func() (s string) {
			defer func() { s = fmt.Sprintf("panic: %v", recover()) }()
			panic(err)
		}()
	}
	if data, err := json.Marshal(v); err == nil {
		out["json"] = string(data)
	}
	if data, err := json.Marshal(struct{ Held any }{v}); err == nil {
		out["json in a struct"] = string(data)
	}
	out["%+v in a struct"] = fmt.Sprintf("%+v", struct{ Held any }{v})
	out["%#v in a struct"] = fmt.Sprintf("%#v", struct{ Held any }{v})
	return out
}

func assertNeverPrinted(t *testing.T, what string, v any) {
	t.Helper()
	for how, text := range renderings(t, v) {
		if strings.Contains(text, marker) {
			t.Errorf("%s printed the body through %s: %s", what, how, text)
		}
		if strings.Contains(text, testKey) {
			t.Errorf("%s printed the key through %s: %s", what, how, text)
		}
	}
}

func TestARefusalNeverPrintsItsOutcome(t *testing.T) {
	_, err := testClient(t, record(fhirReply(400, markedOutcome))).ReadInsurancePlan(context.Background(), "H1")
	assertNeverPrinted(t, "a refusal", err)
	// It is there to read.
	if e := asError(t, err); *e.Outcome().Issue[0].Diagnostics != marker {
		t.Error("the outcome is not readable")
	}
}

func TestANoAnswerNeverPrintsItsOutcome(t *testing.T) {
	_, err := testClient(t, record(fhirReply(500, markedOutcome))).ReadInsurancePlan(context.Background(), "H1")
	assertNeverPrinted(t, "a no-answer", err)
}

func TestASchedulingRefusalNeverPrintsItsProblem(t *testing.T) {
	p := map[string]any{"type": "about:blank", "title": "t", "status": 400, "detail": marker, "instance": "urn:x"}
	_, err := testClient(t, record(plainReply(400, p))).SearchSlots(context.Background(), SearchSlotsParams{})
	assertNeverPrinted(t, "a problem", err)
	if asError(t, err).Problem().Detail != marker {
		t.Error("the problem is not readable")
	}
}

func TestABodyYouSentIsNeverPrinted(t *testing.T) {
	records := &fhir.Bundle{Type: new("collection"), Entry: []fhir.BundleEntry{{Resource: &fhir.MedicationStatement{ID: new(marker)}}}}
	_, err := testClient(t, record(fhirReply(500, nil))).ReconcileMedications(context.Background(), records)
	assertNeverPrinted(t, "an error for a request with a body", err)
	_, err = testClient(t, record(fhirReply(400, nil))).ReconcileMedications(context.Background(), records, WithMaxRetries(0))
	assertNeverPrinted(t, "a refusal of a request with a body", err)
	call := Calls.CodeSystemLookupPost(&fhir.Parameters{ID: new(marker)})
	assertNeverPrinted(t, "a batch entry", call)
}

func TestABatchEntryErrorNeverPrintsItsOutcome(t *testing.T) {
	rec := record(fhirReply(200, batchResponse(map[string]any{"response": map[string]any{"status": "404", "outcome": markedOutcome}})))
	p := Calls.ReadInsurancePlan("H1")
	if err := testClient(t, rec).Batch(context.Background(), []Entry{p}); err != nil {
		t.Fatal(err)
	}
	_, err := p.Result()
	assertNeverPrinted(t, "a batch entry's error", err)
	assertNeverPrinted(t, "a batch entry after its answer", p)
}

func TestARawResponseNeverPrintsItsBody(t *testing.T) {
	raw, err := testClient(t, record(fhirReply(200, markedOutcome))).Request(context.Background(), RawRequest{Method: "GET", Path: "/v1/fhir/r4/x"})
	if err != nil {
		t.Fatal(err)
	}
	assertNeverPrinted(t, "a raw response", raw)
	if !strings.Contains(string(raw.Body()), marker) {
		t.Error("the body is not readable")
	}
}

func TestTheClientNeverPrintsItsKey(t *testing.T) {
	assertNeverPrinted(t, "a client", testClient(t, record()))
}

// The controls: a holder written without care leaks through these renderings, so the cases above
// would catch the SDK doing the same.
type carelessError struct{ Body string }

func (e *carelessError) Error() string { return "refused: " + e.Body }

type carelessHolder struct{ Body string }

func TestTheChecksCatchACarelessHolder(t *testing.T) {
	leaked := func(v any) []string {
		var hows []string
		for how, text := range renderings(t, v) {
			if strings.Contains(text, marker) {
				hows = append(hows, how)
			}
		}
		return hows
	}
	if hows := leaked(&carelessError{Body: marker}); len(hows) < 10 {
		t.Errorf("a careless error leaked through only %v", hows)
	}
	if hows := leaked(carelessHolder{Body: marker}); len(hows) < 4 {
		t.Errorf("a careless holder leaked through only %v", hows)
	}
}
