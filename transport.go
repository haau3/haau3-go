package haau3

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/haau3/haau3-go/fhir"
)

const fhirJSON = "application/fhir+json"

// userAgent names the SDK, its version and the Go it runs on: `haau3-go/0.1.0 (go 1.26)`.
var userAgent = "haau3-go/" + Version + " (go " + goVersion(runtime.Version()) + ")"

func goVersion(v string) string {
	v = strings.TrimPrefix(v, "go")
	if parts := strings.SplitN(v, ".", 3); len(parts) >= 2 && !strings.Contains(v, " ") {
		minor := parts[1]
		if end := strings.IndexFunc(minor, func(r rune) bool { return r < '0' || r > '9' }); end >= 0 {
			minor = minor[:end] // go1.28rc1 is 1.28
		}
		return parts[0] + "." + minor
	}
	return "devel"
}

// operation is one operation of the spec, as the generated methods describe it.
type operation struct {
	id        string
	method    string
	path      string   // in the spec, such as `/v1/fhir/r4/InsurancePlan/{id}`
	expects   []string // the FHIR resource types a success may be; empty checks no type
	bodyTypes []string // the resource types its body may be
	headers   []declaredHeader
}

// declaredHeader is a header parameter an operation declares, sent from an option of its own and
// checked against the spec's schema for it before sending.
type declaredHeader struct {
	wire      string // as the spec names it: `Payer-Authorization`
	option    string // the option it is sent from: WithPayerAuthorization
	maxLength int    // 0 when the spec sets none
	pattern   *regexp.Regexp
}

// label names a request in a message: `GET /v1/fhir/r4/InsurancePlan/{id}`, never a value sent.
func (o *operation) label() string { return o.method + " " + o.path }

// sleep waits between attempts, and stops when ctx does; tests replace it.
var sleep = func(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// answer is what came back from the last attempt. Its body is held in a function, so nothing that
// prints the struct prints the body.
type answer struct {
	status     int
	statusText string
	header     http.Header
	body       func() []byte
	attempts   int
	duration   time.Duration
}

func (a *answer) context(missing string, expects []string) judgeContext {
	return judgeContext{
		requestID: a.header.Get("X-Request-Id"), correlationID: a.header.Get("X-Correlation-Id"),
		attempts: a.attempts, duration: a.duration, retryAfter: a.header.Get("Retry-After"),
		missing: missing, expects: expects,
	}
}

func (a *answer) info() ResponseInfo {
	return ResponseInfo{
		Status: a.status, StatusText: a.statusText, Header: a.header,
		RequestID: a.header.Get("X-Request-Id"), CorrelationID: a.header.Get("X-Correlation-Id"),
		Attempts: a.attempts, Duration: a.duration,
	}
}

func (s *callSettings) fill(a *answer) {
	if s.info != nil {
		*s.info = a.info()
	}
}

// stopped is the error for a call whose own context ended: it is the context's, as errors.Is says.
func stopped(ctx context.Context, name string) error {
	return fmt.Errorf("%s stopped: %w", name, context.Cause(ctx))
}

// send sends one request with the call's settings, trying again after a timeout, a network
// failure, a 429 or a 5xx, and returns the last answer, whatever its status.
func (c *Client) send(ctx context.Context, method, url string, content []byte, s *callSettings, name string) (*answer, error) {
	if ctx == nil {
		return nil, notSent(name + " was not sent: its context is nil.")
	}
	speaksFHIR := isFHIR(c.pathOf(url))
	header := http.Header{}
	header.Set("Accept", "application/json")
	if speaksFHIR {
		header.Set("Accept", fhirJSON)
	}
	header.Set("Authorization", "Bearer "+s.key)
	header.Set("User-Agent", userAgent)
	if content != nil {
		header.Set("Content-Type", "application/json")
		if speaksFHIR {
			header.Set("Content-Type", fhirJSON)
		}
	}
	for _, h := range s.headers {
		header.Set(h[0], h[1])
	}
	// A header the operation declares is sent from its own option, over the same one from WithHeader.
	for _, h := range s.declared {
		header.Set(h[0], h[1])
	}
	if s.correlationID != "" {
		header.Set("X-Request-Id", s.correlationID)
	}
	started := time.Now()
	var failure error
	var reason Reason
	attempt := 0
	for {
		attempt++
		a, err := c.attempt(ctx, method, url, header, content, s.timeout)
		if err != nil && ctx.Err() != nil {
			return nil, stopped(ctx, name)
		}
		var wait time.Duration
		var again bool
		if err == nil {
			a.attempts, a.duration = attempt, time.Since(started)
			if wait, again = waitAfterAnswer(attempt, s.maxRetries, a.status, a.header.Get("Retry-After")); !again {
				return a, nil
			}
		} else {
			failure, reason = err, ReasonNetwork
			if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
				reason = ReasonTimeout
			}
			if wait, again = waitAfterFailure(attempt, s.maxRetries); !again {
				break
			}
		}
		if sleep(ctx, wait) != nil {
			return nil, stopped(ctx, name)
		}
	}
	return nil, &Error{
		Kind: KindNoAnswer, Reason: reason, Attempts: attempt, Duration: time.Since(started),
		Detail: detail(failure), cause: failure,
		message: fmt.Sprintf("%s got no answer (%s) after %s.", name, reason, attemptsText(attempt)),
	}
}

// attempt sends once and reads the whole answer, both within the attempt's timeout.
func (c *Client) attempt(ctx context.Context, method, url string, header http.Header, content []byte, timeout time.Duration) (*answer, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var reader io.Reader
	if content != nil {
		reader = bytes.NewReader(content)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, err
	}
	req.Header = header.Clone()
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	received, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	statusText := strings.TrimPrefix(resp.Status, strconv.Itoa(resp.StatusCode)+" ")
	if statusText == resp.Status || statusText == "" {
		statusText = http.StatusText(resp.StatusCode)
	}
	return &answer{
		status: resp.StatusCode, statusText: statusText, header: resp.Header,
		body: func() []byte { return received },
	}, nil
}

// answerOf is the JSON of a sent request's answer, or the error it is.
func answerOf(name string, expects []string, a *answer) ([]byte, judgeContext, *Error) {
	c := a.context("a body that is not JSON", expects)
	body := jsonBody(a.body(), a.header.Get("Content-Type"))
	if e := judge(name, a.status, body, c); e != nil {
		return nil, c, e
	}
	return body, c, nil
}

// call sends one operation and reads its answer as R: what every generated method does.
func call[R any](ctx context.Context, c *Client, op *operation, pathValues []string, q query, queryErr error,
	body any, decode func([]byte) (R, error), opts []Option) (R, error) {
	var zero R
	name := op.label()
	s, err := c.settingsFor(name, opts, op.headers...)
	if err != nil {
		return zero, err
	}
	url, content, err := c.prepare(op, pathValues, q, queryErr, body)
	if err != nil {
		return zero, err
	}
	a, err := c.send(ctx, op.method, url, content, s, name)
	if err != nil {
		return zero, err
	}
	data, jc, e := answerOf(name, op.expects, a)
	if e != nil {
		return zero, e
	}
	result, err := decode(data)
	if err != nil {
		return zero, unreadable(name, what(op.expects, data), err, jc, a.status)
	}
	s.fill(a)
	return result, nil
}

// what names an answer that did not read as its type: its resource type, or "a body".
func what(expects []string, data []byte) string {
	if len(expects) > 0 {
		return describeAnswer(data)
	}
	return "a body"
}

// prepare is an operation's URL and body, or the error of KindNotSent that refuses it.
func (c *Client) prepare(op *operation, pathValues []string, q query, queryErr error, body any) (string, []byte, error) {
	name := op.label()
	if queryErr != nil {
		return "", nil, notSent(fmt.Sprintf("%s was not sent: %v.", name, queryErr))
	}
	path, err := fillPath(op.path, pathValues)
	if err != nil {
		return "", nil, notSent(fmt.Sprintf("%s was not sent: %v.", name, err))
	}
	url, err := c.urlOnBase(path, encodeQuery(q), name)
	if err != nil {
		return "", nil, err
	}
	content, err := bodyContent(op, body)
	if err != nil {
		return "", nil, notSent(fmt.Sprintf("%s was not sent: %v.", name, err))
	}
	return url, content, nil
}

// bodyContent is a request body as JSON, refused when it is missing or of a type the operation
// does not take.
func bodyContent(op *operation, body any) ([]byte, error) {
	if op.bodyTypes == nil {
		return nil, nil
	}
	resource, ok := body.(fhir.Resource)
	if body == nil || !ok {
		return nil, errors.New("it takes a body, and was given none")
	}
	if t := resource.ResourceType(); !slices.Contains(op.bodyTypes, t) {
		return nil, fmt.Errorf("it takes %s as its body, not %s", strings.Join(withArticles(op.bodyTypes), " or "), withArticle(t))
	}
	content, err := jsonBytes(body)
	if err != nil {
		return nil, errors.New("its body cannot be written as JSON")
	}
	return content, nil
}

func withArticles(types []string) []string {
	out := make([]string, len(types))
	for i, t := range types {
		out[i] = withArticle(t)
	}
	return out
}

// bodyOf is a typed body as the shared code takes it: nil stays nil.
func bodyOf[T any](body *T) any {
	if body == nil {
		return nil
	}
	return body
}

// resourceBody is a body that may be one of several resource types.
func resourceBody(body fhir.Resource) any {
	if body == nil {
		return nil
	}
	return body
}

// decodeAs reads an answer as a T. JSON null is nil.
func decodeAs[T any](data []byte) (*T, error) {
	if isNull(data) {
		return nil, nil
	}
	v := new(T)
	if err := json.Unmarshal(data, v); err != nil {
		return nil, err
	}
	return v, nil
}
