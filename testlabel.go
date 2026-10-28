package haau3

import (
	"bytes"
	"encoding/json"
	"reflect"

	"github.com/haau3/haau3-go/fhir"
)

// testDataLabel is HL7's label for test data, as the API's examples carry it.
var testDataLabel = map[string]any{
	"system":  "http://terminology.hl7.org/CodeSystem/v3-ActReason",
	"code":    "HTEST",
	"display": "test health data",
}

// MarkAsTestData returns a copy of a request with HL7's test data label (HTEST, from
// v3-ActReason) in meta.security on the request and on every resource the API reads in it: each
// Bundle entry, each Parameters resource part at any depth, and the same again inside a Bundle or
// Parameters found there. A resource contained in another goes with the one that contains it, so it
// is left as it is. Labels already there are kept, and a resource that already carries the label
// gets no second one. The request you pass is never changed, and what comes back is of its type.
//
// Without an agreement for patient data, the operations that receive it accept only test data
// labelled as such, and refuse anything else with a KindForbidden error whose Code is
// "phi-agreement-required".
//
// Label only made-up data. The label says a record is not about a real person, and it is never a
// way to send patient data without an agreement.
//
//	marked, err := haau3.MarkAsTestData(records)
//	if err != nil {
//		return err
//	}
//	list, err := client.ReconcileMedications(ctx, marked)
//
// The copy is made through JSON, so the error is a KindNotSent *Error only for a body that cannot
// be written as JSON, which the call itself could not send either.
func MarkAsTestData[T fhir.Resource](body T) (T, error) {
	var zero T
	if v := reflect.ValueOf(body); !v.IsValid() || (v.Kind() == reflect.Pointer && v.IsNil()) {
		return body, nil // nothing to label, and a call refuses a missing body itself
	}
	data, err := jsonBytes(body)
	if err != nil {
		return zero, notSent("MarkAsTestData did not mark the request: its body cannot be written as JSON.")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber() // a number goes back as it was written, never through a float
	var tree any
	if err := dec.Decode(&tree); err != nil {
		return zero, notSent("MarkAsTestData did not mark the request: its body cannot be read back as JSON.")
	}
	label(tree)
	labelled, err := jsonBytes(tree)
	if err != nil {
		return zero, notSent("MarkAsTestData did not mark the request: its body cannot be written as JSON.")
	}
	out, err := decodeAsType[T](labelled)
	if err != nil {
		return zero, notSent("MarkAsTestData did not mark the request: the labelled body does not read as its type.")
	}
	return out, nil
}

// label puts the test data label on a request and on every resource the API reads in it, with a
// stack rather than recursion, as the API's own check walks it.
func label(tree any) {
	stack := []any{tree}
	for len(stack) > 0 {
		node, ok := stack[len(stack)-1].(map[string]any)
		stack = stack[:len(stack)-1]
		if !ok {
			continue
		}
		labelOne(node)
		switch node["resourceType"] {
		case "Bundle":
			entries, _ := node["entry"].([]any)
			for _, entry := range entries {
				if e, ok := entry.(map[string]any); ok {
					stack = append(stack, e["resource"])
				}
			}
		case "Parameters":
			parameters, _ := node["parameter"].([]any)
			parameters = append([]any(nil), parameters...)
			for len(parameters) > 0 {
				p, ok := parameters[len(parameters)-1].(map[string]any)
				parameters = parameters[:len(parameters)-1]
				if !ok {
					continue
				}
				stack = append(stack, p["resource"])
				if parts, ok := p["part"].([]any); ok {
					parameters = append(parameters, parts...)
				}
			}
		}
	}
}

func labelOne(resource map[string]any) {
	meta, ok := resource["meta"].(map[string]any)
	if !ok {
		meta = map[string]any{}
		resource["meta"] = meta
	}
	security, _ := meta["security"].([]any)
	for _, coding := range security {
		if c, ok := coding.(map[string]any); ok && c["system"] == testDataLabel["system"] && c["code"] == testDataLabel["code"] {
			return
		}
	}
	coding := make(map[string]any, len(testDataLabel))
	for k, v := range testDataLabel {
		coding[k] = v
	}
	meta["security"] = append(security, coding)
}

// decodeAsType reads JSON as a T: a pointer to a resource's struct, the struct itself, or the
// fhir.Resource interface, which reads as the type its resourceType names.
func decodeAsType[T fhir.Resource](data []byte) (T, error) {
	var zero T
	t := reflect.TypeFor[T]()
	switch t.Kind() {
	case reflect.Pointer:
		v := reflect.New(t.Elem())
		if err := json.Unmarshal(data, v.Interface()); err != nil {
			return zero, err
		}
		return v.Interface().(T), nil
	case reflect.Interface:
		r, err := fhir.DecodeResource(data)
		if err != nil {
			return zero, err
		}
		out, ok := r.(T)
		if !ok {
			return zero, &json.UnmarshalTypeError{Value: "resource", Type: t}
		}
		return out, nil
	default:
		var v T
		if err := json.Unmarshal(data, &v); err != nil {
			return zero, err
		}
		return v, nil
	}
}
