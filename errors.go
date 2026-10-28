package haau3

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"time"

	"github.com/haau3/haau3-go/fhir"
)

// Kind is which of the SDK's errors an Error is.
type Kind int

const (
	// KindNotSent is a request the SDK refused to send: no API key, a URL on another host than the
	// client's base URL, a correlation id that is not a UUID, a required parameter left empty.
	KindNotSent Kind = iota + 1
	// KindAuth is the API not accepting the key (401): none was sent, it is not one the API knows,
	// or it was revoked. A 403 is never KindAuth: the key was accepted, and the error is
	// KindForbidden.
	KindAuth
	// KindRefused is the API answering and refusing the request: a parameter it does not take, a
	// record that does not exist (404), a page that no longer exists (410), a body that is too
	// large (413). Status says which, and the body says why.
	KindRefused
	// KindNoAnswer is a request that got no answer, after every attempt the client was allowed.
	// Reason says why. It never means "no results": an empty search answers with an empty Bundle
	// or list, not with this error.
	KindNoAnswer
	// KindForbidden is the API accepting the key and refusing the request (403). The key itself is
	// real, so there is nothing to rotate. Code says why when the API names it, and the message
	// says it in words:
	//
	//   - "test-key-outside-sandbox": a test key works only in the Sandbox. Send a live key.
	//   - "live-key-in-sandbox": a live key does not work in the Sandbox. Send a test key.
	//   - "phi-agreement-required": the organization may not send patient data, and not every
	//     resource in the request is labelled as test data. MarkAsTestData labels made-up data.
	//
	// Without a code, the key's organization may not make the request, or the operation is not yet
	// open to it; the body says why, in the API's words.
	KindForbidden
)

func (k Kind) String() string {
	switch k {
	case KindNotSent:
		return "not-sent"
	case KindAuth:
		return "auth"
	case KindRefused:
		return "refused"
	case KindNoAnswer:
		return "no-answer"
	case KindForbidden:
		return "forbidden"
	}
	return "unknown"
}

func (k Kind) goName() string {
	switch k {
	case KindNotSent:
		return "haau3.KindNotSent"
	case KindAuth:
		return "haau3.KindAuth"
	case KindRefused:
		return "haau3.KindRefused"
	case KindNoAnswer:
		return "haau3.KindNoAnswer"
	case KindForbidden:
		return "haau3.KindForbidden"
	}
	return fmt.Sprintf("haau3.Kind(%d)", int(k))
}

// errorName is the name the TypeScript and Python SDKs give the same error, so a log reads alike
// whichever language wrote it.
func (e *Error) errorName() string { return e.Kind.errorName() }

func (k Kind) errorName() string {
	switch k {
	case KindAuth:
		return "AuthError"
	case KindRefused:
		return "RefusedError"
	case KindNoAnswer:
		return "NoAnswerError"
	case KindForbidden:
		return "ForbiddenError"
	}
	return "Haau3Error"
}

// Reason is why a request got no answer.
type Reason string

const (
	// ReasonTimeout is an attempt that ran past its timeout, or the API answering 504.
	ReasonTimeout Reason = "timeout"
	// ReasonNetwork is a request that never reached the API, or a connection that failed.
	ReasonNetwork Reason = "network"
	// ReasonRateLimited is the API answering 429. RetryAfter says when to try again.
	ReasonRateLimited Reason = "rate-limited"
	// ReasonUnavailable is the API, or a source it reads, that could not answer (502, 503).
	ReasonUnavailable Reason = "unavailable"
	// ReasonServerError is the API failing (500).
	ReasonServerError Reason = "server-error"
	// ReasonUnreadable is an answer that is not one: a body that is not JSON, a FHIR resource of
	// another type than the operation answers with, or one that does not read as its type.
	ReasonUnreadable Reason = "unreadable"
)

// Error is every error the SDK returns, but for your own context's: a cancelled or expired ctx
// returns an error that errors.Is reports as the context's own.
//
//	plan, err := client.ReadInsurancePlan(ctx, id)
//	var e *haau3.Error
//	if errors.As(err, &e) {
//		switch e.Kind {
//		case haau3.KindRefused: // e.Status, e.Outcome()
//		case haau3.KindForbidden: // e.Code
//		case haau3.KindNoAnswer: // e.Reason, e.RetryAfter, e.Detail
//		}
//	}
//
// An Error never prints the body the API answered with, by any of fmt's verbs, by slog, or by
// json.Marshal: Outcome and Problem are the only ways to read it. Its message names the request by
// its method and the operation's path, never by a value you sent.
type Error struct {
	// Kind says which error this is.
	Kind Kind
	// Status is the HTTP status the API answered with, or 0 when it did not answer.
	Status int
	// StatusText is that status's text: the answer's own, or the standard one when it came with
	// none, as HTTP/2 answers do. Empty when the API did not answer.
	StatusText string
	// Code is the API's own code for why it refused a request with a 403 (KindForbidden), from its
	// api-error code system, when it names one: "test-key-outside-sandbox" (a test key works only
	// in the Sandbox), "live-key-in-sandbox" (a live key does not work there),
	// "phi-agreement-required" (patient data not all labelled as test data).
	Code string
	// Reason says why there was no answer, for KindNoAnswer.
	Reason Reason
	// RetryAfter is how long to wait before trying again, when the API said (its Retry-After header).
	RetryAfter time.Duration
	// Attempts is how many attempts were made: 0 for a request that was not sent.
	Attempts int
	// Duration is from the first attempt to the last answer, waits between attempts included.
	Duration time.Duration
	// RequestID is the X-Request-Id the API answered with. Quote it when you ask for help.
	RequestID string
	// CorrelationID is the id you sent with WithCorrelationID, as the API returned it in
	// X-Correlation-Id.
	CorrelationID string
	// Detail is, for a timeout or a network failure, what failed, safe to log: "dial: connection
	// refused". It never holds an address.
	Detail string

	message       string
	hasRetryAfter bool
	outcome       func() *fhir.OperationOutcome
	problem       func() *Problem
	cause         error
}

// Error is what happened, naming the request but never a value you sent or the body.
func (e *Error) Error() string { return e.message }

// Unwrap is the transport error behind a no-answer, so errors.Is(err, context.DeadlineExceeded)
// and errors.As(err, &netErr) work. Its own message names the request's URL, never a body or the
// key; the SDK never prints it.
func (e *Error) Unwrap() error { return e.cause }

// Is reports whether e is of the kind a sentinel names: errors.Is(err, haau3.ErrNoAnswer).
func (e *Error) Is(target error) bool {
	k, ok := target.(*kindError)
	return ok && k.kind == e.Kind
}

// Outcome is the OperationOutcome the FHIR base answered an error with, or nil. It is never
// printed when the error is.
func (e *Error) Outcome() *fhir.OperationOutcome {
	if e.outcome == nil {
		return nil
	}
	return e.outcome()
}

// Problem is the problem (RFC 9457) the other services answered an error with, or nil. It is never
// printed when the error is.
func (e *Error) Problem() *Problem {
	if e.problem == nil {
		return nil
	}
	return e.problem()
}

// Format prints the error's message for every verb, and %#v as GoString does, so no verb reaches
// a field that holds a body.
func (e *Error) Format(f fmt.State, verb rune) {
	switch {
	case verb == 'v' && f.Flag('#'):
		_, _ = io.WriteString(f, e.GoString())
	case verb == 'q':
		_, _ = io.WriteString(f, strconv.Quote(e.message))
	default:
		_, _ = io.WriteString(f, e.message)
	}
}

// GoString is the error's log-safe fields as Go syntax.
func (e *Error) GoString() string {
	var b bytes.Buffer
	fmt.Fprintf(&b, "&haau3.Error{Kind: %s", e.Kind.goName())
	if e.Status != 0 {
		fmt.Fprintf(&b, ", Status: %d", e.Status)
	}
	if e.StatusText != "" {
		fmt.Fprintf(&b, ", StatusText: %q", e.StatusText)
	}
	if e.Code != "" {
		fmt.Fprintf(&b, ", Code: %q", e.Code)
	}
	if e.Reason != "" {
		fmt.Fprintf(&b, ", Reason: %q", string(e.Reason))
	}
	if e.hasRetryAfter {
		fmt.Fprintf(&b, ", RetryAfter: %d", int64(e.RetryAfter))
	}
	if e.Kind != KindNotSent {
		fmt.Fprintf(&b, ", Attempts: %d, Duration: %d", e.Attempts, int64(e.Duration))
	}
	for _, f := range [][2]string{{"RequestID", e.RequestID}, {"CorrelationID", e.CorrelationID}, {"Detail", e.Detail}} {
		if f[1] != "" {
			fmt.Fprintf(&b, ", %s: %q", f[0], f[1])
		}
	}
	b.WriteString("}")
	return b.String()
}

// LogValue is the error as slog logs it: the fields that are set and safe to log, never the body
// or the cause.
//
//	slog.Warn("the plan read failed", "error", err)
func (e *Error) LogValue() slog.Value { return slog.GroupValue(e.logged()...) }

// MarshalJSON is the error as a JSON object of the fields that are set and safe to log, the same
// keys LogValue gives and the TypeScript SDK's toJSON() gives.
func (e *Error) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("{")
	for i, a := range e.logged() {
		if i > 0 {
			b.WriteString(",")
		}
		key, _ := json.Marshal(a.Key)
		value, err := json.Marshal(a.Value.Any())
		if err != nil {
			return nil, err
		}
		b.Write(key)
		b.WriteString(":")
		b.Write(value)
	}
	b.WriteString("}")
	return b.Bytes(), nil
}

func (e *Error) logged() []slog.Attr {
	attrs := []slog.Attr{slog.String("name", e.errorName()), slog.String("message", e.message), slog.String("kind", e.Kind.String())}
	if e.Status != 0 {
		attrs = append(attrs, slog.Int("status", e.Status))
	}
	if e.StatusText != "" {
		attrs = append(attrs, slog.String("statusText", e.StatusText))
	}
	if e.Code != "" {
		attrs = append(attrs, slog.String("code", e.Code))
	}
	if e.Reason != "" {
		attrs = append(attrs, slog.String("reason", string(e.Reason)))
	}
	if e.hasRetryAfter {
		attrs = append(attrs, slog.Int64("retryAfter", int64(e.RetryAfter/time.Second)))
	}
	if e.Kind != KindNotSent {
		attrs = append(attrs, slog.Int("attempts", e.Attempts), slog.Int64("durationMs", milliseconds(e.Duration)))
	}
	if e.RequestID != "" {
		attrs = append(attrs, slog.String("requestId", e.RequestID))
	}
	if e.CorrelationID != "" {
		attrs = append(attrs, slog.String("correlationId", e.CorrelationID))
	}
	if e.Detail != "" {
		attrs = append(attrs, slog.String("detail", e.Detail))
	}
	return attrs
}

// milliseconds is a duration in whole milliseconds, rounded, as the other SDKs report it.
func milliseconds(d time.Duration) int64 { return int64((d + time.Millisecond/2) / time.Millisecond) }

type kindError struct{ kind Kind }

func (k *kindError) Error() string { return "haau3: " + k.kind.String() }

// Sentinels for errors.Is, one per Kind: errors.Is(err, haau3.ErrNoAnswer) is true for an *Error
// whose Kind is KindNoAnswer.
var (
	ErrNotSent   error = &kindError{KindNotSent}
	ErrAuth      error = &kindError{KindAuth}
	ErrRefused   error = &kindError{KindRefused}
	ErrNoAnswer  error = &kindError{KindNoAnswer}
	ErrForbidden error = &kindError{KindForbidden}
)

func notSent(message string) *Error { return &Error{Kind: KindNotSent, message: message} }
