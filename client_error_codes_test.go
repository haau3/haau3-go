package haau3

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Why a client could not be made, as a code: every error of KindNotSent that NewClient returns
// before anything is sent carries one, so a caller branches on the code and never on the message.
// The messages are the ones NewClient gave before the codes, and refusals raised by a call carry
// none.

// codeCase is one way NewClient refuses: its code, and its message as it was before codes (a
// pattern, matched whole when it starts with ^ and ends with $).
type codeCase struct {
	name    string
	code    string
	env     map[string]string
	opts    []Option
	goos    string
	message string
}

var codeCases = []codeCase{
	{name: "no key, and the Sandbox not named", code: CodeNoAPIKey, message: `^` + regexp.QuoteMeta(noKey) + `$`},
	{
		name: "quotes an env file kept around the key", code: CodeAPIKeyNotAlone,
		opts:    []Option{WithAPIKey(`"` + markerKey + `"`)},
		message: `^` + regexp.QuoteMeta(`The key in WithAPIKey has something before its haau3_sk_ prefix, such as quotes or "Bearer ". Set the key alone.`) + `$`,
	},
	{name: "Bearer before the key, from the variable", code: CodeAPIKeyNotAlone, env: map[string]string{"HAAU3_API_KEY": "Bearer " + markerKey}, message: `^The key in HAAU3_API_KEY `},
	{
		name: "a test key with production named", code: CodeTestKeyOutsideSandbox,
		opts:    []Option{WithAPIKey(markerTestKey), WithEnvironmentProduction()},
		message: `^` + regexp.QuoteMeta(`The key in WithAPIKey is a test key (haau3_sk_test_…), which works only in the Sandbox, and WithEnvironmentProduction names production. Use a live key there, or name the Sandbox.`) + `$`,
	},
	{name: "a test key with HAAU3_ENVIRONMENT naming production", code: CodeTestKeyOutsideSandbox, env: map[string]string{"HAAU3_ENVIRONMENT": "production"}, opts: []Option{WithAPIKey(markerTestKey)}, message: `and HAAU3_ENVIRONMENT names production`},
	{name: "a live key with the Sandbox named", code: CodeLiveKeyInSandbox, opts: []Option{WithAPIKey(markerKey), WithEnvironmentSandbox()}, message: `is a live key \(haau3_sk_live_…\)`},
	{name: "a live key at the Sandbox's address", code: CodeLiveKeyInSandbox, opts: []Option{WithAPIKey(markerKey), WithBaseURL(sandboxHost)}, message: `which takes test keys only`},
	{name: "an older key with the Sandbox named", code: CodeOlderKeyInSandbox, opts: []Option{WithAPIKey(olderMarkerKey), WithEnvironmentSandbox()}, message: `was made before test and live keys`},
	{name: "an older key at the Sandbox's address", code: CodeOlderKeyInSandbox, opts: []Option{WithAPIKey(olderMarkerKey), WithBaseURL(sandboxHost)}, message: `which takes test keys only`},
	{
		name: "HAAU3_ENVIRONMENT that is neither", code: CodeInvalidEnvironment,
		env: map[string]string{"HAAU3_ENVIRONMENT": "staging"}, opts: []Option{WithAPIKey(markerKey)},
		message: `^` + regexp.QuoteMeta(`HAAU3_ENVIRONMENT takes "production" or "sandbox", and this is neither.`) + `$`,
	},
	{name: "a base URL that is not http or https", code: CodeInvalidBaseURL, opts: []Option{WithAPIKey(markerKey), WithBaseURL("ftp://api.example.com")}, message: `^WithBaseURL takes an http or https URL`},
	{name: "a base URL with a port that is not a number", code: CodeInvalidBaseURL, opts: []Option{WithAPIKey(markerKey), WithBaseURL("https://api.example.com:99999")}, message: `^WithBaseURL takes an http or https URL`},
	{name: "HAAU3_BASE_URL with a query", code: CodeInvalidBaseURL, env: map[string]string{"HAAU3_BASE_URL": "https://proxy.example.com/?a=1"}, opts: []Option{WithAPIKey(markerKey)}, message: `^HAAU3_BASE_URL takes`},
	{
		name: "production named with the Sandbox's address", code: CodeEnvironmentConflictsWithBaseURL,
		opts:    []Option{WithAPIKey(markerKey), WithEnvironmentProduction(), WithBaseURL(sandboxHost)},
		message: `^` + regexp.QuoteMeta(`WithEnvironmentProduction names production, and WithBaseURL is the Sandbox's address, https://sandbox.haau3.com. Name one environment, or a base URL that is neither's.`) + `$`,
	},
	{name: "an AppInfo with no name", code: CodeInvalidAppInfo, opts: []Option{WithAPIKey(markerKey), WithAppInfo(AppInfo{Version: "1.0"})}, message: `^AppInfo\.Name is required`},
	{name: "an AppInfo name with a space", code: CodeInvalidAppInfo, opts: []Option{WithAPIKey(markerKey), WithAppInfo(AppInfo{Name: "my plugin"})}, message: `^AppInfo\.Name takes`},
	{name: "an AppInfo version with a slash", code: CodeInvalidAppInfo, opts: []Option{WithAPIKey(markerKey), WithAppInfo(AppInfo{Name: "a", Version: "1/2"})}, message: `^AppInfo\.Version takes`},
	{name: "an AppInfo URL that is not a web address", code: CodeInvalidAppInfo, opts: []Option{WithAPIKey(markerKey), WithAppInfo(AppInfo{Name: "a", URL: "ftp://x"})}, message: `^AppInfo\.URL takes`},
	{name: "a timeout of zero", code: CodeInvalidOption, opts: []Option{WithAPIKey(markerKey), WithTimeout(0)}, message: `^WithTimeout takes a duration longer than zero\.$`},
	{name: "retries below zero", code: CodeInvalidOption, opts: []Option{WithAPIKey(markerKey), WithMaxRetries(-1)}, message: `^WithMaxRetries takes zero or more\.$`},
	{name: "a call's option given to NewClient", code: CodeCallOnlyOption, opts: []Option{WithAPIKey(markerKey), WithCorrelationID(requestID)}, message: `^NewClient does not take WithCorrelationID, which is for a call only\.$`},
	{name: "a client made in a browser", code: CodeInBrowser, goos: "js", opts: []Option{WithAPIKey(markerKey)}, message: `^haau3 runs on a server only`},
}

// everyCode is every Code constant: a code added without a case here fails the coverage test.
var everyCode = []string{
	CodeNoAPIKey, CodeAPIKeyNotAlone, CodeTestKeyOutsideSandbox, CodeLiveKeyInSandbox, CodeOlderKeyInSandbox,
	CodeInvalidEnvironment, CodeInvalidBaseURL, CodeEnvironmentConflictsWithBaseURL, CodeInvalidAppInfo,
	CodeInvalidOption, CodeCallOnlyOption, CodeInBrowser,
}

// refusalOf makes the case's client and returns its error.
func refusalOf(t *testing.T, c codeCase) *Error {
	t.Helper()
	clearEnvironment(t)
	for name, value := range c.env {
		t.Setenv(name, value)
	}
	if c.goos != "" {
		old := goos
		goos = c.goos
		defer func() { goos = old }()
	}
	client, err := NewClient(c.opts...)
	if err == nil {
		t.Fatalf("the client was made: %v", client)
	}
	return asError(t, err)
}

func TestEveryCodeHasACaseAndEveryCaseOneOfTheCodes(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range codeCases {
		seen[c.code] = true
	}
	for _, code := range everyCode {
		if !seen[code] {
			t.Errorf("no case for %s", code)
		}
		delete(seen, code)
	}
	for code := range seen {
		t.Errorf("a case's code %s is not a Code constant", code)
	}
}

func TestAClientThatCannotBeMadeSaysWhyWithACode(t *testing.T) {
	for _, c := range codeCases {
		t.Run(c.name, func(t *testing.T) {
			e := refusalOf(t, c)
			if e.Kind != KindNotSent || !errors.Is(e, ErrNotSent) {
				t.Errorf("kind %v", e.Kind)
			}
			if e.Code != c.code {
				t.Errorf("code %q, want %q", e.Code, c.code)
			}
			if !regexp.MustCompile(c.message).MatchString(e.Error()) {
				t.Errorf("message %q does not match %s", e.Error(), c.message)
			}
			if e.Status != 0 || e.RequestID != "" || e.CorrelationID != "" {
				t.Errorf("request fields on a refusal sent nowhere: %#v", e)
			}
		})
	}
}

func TestARefusalsCodeIsWhereALogReadsItAndTheKeyIsNot(t *testing.T) {
	for _, c := range codeCases {
		t.Run(c.name, func(t *testing.T) {
			e := refusalOf(t, c)
			got, err := json.Marshal(e)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(got), fmt.Sprintf(`"code":%q`, c.code)) {
				t.Errorf("JSON: %s", got)
			}
			var logged bytes.Buffer
			slog.New(slog.NewJSONHandler(&logged, nil)).Warn("refused", "error", e)
			if !strings.Contains(logged.String(), fmt.Sprintf(`"code":%q`, c.code)) {
				t.Errorf("slog: %s", logged.String())
			}
			if !strings.Contains(fmt.Sprintf("%#v", e), fmt.Sprintf("Code: %q", c.code)) {
				t.Errorf("GoString: %#v", e)
			}
			for _, text := range []string{e.Error(), string(got), logged.String(), fmt.Sprintf("%#v", e), fmt.Sprintf("%+v", e)} {
				if strings.Contains(text, "key_that_must_never_be_printed") {
					t.Errorf("the key is printed: %s", text)
				}
			}
		})
	}
}

// --- what keeps no code -------------------------------------------------------------------------

func TestARefusalRaisedByACallCarriesNoCode(t *testing.T) {
	clearEnvironment(t)
	c := onRecorder(t, record(fhirReply(200, map[string]any{"resourceType": "CapabilityStatement"})), WithAPIKey(markerKey))
	_, err := c.FHIRMetadata(context.Background(), FHIRMetadataParams{}, WithCorrelationID("not-a-uuid"))
	e := asError(t, err)
	if e.Kind != KindNotSent || e.Code != "" {
		t.Errorf("%#v", e)
	}
	got, _ := json.Marshal(e)
	if strings.Contains(string(got), `"code"`) {
		t.Errorf("%s", got)
	}
	// And a call's option that is for NewClient only.
	_, err = c.FHIRMetadata(context.Background(), FHIRMetadataParams{}, WithAppInfo(AppInfo{Name: "a"}))
	if e := asError(t, err); e.Kind != KindNotSent || e.Code != "" {
		t.Errorf("%#v", e)
	}
}

func TestA403StillCarriesTheAPIsOwnCode(t *testing.T) {
	clearEnvironment(t)
	c := onRecorder(t, record(fhirReply(403, coded("phi-agreement-required", apiError))), WithAPIKey(markerKey), WithMaxRetries(0), WithTimeout(time.Second))
	_, err := c.FHIRMetadata(context.Background(), FHIRMetadataParams{})
	if e := asError(t, err); e.Kind != KindForbidden || e.Code != "phi-agreement-required" {
		t.Errorf("%#v", e)
	}
}

func TestAClientThatCanBeMadeIsMade(t *testing.T) {
	clearEnvironment(t)
	c, err := NewClient(WithEnvironmentSandbox(), WithHTTPClient(&http.Client{}))
	if err != nil || c.Environment() != EnvironmentSandbox {
		t.Fatalf("%v %v", c, err)
	}
}
