package haau3

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// BatchCalls makes the entries of a batch: one method per operation a batch can carry, called as
// the client's method of the same name is, without ctx or options. Use it as Calls.
type BatchCalls struct{}

// Calls makes the entries of a batch:
//
//	plan := haau3.Calls.ReadInsurancePlan("H2406-013-000")
//	counties := haau3.Calls.ListInsurancePlanCounties(haau3.ListInsurancePlanCountiesParams{State: "NC"})
//	if err := client.Batch(ctx, []haau3.Entry{plan, counties}); err != nil {
//		return err // the batch as a whole failed
//	}
//	p, err := plan.Result() // an *fhir.InsurancePlan, or the error ReadInsurancePlan alone would have returned
var Calls BatchCalls

// Entry is one entry of a batch: a *Call that Calls made.
type Entry interface {
	entry() *entry
}

// entry is what every Call shares, whatever it answers with.
type entry struct {
	op      *operation
	url     string // relative to the FHIR base, with its query, as the batch sends it
	body    func() []byte
	refused error // why it cannot be sent, found when it was made
	// settle gives the entry its result: its answer's JSON, or its error. name says which entry
	// it was, by its place in the batch.
	settle func(name string, data []byte, c judgeContext, status int, err error)
}

// Call is one operation made into an entry of a batch, by Calls. After Client.Batch, Result gives
// its own answer, or the error the call alone would have returned. A Call is used by one Batch at
// a time; sent again, it holds its newest result.
type Call[T any] struct {
	e      entry
	data   T
	err    error
	result bool
}

func (c *Call[T]) entry() *entry { return &c.e }

// Result is the entry's answer, or its error: a refusal or a no-answer as the call alone would
// have returned, or the batch's own error when the batch as a whole failed.
func (c *Call[T]) Result() (T, error) {
	if !c.result {
		var zero T
		return zero, notSent(c.e.op.label() + " has not been sent: pass it to Client.Batch first.")
	}
	return c.data, c.err
}

// String names the entry by its operation, never a value it was given.
func (c *Call[T]) String() string { return "haau3.Call(" + c.e.op.label() + ")" }

// GoString is String, so %#v shows no body either.
func (c *Call[T]) GoString() string { return c.String() }

// Format prints String for every verb.
func (c *Call[T]) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, c.String()) }

func newCall[T any](op *operation, pathValues []string, q query, queryErr error, body any,
	decode func([]byte) (T, error)) *Call[T] {
	c := &Call[T]{e: entry{op: op}}
	c.e.settle = func(name string, data []byte, jc judgeContext, status int, err error) {
		var zero T
		c.data, c.err, c.result = zero, err, true
		if err != nil {
			return
		}
		value, decodeErr := decode(data)
		if decodeErr != nil {
			c.err = unreadable(name, what(op.expects, data), decodeErr, jc, status)
			return
		}
		c.data = value
	}
	if queryErr != nil {
		c.e.refused = queryErr
		return c
	}
	path, err := fillPath(op.path, pathValues)
	if err != nil {
		c.e.refused = err
		return c
	}
	if encoded := encodeQuery(q); encoded != "" {
		path += "?" + encoded
	}
	c.e.url = strings.TrimPrefix(path, fhirBase+"/")
	content, err := bodyContent(op, body)
	if err != nil {
		c.e.refused = err
		return c
	}
	if content != nil {
		c.e.body = func() []byte { return content }
	}
	return c
}

type batchRequest struct {
	Method string `json:"method"`
	URL    string `json:"url"`
}

type batchEntry struct {
	Request  batchRequest    `json:"request"`
	Resource json.RawMessage `json:"resource,omitempty"`
}

type batchBundle struct {
	ResourceType string       `json:"resourceType"`
	Type         string       `json:"type"`
	Entry        []batchEntry `json:"entry"`
}

var entryStatus = regexp.MustCompile(`^(\d{3})(\s|$)`)

const batchName = "POST /v1/fhir/r4"

// Batch sends several operations of the FHIR base as one request, each entry with its own answer
// or error, in the order given. Each entry is checked as the call alone is, and fails alone while
// the others answer; an entry that fails is not retried. The batch itself is retried as any call
// is, and Batch returns an error only when the batch as a whole failed: then every entry's Result
// gives that error too.
//
// The API takes up to 100 entries in up to 1 MiB and gives a batch 60 seconds, so send a longer
// list as several batches. Every operation on the FHIR base can go in a batch except FHIRBatch and
// the ones the spec marks x-haau3-batchable: false, which api.md names. WithResponseInfo gives the
// batch's own request id.
func (c *Client) Batch(ctx context.Context, entries []Entry, opts ...Option) error {
	err := c.batch(ctx, entries, opts)
	if err != nil {
		for _, e := range entries {
			if e != nil && e.entry() != nil && e.entry().settle != nil {
				e.entry().settle(batchName, nil, judgeContext{}, 0, err)
			}
		}
	}
	return err
}

func (c *Client) batch(ctx context.Context, entries []Entry, opts []Option) error {
	s, err := c.settingsFor(batchName, opts)
	if err != nil {
		return err
	}
	bundle := batchBundle{ResourceType: "Bundle", Type: "batch", Entry: make([]batchEntry, 0, len(entries))}
	for i, e := range entries {
		if e == nil || e.entry() == nil || e.entry().op == nil {
			return notSent(fmt.Sprintf("%s was not sent: entry %d of the batch was not made by haau3.Calls.", batchName, i+1))
		}
		en := e.entry()
		if en.refused != nil {
			return notSent(fmt.Sprintf("%s was not sent: entry %d of the batch, %s, cannot be: %v.", batchName, i+1, en.op.label(), en.refused))
		}
		be := batchEntry{Request: batchRequest{Method: en.op.method, URL: en.url}}
		if en.body != nil {
			be.Resource = en.body()
		}
		bundle.Entry = append(bundle.Entry, be)
	}
	url, err := c.urlOnBase(fhirBase, "", batchName)
	if err != nil {
		return err
	}
	content, err := jsonBytes(bundle)
	if err != nil {
		return notSent(batchName + " was not sent: its body cannot be written as JSON.")
	}
	a, err := c.send(ctx, "POST", url, content, s, batchName)
	if err != nil {
		return err
	}
	data, jc, e := answerOf(batchName, []string{"Bundle"}, a)
	if e != nil {
		return e
	}
	if err := readBatch(entries, data, jc, a.status); err != nil {
		return err
	}
	s.fill(a)
	return nil
}

type batchAnswer struct {
	Type  json.RawMessage `json:"type"`
	Entry json.RawMessage `json:"entry"`
}

type batchAnswerEntry struct {
	Resource json.RawMessage `json:"resource"`
	Response *struct {
		Status  json.RawMessage `json:"status"`
		Outcome json.RawMessage `json:"outcome"`
	} `json:"response"`
}

// readBatch gives each entry its result, paired by position as FHIR R4 says a batch-response is.
// An answer that is not a batch-response, or holds another number of entries, cannot be paired
// at all and is no answer for the whole batch.
func readBatch(entries []Entry, data []byte, jc judgeContext, status int) error {
	noAnswer := func(message string) *Error {
		return &Error{
			Kind: KindNoAnswer, Reason: ReasonUnreadable, Status: status, Attempts: jc.attempts, Duration: jc.duration,
			RequestID: jc.requestID, CorrelationID: jc.correlationID, message: message,
		}
	}
	var answer batchAnswer
	var kind string
	if json.Unmarshal(data, &answer) != nil || json.Unmarshal(answer.Type, &kind) != nil || kind != "batch-response" {
		return noAnswer(batchName + " answered a Bundle that is not a batch-response.")
	}
	var list []json.RawMessage
	if len(answer.Entry) > 0 && !isNull(answer.Entry) {
		if json.Unmarshal(answer.Entry, &list) != nil {
			return noAnswer(batchName + " answered a batch-response whose entries are not a list.")
		}
	}
	if len(list) != len(entries) {
		return noAnswer(fmt.Sprintf("%s answered %d entries for a batch of %d.", batchName, len(list), len(entries)))
	}
	for i, e := range entries {
		en := e.entry()
		name := fmt.Sprintf("%s (entry %d of %d)", en.op.label(), i+1, len(entries))
		ec := jc
		ec.missing, ec.expects, ec.entry, ec.retryAfter = "no resource", en.op.expects, true, ""
		code, body := entryAnswer(list[i])
		if code == 0 {
			en.settle(name, nil, ec, 0, &Error{
				Kind: KindNoAnswer, Reason: ReasonUnreadable, Attempts: jc.attempts, Duration: jc.duration,
				RequestID: jc.requestID, CorrelationID: jc.correlationID, message: name + " answered with no status.",
			})
			continue
		}
		if verdict := judge(name, code, body, ec); verdict != nil {
			en.settle(name, nil, ec, code, verdict)
			continue
		}
		en.settle(name, body, ec, code, nil)
	}
	return nil
}

// entryAnswer is one entry's status ("200", or "200 OK" as FHIR allows) and its body.
func entryAnswer(raw json.RawMessage) (int, []byte) {
	var e batchAnswerEntry
	if json.Unmarshal(raw, &e) != nil || e.Response == nil {
		return 0, nil
	}
	var status string
	if json.Unmarshal(e.Response.Status, &status) != nil {
		return 0, nil
	}
	m := entryStatus.FindStringSubmatch(status)
	if m == nil {
		return 0, nil
	}
	code, _ := strconv.Atoi(m[1])
	// A failed entry's OperationOutcome is its resource, or, as FHIR also allows, its response's outcome.
	body := []byte(e.Resource)
	if (len(body) == 0 || isNull(body)) && code >= 400 {
		body = e.Response.Outcome
	}
	if len(body) == 0 || isNull(body) {
		return code, nil
	}
	return code, body
}
