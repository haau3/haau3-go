package haau3

// WithAppInfo names the caller's app in the User-Agent, after the SDK's own, on every request the
// client sends. A value the header cannot carry is refused by NewClient, before anything is sent,
// and the option is for NewClient only. A User-Agent given with WithHeader still replaces the whole
// value.

import (
	"context"
	"net/http"
	"regexp"
	"testing"
)

func TestTheAppIsAddedAfterTheSDKsOwnPart(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []Option
		want string
	}{
		{"a name", []Option{WithAppInfo(AppInfo{Name: "my-plugin"})}, userAgent + " my-plugin"},
		{"a version", []Option{WithAppInfo(AppInfo{Name: "my-plugin", Version: "1.2.0"})}, userAgent + " my-plugin/1.2.0"},
		{
			"a url",
			[]Option{WithAppInfo(AppInfo{Name: "my-plugin", Version: "1.2.0", URL: "https://example.com/my-plugin"})},
			userAgent + " my-plugin/1.2.0 (https://example.com/my-plugin)",
		},
		{"no option (control)", nil, userAgent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := record(fhirReply(200, plan))
			if _, err := testClient(t, rec, tc.opts...).ReadInsurancePlan(context.Background(), "H1"); err != nil {
				t.Fatal(err)
			}
			if got := rec.last().Header.Get("User-Agent"); got != tc.want {
				t.Errorf("User-Agent %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTheSDKsOwnPartIsTheLanguageAndTheRuntime(t *testing.T) {
	// The control above compares with userAgent; this holds userAgent to its form, so the comparison
	// cannot pass on an empty or changed value.
	rec := record(fhirReply(200, plan))
	if _, err := testClient(t, rec, WithAppInfo(AppInfo{Name: "my-plugin", Version: "1.2.0"})).ReadInsurancePlan(context.Background(), "H1"); err != nil {
		t.Fatal(err)
	}
	got := rec.last().Header.Get("User-Agent")
	if !regexp.MustCompile(`^haau3-go/` + regexp.QuoteMeta(Version) + ` \(go (1\.\d+|devel)\) my-plugin/1\.2\.0$`).MatchString(got) {
		t.Errorf("User-Agent %q", got)
	}
}

func TestTheAppIsSentOnEveryAttemptAndByRequestAndBatch(t *testing.T) {
	want := userAgent + " my-plugin/1.2.0"
	rec := record(
		reply{status: 503, body: map[string]any{}, header: map[string]string{"Retry-After": "0"}},
		fhirReply(200, plan),
		plainReply(200, map[string]any{}),
		fhirReply(200, batchResponse(map[string]any{"resource": plan, "response": map[string]any{"status": "200 OK"}})),
	)
	c := testClient(t, rec, WithAppInfo(AppInfo{Name: "my-plugin", Version: "1.2.0"}), WithMaxRetries(1))
	ctx := context.Background()
	if _, err := c.ReadInsurancePlan(ctx, "H1"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Request(ctx, RawRequest{Method: "GET", Path: "/v1/scheduling/publishers"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Batch(ctx, []Entry{Calls.ReadInsurancePlan("H1")}); err != nil {
		t.Fatal(err)
	}
	if rec.count() != 4 {
		t.Fatalf("%d requests, want 4", rec.count())
	}
	for i, req := range rec.requests {
		if got := req.Header.Get("User-Agent"); got != want {
			t.Errorf("request %d: User-Agent %q, want %q", i+1, got, want)
		}
	}
}

func TestAUserAgentFromWithHeaderStillReplacesItWhole(t *testing.T) {
	for _, where := range []string{"client", "call"} {
		t.Run(where, func(t *testing.T) {
			rec := record(fhirReply(200, plan))
			header := WithHeader("User-Agent", "something-else/9")
			opts := []Option{WithAppInfo(AppInfo{Name: "my-plugin", Version: "1.2.0"})}
			var call []Option
			if where == "client" {
				opts = append(opts, header)
			} else {
				call = append(call, header)
			}
			if _, err := testClient(t, rec, opts...).ReadInsurancePlan(context.Background(), "H1", call...); err != nil {
				t.Fatal(err)
			}
			if got := rec.last().Header.Get("User-Agent"); got != "something-else/9" {
				t.Errorf("User-Agent %q", got)
			}
		})
	}
}

func TestWithAppInfoIsForNewClientOnly(t *testing.T) {
	rec := record(fhirReply(200, plan))
	_, err := testClient(t, rec).ReadInsurancePlan(context.Background(), "H1", WithAppInfo(AppInfo{Name: "my-plugin"}))
	if e := asError(t, err); e.Kind != KindNotSent || e.Error() != "GET /v1/fhir/r4/InsurancePlan/{id} was not sent: WithAppInfo is for NewClient only." {
		t.Errorf("%v", e)
	}
	if rec.count() != 0 {
		t.Errorf("%d requests sent", rec.count())
	}
}

func TestWhatTheHeaderCannotCarryIsRefusedByNewClient(t *testing.T) {
	for _, tc := range []struct {
		name    string
		info    AppInfo
		message string
	}{
		{"no name", AppInfo{Version: "1.2.0"}, `^AppInfo\.Name is required: your app's name, such as "my-plugin"\.$`},
		{"a name with a space", AppInfo{Name: "my plugin"}, `^AppInfo\.Name takes letters, digits .*: "my plugin" is not one\.$`},
		{"a name with a slash", AppInfo{Name: "my/plugin"}, `^AppInfo\.Name takes .*: "my/plugin" is not one\.$`},
		{"a name with a line break", AppInfo{Name: "my-plugin\r\nX-Injected: 1"}, `^AppInfo\.Name takes `},
		{"a name ending in a line break", AppInfo{Name: "my-plugin\n"}, `^AppInfo\.Name takes `},
		{"a version with a space", AppInfo{Name: "my-plugin", Version: "1.2 beta"}, `^AppInfo\.Version takes .*such as "1\.2\.0": "1\.2 beta" is not one\.$`},
		{"a version with a slash", AppInfo{Name: "my-plugin", Version: "1/2"}, `^AppInfo\.Version takes `},
		{"a url that is not http or https", AppInfo{Name: "my-plugin", URL: "ftp://example.com"}, `^AppInfo\.URL takes an http or https URL .*: "ftp://example\.com" is not one\.$`},
		{"a url that is not a URL", AppInfo{Name: "my-plugin", URL: "example.com"}, `^AppInfo\.URL takes `},
		{"a url with a parenthesis", AppInfo{Name: "my-plugin", URL: "https://example.com/a(b)"}, `^AppInfo\.URL takes `},
		{"a url with a space", AppInfo{Name: "my-plugin", URL: "https://example.com/a b"}, `^AppInfo\.URL takes `},
		{"a url with a backslash", AppInfo{Name: "my-plugin", URL: `https://example.com/a\b`}, `^AppInfo\.URL takes `},
		{"a url beyond ASCII", AppInfo{Name: "my-plugin", URL: "https://example.com/é"}, `^AppInfo\.URL takes `},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := record(fhirReply(200, plan))
			_, err := NewClient(WithAPIKey(testKey), WithHTTPClient(&http.Client{Transport: rec}), WithAppInfo(tc.info))
			if e := asError(t, err); e.Kind != KindNotSent || !regexp.MustCompile(tc.message).MatchString(e.Error()) {
				t.Errorf("%v", e)
			}
			if rec.count() != 0 {
				t.Errorf("%d requests sent", rec.count())
			}
		})
	}
}

func TestEveryCharacterATokenMayHoldIsTaken(t *testing.T) {
	rec := record(fhirReply(200, plan))
	c := testClient(t, rec, WithAppInfo(AppInfo{Name: "a-Z_0.9~!#$%&'*+^`|", Version: "1.0.0-rc.1+build.5"}))
	if _, err := c.ReadInsurancePlan(context.Background(), "H1"); err != nil {
		t.Fatal(err)
	}
	if got, want := rec.last().Header.Get("User-Agent"), userAgent+" a-Z_0.9~!#$%&'*+^`|/1.0.0-rc.1+build.5"; got != want {
		t.Errorf("User-Agent %q, want %q", got, want)
	}
}
