package haau3

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"slices"
)

// CallParams is what Client.Get and Client.Post take beside the path: an operation's values, under
// the names the spec gives them.
type CallParams struct {
	// PathParams is a value for each {name} in the path, such as {"id": "Practitioner-1093209884"}
	// for /v1/fhir/r4/Practitioner/{id}. Each is encoded into the path, and the path as you wrote it,
	// {name} left in, is what a message names, so no value you send ends up in one. Every {name}
	// needs a value and every value a {name}; either missing is refused before anything is sent.
	PathParams map[string]string
	// Query is sent with the API's own parameter names, in name order.
	Query url.Values
	// Body is Post's: sent as it is when it is a string or a []byte, and as JSON otherwise. Get
	// refuses one.
	Body any
	// Expects lists the FHIR resource types the answer may be, such as []string{"Bundle"}, as each
	// operation's own method checks them. Any other answer is an error of KindNoAnswer with reason
	// "unreadable". Empty takes any answer.
	Expects []string
}

// Get sends a GET to a path on the API and decodes its answer into res, or returns the errors every
// operation's own method returns: KindAuth, KindForbidden, KindRefused or KindNoAnswer. For an
// operation this version of the module has no method for, or one you call by its path. res is a
// pointer to decode into, such as a *fhir.Practitioner, or nil to read nothing. WithResponseInfo
// gives the request id and timing too.
//
//	var practitioner fhir.Practitioner
//	err := client.Get(ctx, "/v1/fhir/r4/Practitioner/{id}", haau3.CallParams{
//		PathParams: map[string]string{"id": "Practitioner-1093209884"},
//		Expects:    []string{"Practitioner"},
//	}, &practitioner)
func (c *Client) Get(ctx context.Context, path string, params CallParams, res any, opts ...Option) error {
	return c.callPath(ctx, "GET", path, params, res, opts)
}

// Post sends a POST to a path on the API, with params.Body, and decodes its answer into res, or
// returns an error, as Get does.
func (c *Client) Post(ctx context.Context, path string, params CallParams, res any, opts ...Option) error {
	return c.callPath(ctx, "POST", path, params, res, opts)
}

func (c *Client) callPath(ctx context.Context, method, path string, params CallParams, res any, opts []Option) error {
	op := &operation{method: method, path: path, expects: params.Expects}
	name := c.label(op)
	values, err := pathValues(path, params.PathParams)
	if err != nil {
		return notSent(fmt.Sprintf("%s was not sent: %v.", name, err))
	}
	if method == "GET" && params.Body != nil {
		return notSent(name + " was not sent: a GET takes no body.")
	}
	if res != nil {
		if v := reflect.ValueOf(res); v.Kind() != reflect.Pointer || v.IsNil() {
			return notSent(fmt.Sprintf("%s was not sent: res must be a pointer to decode the answer into, not a %T.", name, res))
		}
	}
	s, err := c.settingsFor(name, opts)
	if err != nil {
		return err
	}
	filled, err := fillPath(c.sentPath(op), values)
	if err != nil {
		return notSent(fmt.Sprintf("%s was not sent: %v.", name, err))
	}
	target, err := c.urlOnBase(filled, encodeValues(params.Query), name)
	if err != nil {
		return err
	}
	var content []byte
	switch body := params.Body.(type) {
	case nil:
	case string:
		content = []byte(body)
	case []byte:
		content = body
	default:
		if content, err = jsonBytes(body); err != nil {
			return notSent(name + " was not sent: its body cannot be written as JSON.")
		}
	}
	a, err := c.send(ctx, method, target, content, s, name)
	if err != nil {
		return err
	}
	data, jc, e := answerOf(name, op.expects, a)
	if e != nil {
		return e
	}
	if res != nil {
		if err := json.Unmarshal(data, res); err != nil {
			return unreadable(name, what(op.expects, data), err, jc, a.status)
		}
	}
	s.fill(a)
	return nil
}

// pathValues is the value for each {name} in a path, in order. A {name} with no value, or a value
// with no {name}, is refused: either would send the request to another address than the one meant.
// The error names the placeholder, never a value.
func pathValues(path string, given map[string]string) ([]string, error) {
	var names []string
	for _, match := range placeholder.FindAllStringSubmatch(path, -1) {
		names = append(names, match[1])
	}
	values := make([]string, 0, len(names))
	for _, name := range names {
		value, ok := given[name]
		if !ok {
			return nil, fmt.Errorf("PathParams has no value for {%s}", name)
		}
		values = append(values, value)
	}
	for _, name := range slices.Sorted(func(yield func(string) bool) {
		for k := range given {
			if !yield(k) {
				return
			}
		}
	}) {
		if !slices.Contains(names, name) {
			return nil, fmt.Errorf("the path has no {%s} for PathParams' value", name)
		}
	}
	return values, nil
}
