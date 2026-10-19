// Package fhir holds FHIR R4 (4.0.1) as Go types: every resource and datatype, generated from
// HL7's own fhir.schema.json.
//
// Each element is optional, as FHIR's JSON makes it: a single value is a pointer, nil when absent,
// and a list is a slice. A decimal keeps the digits it was written with, as a json.Number. A place
// that holds any resource, such as Bundle.entry.resource or a resource's contained list, holds a
// Resource, read as the type its resourceType names:
//
//	for _, entry := range bundle.Entry {
//		switch r := entry.Resource.(type) {
//		case *fhir.InsurancePlan:
//			fmt.Println(*r.ID)
//		case *fhir.OperationOutcome:
//			// a search's warnings
//		}
//	}
//
// Each type writes its resourceType itself, so a resource you build marshals as FHIR JSON:
//
//	statement := &fhir.MedicationStatement{Status: new("active")}
//
// These types say what FHIR R4 defines; they do not validate a resource against it. Reading JSON
// into them checks each element's JSON type (a list where a list belongs), and an element FHIR R4
// does not define is not kept.
package fhir

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Resource is any FHIR resource: one of this package's resource types, or an OtherResource.
type Resource interface {
	// ResourceType is the resource's type, as its JSON's resourceType says it: "InsurancePlan".
	ResourceType() string
}

// OtherResource is a resource of a type FHIR R4 does not define, such as one from a later FHIR
// version, kept as the JSON it was. It marshals back to that JSON.
type OtherResource struct {
	// Type is its resourceType, or "" when it had none.
	Type string
	// JSON is the resource as it was read.
	JSON json.RawMessage
}

// ResourceType is the resource's resourceType.
func (r *OtherResource) ResourceType() string { return r.Type }

// MarshalJSON writes the resource as it was read.
func (r *OtherResource) MarshalJSON() ([]byte, error) {
	if len(r.JSON) == 0 {
		return []byte("null"), nil
	}
	return r.JSON, nil
}

// DecodeResource reads one resource as the type its resourceType names, or as an OtherResource
// for a type FHIR R4 does not define. JSON null is a nil Resource.
func DecodeResource(data []byte) (Resource, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	var probe struct {
		ResourceType *string `json:"resourceType"`
	}
	if err := json.Unmarshal(trimmed, &probe); err != nil {
		return nil, err
	}
	if probe.ResourceType != nil {
		if newOf, ok := newResource[*probe.ResourceType]; ok {
			r := newOf()
			if err := json.Unmarshal(trimmed, r); err != nil {
				return nil, err
			}
			return r, nil
		}
	}
	other := &OtherResource{JSON: append(json.RawMessage(nil), trimmed...)}
	if probe.ResourceType != nil {
		other.Type = *probe.ResourceType
	}
	return other, nil
}

// decodeAt reads the resource held at one element, naming the element when it cannot.
func decodeAt(data json.RawMessage, element string) (Resource, error) {
	r, err := DecodeResource(data)
	if err != nil {
		return nil, &ElementError{Element: element, Err: err}
	}
	return r, nil
}

func decodeResources(data []json.RawMessage, element string) ([]Resource, error) {
	if data == nil {
		return nil, nil
	}
	out := make([]Resource, len(data))
	for i, d := range data {
		r, err := DecodeResource(d)
		if err != nil {
			return nil, &ElementError{Element: fmt.Sprintf("%s[%d]", element, i), Err: err}
		}
		out[i] = r
	}
	return out, nil
}

// ElementError is a resource inside another that could not be read: Element says where, and Err
// says why. Neither holds a value from the JSON.
type ElementError struct {
	Element string
	Err     error
}

func (e *ElementError) Error() string { return "fhir: reading " + e.Element + ": " + e.Err.Error() }
func (e *ElementError) Unwrap() error { return e.Err }

// marshalResource writes a resource's elements with its resourceType first, as FHIR JSON puts it.
// HTML characters stay as they are, as FHIR's narrative holds XHTML.
func marshalResource(resourceType string, v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	body := bytes.TrimSpace(buf.Bytes())
	head := `{"resourceType":` + fmt.Sprintf("%q", resourceType)
	if len(body) <= 2 {
		return []byte(head + "}"), nil
	}
	return append([]byte(head+","), body[1:]...), nil
}
