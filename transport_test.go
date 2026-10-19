package haau3

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

var plan = map[string]any{"resourceType": "InsurancePlan", "id": "H1"}

func TestA5xxIsRetriedOnceThenAnswers(t *testing.T) {
	rec := record(fhirReply(503, outcome), fhirReply(200, plan))
	c := testClient(t, rec)
	waits := noWaits(t)
	var info ResponseInfo
	if _, err := c.ReadInsurancePlan(context.Background(), "H1", WithResponseInfo(&info)); err != nil {
		t.Fatal(err)
	}
	if rec.count() != 2 || info.Attempts != 2 || len(*waits) != 1 {
		t.Errorf("%d requests, %d attempts, waits %v", rec.count(), info.Attempts, *waits)
	}
}

func TestAFailureWithNoAnswerIsRetriedThenReturned(t *testing.T) {
	refused := &net.OpError{Op: "dial", Net: "tcp", Err: &net.AddrError{Err: "x", Addr: "10.0.0.1:443"}}
	rec := record(reply{err: refused})
	c := testClient(t, rec)
	_, err := c.ReadInsurancePlan(context.Background(), "H1")
	e := asError(t, err)
	if e.Kind != KindNoAnswer || e.Reason != ReasonNetwork || e.Attempts != 2 || rec.count() != 2 {
		t.Errorf("%#v", e)
	}
	if e.Error() != "GET /v1/fhir/r4/InsurancePlan/{id} got no answer (network) after 2 attempts." {
		t.Errorf("%q", e.Error())
	}
	if !errors.Is(err, refused) {
		t.Error("the cause is not reachable with errors.Is")
	}
}

func TestA429IsRetriedAndAShortRetryAfterIsWaited(t *testing.T) {
	rec := record(reply{status: 429, body: outcome, header: map[string]string{"Retry-After": "7"}}, fhirReply(200, plan))
	c := testClient(t, rec)
	waits := noWaits(t)
	if _, err := c.ReadInsurancePlan(context.Background(), "H1"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(*waits, []time.Duration{7 * time.Second}) {
		t.Errorf("waits %v", *waits)
	}
	rec = record(reply{status: 500, header: map[string]string{"Retry-After": "2"}}, fhirReply(200, plan))
	c = testClient(t, rec)
	waits = noWaits(t)
	if _, err := c.ReadInsurancePlan(context.Background(), "H1"); err != nil || !slices.Equal(*waits, []time.Duration{2 * time.Second}) {
		t.Errorf("waits %v, %v", *waits, err)
	}
}

func TestARetryAfterOver60SecondsFailsAtOnce(t *testing.T) {
	rec := record(reply{status: 429, body: outcome, header: map[string]string{"Retry-After": "61"}})
	c := testClient(t, rec)
	waits := noWaits(t)
	e := asError(t, func() error { _, err := c.ReadInsurancePlan(context.Background(), "H1"); return err }())
	if rec.count() != 1 || len(*waits) != 0 || e.RetryAfter != 61*time.Second || e.Attempts != 1 {
		t.Errorf("%#v, waits %v", e, *waits)
	}
}

func TestAnHTTPDateRetryAfterIsNotRead(t *testing.T) {
	rec := record(reply{status: 503, header: map[string]string{"Retry-After": "Wed, 21 Oct 2026 07:28:00 GMT"}})
	c := testClient(t, rec)
	waits := noWaits(t)
	e := asError(t, func() error { _, err := c.ReadInsurancePlan(context.Background(), "H1"); return err }())
	if rec.count() != 2 || len(*waits) != 1 || (*waits)[0] != 375*time.Millisecond || e.hasRetryAfter {
		t.Errorf("%#v, waits %v", e, *waits)
	}
}

func TestMaxRetriesZeroOnTheClientOrTheCallSendsOnce(t *testing.T) {
	rec := record(fhirReply(503, nil))
	_, _ = testClient(t, rec, WithMaxRetries(0)).ReadInsurancePlan(context.Background(), "H1")
	if rec.count() != 1 {
		t.Errorf("%d requests", rec.count())
	}
	rec = record(fhirReply(503, nil))
	_, _ = testClient(t, rec).ReadInsurancePlan(context.Background(), "H1", WithMaxRetries(0))
	if rec.count() != 1 {
		t.Errorf("%d requests", rec.count())
	}
}

func TestA4xxIsNeverRetried(t *testing.T) {
	rec := record(fhirReply(404, outcome))
	_, _ = testClient(t, rec).ReadInsurancePlan(context.Background(), "H1")
	if rec.count() != 1 {
		t.Errorf("%d requests", rec.count())
	}
}

func TestBackoffGrowsAndStopsAt8Seconds(t *testing.T) {
	noWaits(t)
	for attempt, want := range map[int]time.Duration{1: 375 * time.Millisecond, 2: 750 * time.Millisecond, 3: 1500 * time.Millisecond, 5: 6 * time.Second, 6: 6 * time.Second, 9: 6 * time.Second} {
		if got := backoff(attempt); got != want {
			t.Errorf("attempt %d: %v, want %v", attempt, got, want)
		}
	}
	random = func() float64 { return 0 }
	if backoff(1) != 250*time.Millisecond {
		t.Error(backoff(1))
	}
	random = func() float64 { return 0.9999999999 }
	if b := backoff(9); b < 7999*time.Millisecond || b > 8*time.Second {
		t.Error(b)
	}
}

func TestARedirectIsNotFollowedAndIsNotAnAnswer(t *testing.T) {
	var hits []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.URL.Path)
		if r.URL.Path == "/elsewhere" {
			w.Header().Set("Content-Type", "application/fhir+json")
			_, _ = io.WriteString(w, `{"resourceType":"InsurancePlan"}`)
			return
		}
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	}))
	defer server.Close()
	// Even a client of yours that follows redirects does not, as the SDK sends with it.
	follows := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return nil }}
	c, err := NewClient(WithAPIKey(testKey), WithBaseURL(server.URL), WithHTTPClient(follows))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.ReadInsurancePlan(context.Background(), "H1")
	e := asError(t, err)
	if e.Reason != ReasonUnreadable || e.Status != 302 || len(hits) != 1 {
		t.Errorf("%#v, %v", e, hits)
	}
	if e.Error() != "GET /v1/fhir/r4/InsurancePlan/{id} answered 302, which is not an answer." {
		t.Errorf("%q", e.Error())
	}
	if follows.CheckRedirect == nil {
		t.Error("your client was changed")
	}
}

func TestAnAttemptThatNeverAnswersTimesOut(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)
	noWaits(t)
	c, _ := NewClient(WithAPIKey(testKey), WithBaseURL(server.URL), WithTimeout(50*time.Millisecond))
	started := time.Now()
	_, err := c.ReadInsurancePlan(context.Background(), "H1")
	e := asError(t, err)
	if e.Reason != ReasonTimeout || e.Attempts != 2 || e.Detail != "timeout" || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("%#v", e)
	}
	if took := time.Since(started); took > 2*time.Second {
		t.Errorf("took %v", took)
	}
}

func TestAnAnswerThatKeepsDrippingTimesOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/fhir+json")
		w.WriteHeader(200)
		for {
			if _, err := io.WriteString(w, " "); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			select {
			case <-time.After(10 * time.Millisecond):
			case <-r.Context().Done():
				return
			}
		}
	}))
	defer server.Close()
	noWaits(t)
	c, _ := NewClient(WithAPIKey(testKey), WithBaseURL(server.URL), WithTimeout(80*time.Millisecond), WithMaxRetries(0))
	_, err := c.ReadInsurancePlan(context.Background(), "H1")
	if e := asError(t, err); e.Reason != ReasonTimeout {
		t.Errorf("%#v", e)
	}
}

func TestARefusedConnectionSaysSoWithoutAnAddress(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	noWaits(t)
	c, _ := NewClient(WithAPIKey(testKey), WithBaseURL("http://"+address), WithMaxRetries(0))
	_, err = c.ReadInsurancePlan(context.Background(), "H1")
	e := asError(t, err)
	if e.Reason != ReasonNetwork || !strings.HasPrefix(e.Detail, "dial: ") || strings.Contains(e.Detail, "127.0.0.1") {
		t.Errorf("%#v", e)
	}
	// The system's own error number is reachable; Windows numbers a refusal its own way.
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		t.Errorf("no error number reachable for %q", e.Detail)
	}
	if runtime.GOOS != "windows" && !errors.Is(err, syscall.ECONNREFUSED) {
		t.Errorf("errors.Is(err, syscall.ECONNREFUSED) is false for %q", e.Detail)
	}
}

func TestYourOwnContextEndingIsYourContextsError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	rec := record(reply{handle: func(*http.Request) reply { cancel(); return reply{err: context.Canceled} }})
	_, err := testClient(t, rec).ReadInsurancePlan(ctx, "H1")
	var e *Error
	if !errors.Is(err, context.Canceled) || errors.As(err, &e) || rec.count() != 1 {
		t.Errorf("%T %v, %d requests", err, err, rec.count())
	}
	if err.Error() != "GET /v1/fhir/r4/InsurancePlan/{id} stopped: context canceled" {
		t.Errorf("%q", err.Error())
	}
	// It is not waited out between attempts either.
	ctx, cancel = context.WithCancel(context.Background())
	rec = record(fhirReply(503, nil))
	c := testClient(t, rec)
	sleep = func(context.Context, time.Duration) error { cancel(); return context.Canceled }
	if _, err := c.ReadInsurancePlan(ctx, "H1"); !errors.Is(err, context.Canceled) || rec.count() != 1 {
		t.Errorf("%v, %d requests", err, rec.count())
	}
}

func TestHeadersSentAndReplaced(t *testing.T) {
	rec := record(fhirReply(200, plan))
	c := testClient(t, rec, WithHeader("X-Team", "a"))
	if _, err := c.ReadInsurancePlan(context.Background(), "H1", WithHeader("X-Call", "b")); err != nil {
		t.Fatal(err)
	}
	h := rec.last().Header
	if h.Get("X-Team") != "a" || h.Get("X-Call") != "b" || h.Get("Content-Type") != "" {
		t.Errorf("%v", h)
	}
}

func TestDefaults(t *testing.T) {
	c, err := NewClient(WithAPIKey(testKey))
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL() != "https://api.haau3.com" || c.timeout != 65*time.Second || c.maxRetries != 1 {
		t.Errorf("%s %v %d", c.BaseURL(), c.timeout, c.maxRetries)
	}
}
