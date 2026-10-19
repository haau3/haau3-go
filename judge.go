package haau3

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/haau3/haau3-go/fhir"
)

// What an answer means: the answer itself, or the error it is. A single call and each entry of a
// batch are judged alike, word for word as the TypeScript and Python SDKs judge them.

// longestWait is the longest Retry-After the client waits out. A longer one fails at once and says when.
const longestWait = 60

var (
	digits        = regexp.MustCompile(`^[0-9]+$`)
	plausibleType = regexp.MustCompile(`^[A-Z][A-Za-z]{0,63}$`)
)

// jsonBody is the body when its type says JSON and it parses, else nil.
func jsonBody(content []byte, contentType string) []byte {
	if !strings.Contains(strings.ToLower(contentType), "json") || !json.Valid(content) {
		return nil
	}
	return content
}

func isNull(body []byte) bool { return bytes.Equal(bytes.TrimSpace(body), []byte("null")) }

// resourceType is the body's resourceType when it is a string, and whether it is one.
func resourceType(body []byte) (string, bool) {
	var probe struct {
		ResourceType json.RawMessage `json:"resourceType"`
	}
	if json.Unmarshal(body, &probe) != nil || len(probe.ResourceType) == 0 {
		return "", false
	}
	var t string
	if json.Unmarshal(probe.ResourceType, &t) != nil {
		return "", false
	}
	return t, true
}

func withArticle(t string) string {
	if t != "" && strings.ContainsRune("AEIOU", rune(t[0])) {
		return "an " + t
	}
	return "a " + t
}

// describeAnswer names an answer in a message: its resource type when it has a plausible one, never a value.
func describeAnswer(body []byte) string {
	if t, ok := resourceType(body); ok && plausibleType.MatchString(t) {
		return withArticle(t)
	}
	return "something that is not a FHIR resource"
}

// readErrorBody reads an error body as the SDK does: an OperationOutcome under the FHIR base, a
// problem elsewhere.
func readErrorBody(body []byte) (*fhir.OperationOutcome, *Problem) {
	if body == nil {
		return nil, nil
	}
	if t, _ := resourceType(body); t == "OperationOutcome" {
		var outcome fhir.OperationOutcome
		if json.Unmarshal(body, &outcome) == nil {
			return &outcome, nil
		}
		return nil, nil
	}
	var probe struct {
		Type   json.RawMessage `json:"type"`
		Status json.RawMessage `json:"status"`
	}
	if json.Unmarshal(body, &probe) != nil {
		return nil, nil
	}
	var kind string
	var status int
	if json.Unmarshal(probe.Type, &kind) != nil || json.Unmarshal(probe.Status, &status) != nil {
		return nil, nil
	}
	var problem Problem
	if json.Unmarshal(body, &problem) != nil {
		return nil, nil
	}
	return nil, &problem
}

// retryAfterSeconds reads a Retry-After header that holds a number of seconds. An HTTP date is not read.
func retryAfterSeconds(value string) (int, bool) {
	value = strings.TrimSpace(value)
	if !digits.MatchString(value) || len(value) > 9 {
		return 0, false
	}
	var n int
	_, _ = fmt.Sscan(value, &n)
	return n, true
}

// random gives the jitter between retries; tests replace it.
var random = rand.Float64

// backoff is how long to wait after a failed attempt: about 0.5 seconds, then 1, 2 and so on,
// never past 8, with jitter.
func backoff(attempt int) time.Duration {
	ceiling := min(8.0, 0.5*float64(int(1)<<(attempt-1)))
	return time.Duration(ceiling * (0.5 + random()*0.5) * float64(time.Second))
}

// waitAfterAnswer is how long to wait before retrying an answer, and false to keep it. Only a 429
// or a 5xx is retried.
func waitAfterAnswer(attempt, maxRetries, status int, retryAfter string) (time.Duration, bool) {
	if (status != 429 && status < 500) || attempt > maxRetries {
		return 0, false
	}
	seconds, ok := retryAfterSeconds(retryAfter)
	if !ok {
		return backoff(attempt), true
	}
	if seconds > longestWait {
		return 0, false
	}
	return time.Duration(seconds) * time.Second, true
}

// waitAfterFailure is how long to wait before retrying after a timeout or a network failure, and
// false when no attempts are left.
func waitAfterFailure(attempt, maxRetries int) (time.Duration, bool) {
	if attempt > maxRetries {
		return 0, false
	}
	return backoff(attempt), true
}

// judgeContext is what came with an answer.
type judgeContext struct {
	requestID     string
	correlationID string
	attempts      int
	duration      time.Duration
	retryAfter    string
	// missing is how a success with no body is described: "a body that is not JSON", "no resource".
	missing string
	// expects is the FHIR resource types a success may be. Empty checks no type.
	expects []string
	// entry is an answer inside a batch: the key is checked once for the whole batch, so a 401 or
	// 403 on one entry is a refusal of that entry, not of the key.
	entry bool
}

func noAnswerReason(status int) Reason {
	switch status {
	case 429:
		return ReasonRateLimited
	case 502, 503:
		return ReasonUnavailable
	case 504:
		return ReasonTimeout
	}
	return ReasonServerError
}

func attemptsText(n int) string {
	if n == 1 {
		return "1 attempt"
	}
	return fmt.Sprintf("%d attempts", n)
}

// judge says what one answer means: nil for an answer, or the error it is. name says which request
// it was, in words that carry no value a caller sent: the method and the operation's path with its
// `{id}` left in. body is nil when the answer was not JSON.
func judge(name string, status int, body []byte, c judgeContext) *Error {
	answered := func(kind Kind, message string) *Error {
		e := &Error{
			Kind: kind, Status: status, Attempts: c.attempts, Duration: c.duration,
			RequestID: c.requestID, CorrelationID: c.correlationID, message: message,
		}
		if status >= 400 {
			outcome, problem := readErrorBody(body)
			if outcome != nil {
				e.outcome = func() *fhir.OperationOutcome { return outcome }
			}
			if problem != nil {
				e.problem = func() *Problem { return problem }
			}
		}
		return e
	}
	noAnswer := func(message string, reason Reason) *Error {
		e := answered(KindNoAnswer, message)
		e.Reason = reason
		return e
	}
	switch {
	case 200 <= status && status < 300:
		if body == nil {
			return noAnswer(fmt.Sprintf("%s answered %d with %s.", name, status, c.missing), ReasonUnreadable)
		}
		if len(c.expects) > 0 {
			if t, ok := resourceType(body); !ok || !slices.Contains(c.expects, t) {
				wanted := make([]string, len(c.expects))
				for i, t := range c.expects {
					wanted[i] = withArticle(t)
				}
				return noAnswer(fmt.Sprintf("%s answered %s where %s was expected.", name, describeAnswer(body), strings.Join(wanted, " or ")), ReasonUnreadable)
			}
		}
		return nil
	case status == 429 || status >= 500:
		reason := noAnswerReason(status)
		e := noAnswer(fmt.Sprintf("%s got no answer (%s, %d) after %s.", name, reason, status, attemptsText(c.attempts)), reason)
		if seconds, ok := retryAfterSeconds(c.retryAfter); ok {
			e.RetryAfter, e.hasRetryAfter = time.Duration(seconds)*time.Second, true
		}
		return e
	case status < 400:
		return noAnswer(fmt.Sprintf("%s answered %d, which is not an answer.", name, status), ReasonUnreadable)
	case (status == 401 || status == 403) && !c.entry:
		return answered(KindAuth, fmt.Sprintf("%s was refused: the API key was not accepted (%d).", name, status))
	}
	return answered(KindRefused, fmt.Sprintf("%s was refused (%d).", name, status))
}

// unreadable is an answer of the right type that does not read as it: a list where FHIR R4 has an
// object, say. The message names the element and what it should be, never its value.
func unreadable(name, what string, err error, c judgeContext, status int) *Error {
	message := fmt.Sprintf("%s answered %s that could not be read.", name, what)
	if where := readFailure(err); where != "" {
		message = fmt.Sprintf("%s answered %s that could not be read: %s.", name, what, where)
	}
	return &Error{
		Kind: KindNoAnswer, Reason: ReasonUnreadable, Status: status, Attempts: c.attempts, Duration: c.duration,
		RequestID: c.requestID, CorrelationID: c.correlationID, message: message,
	}
}

// readFailure says where a body did not read as its type, from encoding/json's error: the element's
// path and the JSON it should have been. It never repeats a value; json's own message can.
func readFailure(err error) string {
	var place []string
	for {
		var element *fhir.ElementError
		if !errors.As(err, &element) {
			break
		}
		place = append(place, element.Element)
		err = element.Err
	}
	// A time's own error repeats the text it could not read, so only its kind is said.
	if timeErr := (*time.ParseError)(nil); errors.As(err, &timeErr) {
		return "a time is not RFC 3339"
	}
	var typeErr *json.UnmarshalTypeError
	if !errors.As(err, &typeErr) {
		return ""
	}
	// Each resource is read through a type of its own named plain, which json counts in the path.
	for _, part := range strings.Split(typeErr.Field, ".") {
		if part != "" && part != "plain" {
			place = append(place, part)
		}
	}
	where := "its body"
	if len(place) > 0 {
		where = strings.Join(place, ".")
	}
	return where + " is not " + jsonKind(typeErr.Type)
}

var numberType = reflect.TypeFor[json.Number]()

func jsonKind(t reflect.Type) string {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil {
		return "what it should be"
	}
	if t == numberType {
		return "a number"
	}
	switch t.Kind() {
	case reflect.Slice, reflect.Array:
		return "a list"
	case reflect.Struct, reflect.Map:
		return "an object"
	case reflect.String:
		return "a string"
	case reflect.Bool:
		return "true or false"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "a whole number"
	case reflect.Float32, reflect.Float64:
		return "a number"
	}
	return "what it should be"
}
