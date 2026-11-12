package haau3

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/haau3/haau3-go/fhir"
)

// Which environment a client calls, and with which key: the key's mode picks the host, no key is
// allowed only in the Sandbox, a mismatch is refused before anything is sent, and an explicit base
// URL still decides where requests go.

// olderKey is a key made before test and live keys: no mode in its prefix.
const olderMarkerKey = "haau3_sk_older_key_that_must_never_be_printed"

const (
	productionHost = "https://api.haau3.com"
	sandboxHost    = "https://sandbox.haau3.com"
)

// clearEnvironment unsets the three variables a client reads, so each test names its own.
func clearEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{"HAAU3_API_KEY", "HAAU3_ENVIRONMENT", "HAAU3_BASE_URL"} {
		t.Setenv(name, "")
	}
}

// onRecorder is a client on rec, with no other key or base URL than opts give it.
func onRecorder(t *testing.T, rec *recorder, opts ...Option) *Client {
	t.Helper()
	noWaits(t)
	c, err := NewClient(append([]Option{WithHTTPClient(&http.Client{Transport: rec})}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// refusedBy is the error NewClient returned, which must be of KindNotSent.
func refusedBy(t *testing.T, opts ...Option) *Error {
	t.Helper()
	c, err := NewClient(opts...)
	if c != nil || err == nil {
		t.Fatalf("the client was made: %v", c)
	}
	var e *Error
	if !errors.As(err, &e) || e.Kind != KindNotSent || !errors.Is(err, ErrNotSent) {
		t.Fatalf("%v is not an error of KindNotSent", err)
	}
	return e
}

func TestATestKeySendsEveryCallToTheSandboxAndNoneToProduction(t *testing.T) {
	clearEnvironment(t)
	rec := record(fhirReply(200, map[string]any{"resourceType": "CapabilityStatement"}))
	c := onRecorder(t, rec, WithAPIKey(markerTestKey))
	if c.Environment() != EnvironmentSandbox || c.BaseURL() != sandboxHost {
		t.Fatalf("the client calls %s at %s", c.Environment(), c.BaseURL())
	}
	for range 2 {
		if _, err := c.FHIRMetadata(context.Background(), FHIRMetadataParams{}); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range rec.requests {
		if origin(r.URL) != sandboxHost {
			t.Errorf("a request went to %s", origin(r.URL))
		}
	}
	if got := rec.last().Header.Get("Authorization"); got != "Bearer "+markerTestKey {
		t.Errorf("Authorization is %q", got)
	}
}

func TestALiveKeyAndOneMadeBeforeModesGoToProduction(t *testing.T) {
	clearEnvironment(t)
	for _, key := range []string{markerKey, olderMarkerKey} {
		rec := record(fhirReply(200, map[string]any{"resourceType": "CapabilityStatement"}))
		c := onRecorder(t, rec, WithAPIKey(key))
		if c.Environment() != EnvironmentProduction || c.BaseURL() != productionHost {
			t.Fatalf("the client calls %s at %s", c.Environment(), c.BaseURL())
		}
		if _, err := c.FHIRMetadata(context.Background(), FHIRMetadataParams{}); err != nil {
			t.Fatal(err)
		}
		if origin(rec.last().URL) != productionHost {
			t.Errorf("a request went to %s", origin(rec.last().URL))
		}
	}
}

func TestTheKeysModeIsReadFromHAAU3APIKeyToo(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("HAAU3_API_KEY", markerTestKey)
	c, err := NewClient()
	if err != nil || c.BaseURL() != sandboxHost {
		t.Fatalf("%v %v", c, err)
	}
}

func TestNoKeyCallsTheSandboxWithNoAuthorizationHeaderWhenTheEnvironmentNamesIt(t *testing.T) {
	clearEnvironment(t)
	for _, name := range []string{"option", "HAAU3_ENVIRONMENT"} {
		var opts []Option
		if name == "option" {
			opts = append(opts, WithEnvironmentSandbox())
		} else {
			t.Setenv("HAAU3_ENVIRONMENT", "sandbox")
		}
		rec := record(fhirReply(200, map[string]any{"resourceType": "CapabilityStatement"}))
		c := onRecorder(t, rec, opts...)
		if c.Environment() != EnvironmentSandbox || c.BaseURL() != sandboxHost {
			t.Fatalf("%s: the client calls %s at %s", name, c.Environment(), c.BaseURL())
		}
		if _, err := c.FHIRMetadata(context.Background(), FHIRMetadataParams{}); err != nil {
			t.Fatal(err)
		}
		if _, sent := rec.last().Header["Authorization"]; sent || origin(rec.last().URL) != sandboxHost {
			t.Errorf("%s: Authorization sent %v, to %s", name, sent, origin(rec.last().URL))
		}
	}
}

func TestNoKeyIsRefusedWithProductionOrNothingNamed(t *testing.T) {
	clearEnvironment(t)
	for _, opts := range [][]Option{nil, {WithEnvironmentProduction()}, {WithBaseURL("http://localhost:3001")}, {WithAPIKey("")}} {
		e := refusedBy(t, opts...)
		for _, want := range []string{
			"No API key. Set HAAU3_API_KEY, or pass it as haau3.WithAPIKey.",
			"https://platform.haau3.com",
			"haau3.WithEnvironmentSandbox()",
			"HAAU3_ENVIRONMENT=sandbox",
		} {
			if !strings.Contains(e.Error(), want) {
				t.Errorf("%q does not say %q", e.Error(), want)
			}
		}
	}
}

func TestAKeyOfTheOtherModeIsRefusedBeforeAnyRequest(t *testing.T) {
	clearEnvironment(t)
	rec := record(fhirReply(200, nil))
	client := &http.Client{Transport: rec}
	for _, tc := range []struct {
		opts  []Option
		begin string
	}{
		{[]Option{WithAPIKey(markerTestKey), WithEnvironmentProduction()},
			"The key in WithAPIKey is a test key (haau3_sk_test_…), which works only in the Sandbox, and WithEnvironmentProduction names production. Use a live key there, or name the Sandbox."},
		{[]Option{WithAPIKey(markerKey), WithEnvironmentSandbox()},
			"The key in WithAPIKey is a live key (haau3_sk_live_…), which does not work in the Sandbox, and WithEnvironmentSandbox names the Sandbox. Use a test key there, or pass no key to call it with none."},
		{[]Option{WithAPIKey(olderMarkerKey), WithEnvironmentSandbox()},
			"The key in WithAPIKey was made before test and live keys, so it does not work in the Sandbox"},
	} {
		e := refusedBy(t, append(tc.opts, WithHTTPClient(client))...)
		if !strings.HasPrefix(e.Error(), tc.begin) {
			t.Errorf("%q does not begin %q", e.Error(), tc.begin)
		}
		for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
			for _, key := range []string{markerKey, markerTestKey, olderMarkerKey} {
				if strings.Contains(fmt.Sprintf(verb, e), key) {
					t.Errorf("%s shows a key", verb)
				}
			}
		}
	}
	if rec.count() != 0 {
		t.Errorf("%d requests were sent", rec.count())
	}
}

func TestTheErrorNamesHAAU3APIKeyAndSaysToUnsetIt(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("HAAU3_API_KEY", markerKey)
	want := "The key in HAAU3_API_KEY is a live key (haau3_sk_live_…), which does not work in the Sandbox, and WithEnvironmentSandbox names the Sandbox. Use a test key there, or unset HAAU3_API_KEY to call it with no key."
	if e := refusedBy(t, WithEnvironmentSandbox()); e.Error() != want {
		t.Errorf("%q", e.Error())
	}
	t.Setenv("HAAU3_ENVIRONMENT", "sandbox")
	if e := refusedBy(t); !strings.Contains(e.Error(), "and HAAU3_ENVIRONMENT names the Sandbox") {
		t.Errorf("%q", e.Error())
	}
}

func TestTheEnvironmentOptionWinsOverHAAU3Environment(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("HAAU3_ENVIRONMENT", "sandbox")
	c, err := NewClient(WithAPIKey(markerKey), WithEnvironmentProduction())
	if err != nil || c.Environment() != EnvironmentProduction {
		t.Fatalf("%v %v", c, err)
	}
}

// The control: a key goes with its own environment.
func TestAKeyOfTheSameModeGoesWithItsEnvironment(t *testing.T) {
	clearEnvironment(t)
	for _, tc := range []struct {
		opts []Option
		host string
	}{
		{[]Option{WithAPIKey(markerTestKey), WithEnvironmentSandbox()}, sandboxHost},
		{[]Option{WithAPIKey(markerKey), WithEnvironmentProduction()}, productionHost},
		{[]Option{WithAPIKey(olderMarkerKey), WithEnvironmentProduction()}, productionHost},
	} {
		c, err := NewClient(tc.opts...)
		if err != nil || c.BaseURL() != tc.host {
			t.Errorf("%v %v", c, err)
		}
	}
}

func TestHAAU3EnvironmentThatIsNeitherIsRefusedWithoutRepeatingIt(t *testing.T) {
	clearEnvironment(t)
	t.Setenv("HAAU3_ENVIRONMENT", markerTestKey)
	e := refusedBy(t, WithAPIKey(markerKey))
	if e.Error() != `HAAU3_ENVIRONMENT takes "production" or "sandbox", and this is neither.` {
		t.Errorf("%q", e.Error())
	}
}

func TestAnExplicitBaseURLDecidesWhereRequestsGo(t *testing.T) {
	clearEnvironment(t)
	rec := record(fhirReply(200, map[string]any{"resourceType": "CapabilityStatement"}))
	c := onRecorder(t, rec, WithAPIKey(markerTestKey), WithBaseURL("http://localhost:3001"))
	if c.Environment() != EnvironmentSandbox {
		t.Errorf("the client calls %s", c.Environment())
	}
	if _, err := c.FHIRMetadata(context.Background(), FHIRMetadataParams{}); err != nil {
		t.Fatal(err)
	}
	if origin(rec.last().URL) != "http://localhost:3001" {
		t.Errorf("a request went to %s", origin(rec.last().URL))
	}
	t.Setenv("HAAU3_BASE_URL", "http://localhost:3002")
	if c, err := NewClient(WithAPIKey(markerTestKey)); err != nil || c.BaseURL() != "http://localhost:3002" {
		t.Errorf("%v %v", c, err)
	}
}

func TestAnExplicitBaseURLKeepsTheEnvironmentsRulesForTheKey(t *testing.T) {
	clearEnvironment(t)
	if e := refusedBy(t, WithAPIKey(markerKey), WithEnvironmentSandbox(), WithBaseURL("http://localhost:3001")); !strings.HasPrefix(e.Error(), "The key in WithAPIKey is a live key") {
		t.Errorf("%q", e.Error())
	}
	c, err := NewClient(WithEnvironmentSandbox(), WithBaseURL("http://localhost:3001"))
	if err != nil || c.BaseURL() != "http://localhost:3001" {
		t.Errorf("%v %v", c, err)
	}
}

// A call's key never changes the host, and is sent as given: the API's own 403 names a key of the
// wrong mode. So a live-key client still sends the first address's operation with a call's test key,
// as it did before the key's mode picked the host.
func TestACallsKeyIsSentAsGivenToTheClientsHost(t *testing.T) {
	clearEnvironment(t)
	rec := record(fhirReply(200, map[string]any{"resourceType": "Bundle", "type": "message"}))
	production := onRecorder(t, rec, WithAPIKey(markerKey))
	if _, err := production.SandboxProcessMessage(context.Background(), &fhir.Bundle{}, WithAPIKey(markerTestKey)); err != nil {
		t.Fatal(err)
	}
	if got := rec.last().URL.String(); got != productionHost+"/v1/sandbox/fhir/r4/$process-message" {
		t.Errorf("sent to %s", got)
	}
	if rec.last().Header.Get("Authorization") != "Bearer "+markerTestKey {
		t.Error("the call's key was not the one sent")
	}
}

func TestTheDeprecatedMethodsCallTheSameOperationsOnTheSandboxsOwnHost(t *testing.T) {
	clearEnvironment(t)
	message := &fhir.Bundle{}
	for _, tc := range []struct {
		name string
		opts []Option
		url  string
	}{
		{"a test key", []Option{WithAPIKey(markerTestKey)}, sandboxHost + "/v1/fhir/r4/$process-message"},
		{"no key", []Option{WithEnvironmentSandbox()}, sandboxHost + "/v1/fhir/r4/$process-message"},
		{"a stand-in for the Sandbox", []Option{WithEnvironmentSandbox(), WithBaseURL("http://localhost:3001")}, "http://localhost:3001/v1/fhir/r4/$process-message"},
		{"a live key", []Option{WithAPIKey(markerKey)}, productionHost + "/v1/sandbox/fhir/r4/$process-message"},
		{"a key made before modes", []Option{WithAPIKey(olderMarkerKey)}, productionHost + "/v1/sandbox/fhir/r4/$process-message"},
		{"a test key with a base URL", []Option{WithAPIKey(markerTestKey), WithBaseURL(productionHost)}, productionHost + "/v1/sandbox/fhir/r4/$process-message"},
	} {
		rec := record(fhirReply(200, map[string]any{"resourceType": "Bundle", "type": "message"}))
		c := onRecorder(t, rec, tc.opts...)
		if _, err := c.SandboxProcessMessage(context.Background(), message, WithPayerAuthorization("Bearer abc")); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := rec.last().URL.String(); got != tc.url {
			t.Errorf("%s: sent to %s, not %s", tc.name, got, tc.url)
		}
		if rec.last().Method != http.MethodPost || rec.last().Header.Get("Payer-Authorization") != "Bearer abc" {
			t.Errorf("%s: %s, Payer-Authorization %q", tc.name, rec.last().Method, rec.last().Header.Get("Payer-Authorization"))
		}
	}
	rec := record(fhirReply(200, map[string]any{"resourceType": "CapabilityStatement"}))
	if _, err := onRecorder(t, rec, WithAPIKey(markerTestKey)).SandboxMetadata(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := rec.last().URL.String(); got != sandboxHost+"/v1/fhir/r4/metadata" {
		t.Errorf("metadata sent to %s", got)
	}
}

func TestADeprecatedMethodsRefusalNamesThePathItWasSentTo(t *testing.T) {
	clearEnvironment(t)
	rec := record(fhirReply(400, map[string]any{"resourceType": "OperationOutcome", "issue": []any{}}))
	_, err := onRecorder(t, rec, WithAPIKey(markerTestKey)).SandboxProcessMessage(context.Background(), &fhir.Bundle{})
	if err == nil || !strings.HasPrefix(err.Error(), "POST /v1/fhir/r4/$process-message was refused") {
		t.Errorf("%v", err)
	}
}

// The control: only the deprecated methods follow the client, never Request.
func TestRequestIsSentAsItIsGiven(t *testing.T) {
	clearEnvironment(t)
	rec := record(fhirReply(200, map[string]any{"resourceType": "Bundle"}))
	c := onRecorder(t, rec, WithAPIKey(markerTestKey))
	if _, err := c.Request(context.Background(), RawRequest{Method: http.MethodPost, Path: "/v1/sandbox/fhir/r4/$process-message"}); err != nil {
		t.Fatal(err)
	}
	if got := rec.last().URL.String(); got != sandboxHost+"/v1/sandbox/fhir/r4/$process-message" {
		t.Errorf("sent to %s", got)
	}
}

func TestAClientWithNoKeyPrintsWhereItSendsAndNoKey(t *testing.T) {
	clearEnvironment(t)
	c, err := NewClient(WithEnvironmentSandbox())
	if err != nil {
		t.Fatal(err)
	}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		if got := fmt.Sprintf(verb, c); got != "haau3.Client("+sandboxHost+")" {
			t.Errorf("%s: %s", verb, got)
		}
	}
}

func TestAClientWithNoKeySaysARequestAndThatNoKeyWasSent(t *testing.T) {
	clearEnvironment(t)
	rec := record(fhirReply(401, map[string]any{"resourceType": "OperationOutcome", "issue": []any{}}))
	c := onRecorder(t, rec, WithEnvironmentSandbox())
	_, err := c.FHIRMetadata(context.Background(), FHIRMetadataParams{})
	if err == nil || err.Error() != "GET /v1/fhir/r4/metadata was refused: it was sent with no API key (401)." {
		t.Errorf("%v", err)
	}
	_, err = c.Request(context.Background(), RawRequest{Method: http.MethodGet, Path: productionHost + "/v1/fhir/r4/metadata"})
	if err == nil || !strings.Contains(err.Error(), "refusing to send a request to "+productionHost) {
		t.Errorf("%v", err)
	}
}

func TestAKeyIsTrimmedAndAKeyWithAnythingBeforeItsPrefixIsRefused(t *testing.T) {
	clearEnvironment(t)
	rec := record(fhirReply(200, map[string]any{"resourceType": "CapabilityStatement"}))
	t.Setenv("HAAU3_API_KEY", "  "+markerTestKey+"\n")
	c := onRecorder(t, rec)
	if _, err := c.FHIRMetadata(context.Background(), FHIRMetadataParams{}); err != nil {
		t.Fatal(err)
	}
	if c.BaseURL() != sandboxHost || rec.last().Header.Get("Authorization") != "Bearer "+markerTestKey {
		t.Errorf("sent to %s", c.BaseURL())
	}
	want := `The key in HAAU3_API_KEY has something before its haau3_sk_ prefix, such as quotes or "Bearer ". Set the key alone.`
	for _, held := range []string{`"` + markerTestKey + `"`, "'" + markerKey + "'", "Bearer " + markerTestKey} {
		t.Setenv("HAAU3_API_KEY", held)
		if e := refusedBy(t); e.Error() != want {
			t.Errorf("%q", e.Error())
		}
	}
	if e := refusedBy(t, WithAPIKey(`"`+markerKey+`"`)); !strings.HasPrefix(e.Error(), "The key in WithAPIKey has something before") {
		t.Errorf("%q", e.Error())
	}
}

func TestHAAU3EnvironmentIsReadTrimmedInAnyCaseAndOnlyWithNoOption(t *testing.T) {
	clearEnvironment(t)
	for _, written := range []string{"Sandbox", " sandbox ", "SANDBOX\n"} {
		t.Setenv("HAAU3_ENVIRONMENT", written)
		if c, err := NewClient(); err != nil || c.Environment() != EnvironmentSandbox {
			t.Errorf("%q: %v %v", written, c, err)
		}
	}
	t.Setenv("HAAU3_ENVIRONMENT", "staging")
	if c, err := NewClient(WithAPIKey(markerKey), WithEnvironmentProduction()); err != nil || c.Environment() != EnvironmentProduction {
		t.Errorf("a stray HAAU3_ENVIRONMENT stopped a client that names one: %v", err)
	}
}

func TestABaseURLOnOneEnvironmentsAddressMustAgreeWithTheNamedEnvironment(t *testing.T) {
	clearEnvironment(t)
	if e := refusedBy(t, WithEnvironmentSandbox(), WithBaseURL(productionHost)); e.Error() != "WithEnvironmentSandbox names the Sandbox, and WithBaseURL is production's address, https://api.haau3.com. Name one environment, or a base URL that is neither's." {
		t.Errorf("%q", e.Error())
	}
	t.Setenv("HAAU3_BASE_URL", sandboxHost)
	if e := refusedBy(t, WithAPIKey(markerKey), WithEnvironmentProduction()); e.Error() != "WithEnvironmentProduction names production, and HAAU3_BASE_URL is the Sandbox's address, https://sandbox.haau3.com. Name one environment, or a base URL that is neither's." {
		t.Errorf("%q", e.Error())
	}
}

func TestTheSandboxsAddressTakesTestKeysOnlyByItsOrigin(t *testing.T) {
	clearEnvironment(t)
	for _, base := range []string{sandboxHost, "https://SANDBOX.haau3.com", "https://sandbox.haau3.com:443"} {
		if e := refusedBy(t, WithAPIKey(markerKey), WithBaseURL(base)); e.Error() != "The key in WithAPIKey is a live key (haau3_sk_live_…), and WithBaseURL is the Sandbox's address, https://sandbox.haau3.com, which takes test keys only. Use a test key there." {
			t.Errorf("%s: %q", base, e.Error())
		}
		if e := refusedBy(t, WithAPIKey(olderMarkerKey), WithBaseURL(base)); !strings.HasPrefix(e.Error(), "The key in WithAPIKey was made before test and live keys, and WithBaseURL is the Sandbox's address") {
			t.Errorf("%s: %q", base, e.Error())
		}
		rec := record(fhirReply(200, map[string]any{"resourceType": "Bundle", "type": "message"}))
		c := onRecorder(t, rec, WithAPIKey(markerTestKey), WithBaseURL(base))
		if c.Environment() != EnvironmentSandbox {
			t.Errorf("%s: %s", base, c.Environment())
		}
		if _, err := c.SandboxProcessMessage(context.Background(), &fhir.Bundle{}); err != nil {
			t.Fatal(err)
		}
		if rec.last().URL.Path != "/v1/fhir/r4/$process-message" {
			t.Errorf("%s: the deprecated method went to %s", base, rec.last().URL.Path)
		}
	}
}

func TestProductionsAddressStillTakesATestKeyAtTheFirstAddress(t *testing.T) {
	clearEnvironment(t)
	rec := record(fhirReply(200, map[string]any{"resourceType": "Bundle", "type": "message"}))
	c := onRecorder(t, rec, WithAPIKey(markerTestKey), WithBaseURL(productionHost))
	if c.Environment() != EnvironmentProduction {
		t.Errorf("%s", c.Environment())
	}
	if _, err := c.SandboxProcessMessage(context.Background(), &fhir.Bundle{}); err != nil {
		t.Fatal(err)
	}
	if got := rec.last().URL.String(); got != productionHost+"/v1/sandbox/fhir/r4/$process-message" {
		t.Errorf("sent to %s", got)
	}
}
