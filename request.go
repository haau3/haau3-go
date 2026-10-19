package haau3

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// RawRequest is a request as you write it, for Client.Request.
type RawRequest struct {
	// Method is GET or POST.
	Method string
	// Path is a path such as /v1/scheduling/publishers, or a whole URL on the client's base URL.
	Path string
	// Query is sent with the API's own parameter names, in name order.
	Query url.Values
	// Body is sent as it is when it is a string or a []byte, and as JSON otherwise. Nil sends none.
	Body any
}

// RawResponse is what the API answered, whatever its status. Printing it shows its status, never
// its body.
type RawResponse struct {
	// Status is the HTTP status.
	Status int
	// StatusText is its reason phrase, such as "Not Found".
	StatusText string
	// Header is the answer's headers.
	Header http.Header
	// RequestID is the X-Request-Id the API answered with.
	RequestID string
	// CorrelationID is the id you sent with WithCorrelationID, as the API returned it.
	CorrelationID string
	// Attempts is how many attempts were made.
	Attempts int
	// Duration is from the first attempt to the answer, waits between attempts included.
	Duration time.Duration

	body func() []byte
}

// Body is the answer's body as it arrived.
func (r *RawResponse) Body() []byte {
	if r.body == nil {
		return nil
	}
	return r.body()
}

// String names the response by its status, never its body.
func (r *RawResponse) String() string {
	return fmt.Sprintf("haau3.RawResponse(%d %s)", r.Status, r.StatusText)
}

// GoString is String, so %#v shows no body either.
func (r *RawResponse) GoString() string { return r.String() }

// Format prints String for every verb.
func (r *RawResponse) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, r.String()) }

// Request sends any request and returns what the API answered, whatever its status, with its
// headers, request id, attempts and duration. A 429 or a 5xx is still tried again. It returns an
// error only when no answer arrived, or the request was not sent (a URL on another host, say).
//
//	raw, err := client.Request(ctx, haau3.RawRequest{Method: "GET", Path: "/v1/scheduling/publishers"})
//	fmt.Println(raw.Status, raw.StatusText, raw.RequestID)
func (c *Client) Request(ctx context.Context, req RawRequest, opts ...Option) (*RawResponse, error) {
	if req.Method != "GET" && req.Method != "POST" {
		return nil, notSent(fmt.Sprintf("Request takes GET or POST, not %q.", req.Method))
	}
	target, err := c.urlOnBase(req.Path, encodeValues(req.Query), req.Method+" "+req.Path)
	if err != nil {
		return nil, err
	}
	name := req.Method + " " + c.pathOf(target)
	s, err := c.settingsFor(name, opts)
	if err != nil {
		return nil, err
	}
	var content []byte
	switch body := req.Body.(type) {
	case nil:
	case string:
		content = []byte(body)
	case []byte:
		content = body
	default:
		if content, err = jsonBytes(body); err != nil {
			return nil, notSent(name + " was not sent: its body cannot be written as JSON.")
		}
	}
	a, err := c.send(ctx, req.Method, target, content, s, name)
	if err != nil {
		return nil, err
	}
	s.fill(a)
	info := a.info()
	return &RawResponse{
		Status: info.Status, StatusText: info.StatusText, Header: info.Header, RequestID: info.RequestID,
		CorrelationID: info.CorrelationID, Attempts: info.Attempts, Duration: info.Duration, body: a.body,
	}, nil
}
