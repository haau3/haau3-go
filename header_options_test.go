package haau3

// A header an operation declares is an option of its own: ProcessMessage's WithPayerAuthorization,
// sent as Payer-Authorization. It is checked against the spec's schema before anything is sent,
// wins over the same header given with WithHeader, is for a call to ProcessMessage only, and no
// error carries its value.

import (
	"context"
	"strings"
	"testing"

	"github.com/haau3/haau3-go/fhir"
)

// payerToken is a token no error, message or printout may ever carry.
const payerToken = "eyJ0b2tlbi10aGF0LW11c3QtbmV2ZXItYmUtcHJpbnRlZA"

var answerBundle = map[string]any{"resourceType": "Bundle", "type": "message", "entry": []any{}}

func message() *fhir.Bundle {
	return &fhir.Bundle{Type: new("message")}
}

func TestPayerAuthorizationIsSentBesideTheKey(t *testing.T) {
	rec := record(fhirReply(200, answerBundle))
	c := testClient(t, rec)
	if _, err := c.ProcessMessage(context.Background(), message(), WithPayerAuthorization("Bearer "+payerToken)); err != nil {
		t.Fatal(err)
	}
	h := rec.last().Header
	if h.Get("Payer-Authorization") != "Bearer "+payerToken || h.Get("Authorization") != "Bearer "+testKey {
		t.Errorf("%v", h)
	}
}

func TestPayerAuthorizationIsNotSentWhenNotGiven(t *testing.T) {
	// The control: the header is the option's doing.
	rec := record(fhirReply(200, answerBundle))
	c := testClient(t, rec)
	if _, err := c.ProcessMessage(context.Background(), message(), WithMaxRetries(0)); err != nil {
		t.Fatal(err)
	}
	if v, ok := rec.last().Header["Payer-Authorization"]; ok {
		t.Errorf("sent %d values", len(v))
	}
}

func TestPayerAuthorizationTakesAnyCaseOfTheScheme(t *testing.T) {
	rec := record(fhirReply(200, answerBundle))
	if _, err := testClient(t, rec).ProcessMessage(context.Background(), message(), WithPayerAuthorization("bearer  "+payerToken)); err != nil {
		t.Fatal(err)
	}
	if got := rec.last().Header.Get("Payer-Authorization"); got != "bearer  "+payerToken {
		t.Errorf("%q", got)
	}
}

func TestPayerAuthorizationTheAPIWouldRefuseIsNotSent(t *testing.T) {
	const form = "POST /v1/fhir/r4/$process-message was not sent: WithPayerAuthorization is not in the form the API takes in Payer-Authorization, which the option's description gives."
	const long = "POST /v1/fhir/r4/$process-message was not sent: WithPayerAuthorization is longer than the 4096 characters the API takes in Payer-Authorization."
	for name, tc := range map[string]struct{ value, said string }{
		"a bare token":       {payerToken, form},
		"another scheme":     {"Basic " + payerToken, form},
		"an empty value":     {"", form},
		"a final newline":    {"Bearer " + payerToken + "\n", form},
		"4,097 characters":   {"Bearer " + strings.Repeat("a", 4090), long},
		"a line break in it": {"Bearer " + payerToken + "\r\nX-Other: 1", form},
	} {
		t.Run(name, func(t *testing.T) {
			rec := record(fhirReply(200, answerBundle))
			_, err := testClient(t, rec).ProcessMessage(context.Background(), message(), WithPayerAuthorization(tc.value))
			if e := asError(t, err); e.Kind != KindNotSent || e.Error() != tc.said {
				t.Errorf("%v %q", e.Kind, e.Error())
			}
			if rec.count() != 0 {
				t.Errorf("%d requests sent", rec.count())
			}
			for how, text := range renderings(t, err) {
				if strings.Contains(text, payerToken) || (tc.value != "" && strings.Contains(text, tc.value[len(tc.value)-12:])) {
					t.Errorf("printed the value through %s: %s", how, text)
				}
			}
		})
	}
}

func TestPayerAuthorizationTakes4096Characters(t *testing.T) {
	rec := record(fhirReply(200, answerBundle))
	atLimit := "Bearer " + strings.Repeat("a", 4089)
	if len(atLimit) != 4096 {
		t.Fatal(len(atLimit))
	}
	if _, err := testClient(t, rec).ProcessMessage(context.Background(), message(), WithPayerAuthorization(atLimit)); err != nil {
		t.Fatal(err)
	}
	if rec.last().Header.Get("Payer-Authorization") != atLimit {
		t.Error("not sent")
	}
}

func TestPayerAuthorizationWinsOverWithHeader(t *testing.T) {
	rec := record(fhirReply(200, answerBundle))
	c := testClient(t, rec, WithHeader("PAYER-AUTHORIZATION", "Bearer from-the-client"))
	_, err := c.ProcessMessage(context.Background(), message(),
		WithPayerAuthorization("Bearer "+payerToken), WithHeader("payer-authorization", "Bearer from-the-call"))
	if err != nil {
		t.Fatal(err)
	}
	if got := rec.last().Header.Values("Payer-Authorization"); len(got) != 1 || got[0] != "Bearer "+payerToken {
		t.Errorf("%q", got)
	}
}

func TestPayerAuthorizationIsSentAgainOnARetry(t *testing.T) {
	rec := record(fhirReply(503, nil), fhirReply(200, answerBundle))
	if _, err := testClient(t, rec).ProcessMessage(context.Background(), message(), WithPayerAuthorization("Bearer "+payerToken), WithMaxRetries(1)); err != nil {
		t.Fatal(err)
	}
	if rec.count() != 2 {
		t.Fatalf("%d requests", rec.count())
	}
	for i, req := range rec.requests {
		if req.Header.Get("Payer-Authorization") != "Bearer "+payerToken {
			t.Errorf("attempt %d sent %q", i+1, req.Header.Get("Payer-Authorization"))
		}
	}
}

func TestPayerAuthorizationIsForAProcessMessageCallOnly(t *testing.T) {
	if _, err := NewClient(WithAPIKey(testKey), WithPayerAuthorization("Bearer "+payerToken)); err == nil {
		t.Fatal("NewClient took it")
	} else if e := asError(t, err); e.Kind != KindNotSent || !strings.Contains(e.Error(), "WithPayerAuthorization") || strings.Contains(e.Error(), payerToken) {
		t.Errorf("%v", e)
	}
	rec := record(fhirReply(200, plan))
	_, err := testClient(t, rec).ReadInsurancePlan(context.Background(), "H1", WithPayerAuthorization("Bearer "+payerToken))
	said := "GET /v1/fhir/r4/InsurancePlan/{id} was not sent: WithPayerAuthorization is for a call whose operation declares Payer-Authorization, which this one does not."
	if e := asError(t, err); e.Kind != KindNotSent || e.Error() != said {
		t.Errorf("%q", e.Error())
	}
	if rec.count() != 0 {
		t.Errorf("%d requests sent", rec.count())
	}
}

func TestNoErrorTheAPIAnswersCarriesThePayerAuthorization(t *testing.T) {
	outcome := map[string]any{"resourceType": "OperationOutcome", "issue": []any{map[string]any{"severity": "error", "code": "invalid"}}}
	rec := record(fhirReply(400, outcome))
	_, err := testClient(t, rec).ProcessMessage(context.Background(), message(), WithPayerAuthorization("Bearer "+payerToken))
	if e := asError(t, err); e.Kind != KindRefused {
		t.Fatalf("%v", e.Kind)
	}
	for how, text := range renderings(t, err) {
		if strings.Contains(text, payerToken) {
			t.Errorf("printed the token through %s: %s", how, text)
		}
	}
}
