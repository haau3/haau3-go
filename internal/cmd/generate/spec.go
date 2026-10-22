package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// batchableField is the extension the spec sets to false on each operation the API refuses inside a
// batch by name. Every other operation under the FHIR base can go in a batch; the batch itself sits
// at the base, not under it. The spec defines the field only as false, and only on the FHIR base, so
// anything else stops the generator rather than being guessed at.
const batchableField = "x-haau3-batchable"

const fhirBase = "/v1/fhir/r4"

type object = map[string]any

// param is one query parameter of an operation.
type param struct {
	Wire        string // the API's own name: `name:contains`
	Name        string // the Go field: `NameContains`
	Required    bool
	Kind        string // "string", "int", "bool", "strings": what the Go field holds
	Description string
	Enum        []string
	Example     any // the spec's example value, when it has one
}

// operation is one operation of the spec, as the generator writes it.
type operation struct {
	ID          string // operationId: `readInsurancePlan`
	Name        string // the Go method: `ReadInsurancePlan`
	Method      string // GET or POST
	Path        string // `/v1/fhir/r4/InsurancePlan/{id}`
	Tag         string
	Summary     string
	Description string
	Docs        string
	PathParams  []string
	PathExample []string
	Query       []param
	Body        string   // the Go type of the body: `*fhir.Parameters`, `fhir.Resource`, or ""
	BodyTypes   []string // the resource types a body may be
	Answer      string   // the Go type of the answer: `*fhir.InsurancePlan`, `*SearchSlotsResponse`, `fhir.Resource`
	Expects     []string // the FHIR resource types a 200 may be; empty for Scheduling's answers
	Batchable   bool
	Refused     bool            // the spec marks it x-haau3-batchable: false
	Example     json.RawMessage // the spec's example answer
	BodyExample json.RawMessage // the spec's example request body
	BodyLabel   string          // the label the spec gives that example
}

func (o operation) ParamsType() string { return o.Name + "Params" }
func (o operation) Label() string      { return o.Method + " " + o.Path }

type spec struct {
	doc        object
	order      keyOrder
	Version    string
	schemas    map[string]object
	fhirTypes  []string // the components that are FHIR resources (a `resourceType` const)
	ownTypes   []string // every other component
	Operations []operation
	Tags       []tagInfo
}

type tagInfo struct{ Name, Description string }

func parseSpec(data []byte) (*spec, error) {
	var doc object
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	order, err := readKeyOrder(data)
	if err != nil {
		return nil, err
	}
	s := &spec{doc: doc, order: order, schemas: map[string]object{}}
	if v, _ := doc["openapi"].(string); !strings.HasPrefix(v, "3.1") {
		return nil, fmt.Errorf("the spec is OpenAPI %q; the generator reads 3.1", v)
	}
	s.Version, _ = at(doc, "info", "version").(string)
	for name, schema := range at(doc, "components", "schemas").(object) {
		s.schemas[name] = schema.(object)
	}
	for name, schema := range s.schemas {
		if t := fhirType(schema); t != "" {
			if t != name {
				return nil, fmt.Errorf("the FHIR component %s is named for another resource type, %s", name, t)
			}
			s.fhirTypes = append(s.fhirTypes, name)
		} else {
			s.ownTypes = append(s.ownTypes, name)
		}
	}
	slices.Sort(s.fhirTypes)
	slices.Sort(s.ownTypes)
	for _, t := range asList(doc["tags"]) {
		tag := t.(object)
		desc, _ := tag["description"].(string)
		s.Tags = append(s.Tags, tagInfo{tag["name"].(string), desc})
	}
	if err := s.readOperations(); err != nil {
		return nil, err
	}
	return s, nil
}

func at(v any, keys ...string) any {
	for _, k := range keys {
		m, ok := v.(object)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

func fhirType(schema object) string {
	t, _ := at(schema, "properties", "resourceType", "const").(string)
	return t
}

func refName(ref string) string { return ref[strings.LastIndex(ref, "/")+1:] }

// componentType is the Go type of a component as an answer or a body.
func (s *spec) componentType(ref string) (string, error) {
	name := refName(ref)
	if _, ok := s.schemas[name]; !ok {
		return "", fmt.Errorf("%s is not a component", ref)
	}
	if slices.Contains(s.fhirTypes, name) {
		return "*fhir." + name, nil
	}
	return "*" + name, nil
}

func onlyContent(v any, what string) (object, string, error) {
	content, _ := v.(object)
	if len(content) != 1 {
		return nil, "", fmt.Errorf("%s has %d media types", what, len(content))
	}
	for mediaType, media := range content {
		return media.(object), mediaType, nil
	}
	return nil, "", nil
}

// firstExample is a media type's example: its `example`, or the first of its `examples` in the
// spec's own order, which is the one the reference SDKs read. `at` is the media type's pointer.
func (s *spec) firstExample(media object, at pointer) (json.RawMessage, error) {
	if v, ok := media["example"]; ok {
		return json.Marshal(v)
	}
	examples, _ := media["examples"].(object)
	keys := s.order.keys(at.to("examples"), examples)
	if len(keys) == 0 {
		return nil, nil
	}
	return json.Marshal(examples[keys[0]].(object)["value"])
}

// firstExampleLabel is the summary of a media type's first example, in the spec's own order.
func (s *spec) firstExampleLabel(media object, at pointer) string {
	examples, _ := media["examples"].(object)
	keys := s.order.keys(at.to("examples"), examples)
	if len(keys) == 0 {
		return ""
	}
	label, _ := examples[keys[0]].(object)["summary"].(string)
	return label
}

// notInABatch is the batch itself, then every operation the spec marks x-haau3-batchable: false.
func (s *spec) notInABatch() []string {
	var ids []string
	for _, o := range s.Operations {
		if o.Path == fhirBase && o.Method == "POST" {
			ids = append([]string{o.ID}, ids...)
		} else if o.Refused {
			ids = append(ids, o.ID)
		}
	}
	return ids
}

func (s *spec) readOperations() error {
	docs := docsURLs{}
	paths := at(s.doc, "paths").(object)
	for _, path := range s.order.keys(pointer{"paths"}, paths) {
		item := paths[path].(object)
		for _, method := range []string{"get", "post"} {
			raw, ok := item[method]
			if !ok {
				continue
			}
			op, err := s.readOperation(path, strings.ToUpper(method), raw.(object), pointer{"paths", path, method})
			if err != nil {
				return err
			}
			for _, other := range []string{"put", "patch", "delete"} {
				if _, ok := item[other]; ok {
					return fmt.Errorf("%s has a %s; the SDK sends GET and POST only", path, other)
				}
			}
			s.Operations = append(s.Operations, op)
		}
	}
	// docs.haau3.com numbers repeated pages in the spec's own order, which is the order read here.
	for i := range s.Operations {
		op := &s.Operations[i]
		summary := cmp.Or(op.Summary, op.ID)
		op.Docs = docs.url(op.Tag, summary)
	}
	if !slices.ContainsFunc(s.Operations, func(o operation) bool { return o.Path == fhirBase && o.Method == "POST" }) {
		return fmt.Errorf("the spec has no batch at POST %s", fhirBase)
	}
	names := map[string]string{}
	for i := range s.Operations {
		op := &s.Operations[i]
		op.Batchable = strings.HasPrefix(op.Path, fhirBase+"/") && !op.Refused
		if other, ok := names[op.Name]; ok {
			return fmt.Errorf("%s and %s are both %s in Go", other, op.ID, op.Name)
		}
		names[op.Name] = op.ID
	}
	slices.SortFunc(s.Operations, func(a, b operation) int { return cmp.Compare(a.ID, b.ID) })
	return nil
}

func (s *spec) readOperation(path, method string, op object, here pointer) (operation, error) {
	id, _ := op["operationId"].(string)
	o := operation{ID: id, Name: goName(id), Method: method, Path: path}
	o.Summary, _ = op["summary"].(string)
	o.Description, _ = op["description"].(string)
	o.Tag = "Other"
	if tags := asList(op["tags"]); len(tags) > 0 {
		o.Tag = tags[0].(string)
	}
	if mark, ok := op[batchableField]; ok {
		if mark != false {
			return o, fmt.Errorf("%s carries %s: %v, where the spec defines only false", id, batchableField, mark)
		}
		if !strings.HasPrefix(path, fhirBase+"/") {
			return o, fmt.Errorf("%s is marked %s: false, but is not on the FHIR base", id, batchableField)
		}
		o.Refused = true
	}
	fields := map[string]string{}
	for _, raw := range asList(op["parameters"]) {
		p := raw.(object)
		if _, isRef := p["$ref"]; isRef {
			continue // the X-Request-Id header, which WithCorrelationID sends
		}
		name, _ := p["name"].(string)
		switch p["in"] {
		case "path":
			o.PathParams = append(o.PathParams, name)
			o.PathExample = append(o.PathExample, fmt.Sprint(p["example"]))
		case "query":
			q, err := s.readParam(id, p)
			if err != nil {
				return o, err
			}
			if other, ok := fields[q.Name]; ok {
				return o, fmt.Errorf("%s: the query parameters %s and %s are both %s", id, other, q.Wire, q.Name)
			}
			fields[q.Name] = q.Wire
			o.Query = append(o.Query, q)
		case "header":
			// An optional header is sent with WithHeader until the generator offers it as an option of
			// its own. A required one would make a call the generated method cannot complete, so it
			// stops the generator, as any parameter it does not read does.
			if required, _ := p["required"].(bool); required {
				return o, fmt.Errorf("%s: the required header %s, which the generator does not send", id, name)
			}
		default:
			return o, fmt.Errorf("%s: a parameter in %v, which the generator does not read", id, p["in"])
		}
	}
	answer, mediaType, err := onlyContent(at(op, "responses", "200", "content"), id+"'s 200")
	if err != nil {
		return o, err
	}
	if o.Example, err = s.firstExample(answer, here.to("responses", "200", "content", mediaType)); err != nil {
		return o, err
	}
	schema := answer["schema"].(object)
	o.Expects, err = s.expects(schema)
	if err != nil {
		return o, fmt.Errorf("%s: %w", id, err)
	}
	switch {
	case schema["$ref"] != nil:
		if o.Answer, err = s.componentType(schema["$ref"].(string)); err != nil {
			return o, err
		}
	case len(o.Expects) > 1:
		o.Answer = "fhir.Resource"
	default:
		return o, fmt.Errorf("%s's 200 has a schema the generator does not read", id)
	}
	if body, ok := op["requestBody"].(object); ok {
		media, mediaType, err := onlyContent(body["content"], id+"'s body")
		if err != nil {
			return o, err
		}
		if o.BodyExample, err = s.firstExample(media, here.to("requestBody", "content", mediaType)); err != nil {
			return o, err
		}
		o.BodyLabel = s.firstExampleLabel(media, here.to("requestBody", "content", mediaType))
		schema := media["schema"].(object)
		if ref, ok := schema["$ref"].(string); ok {
			if o.Body, err = s.componentType(ref); err != nil {
				return o, err
			}
			o.BodyTypes = []string{fhirType(s.schemas[refName(ref)])}
		} else {
			// MedList takes a Bundle or a Parameters, each declared inline with its resourceType.
			for _, branch := range asList(schema["oneOf"]) {
				t := fhirType(branch.(object))
				if t == "" || !slices.Contains(s.fhirTypes, t) {
					return o, fmt.Errorf("%s's body has a schema the generator does not read", id)
				}
				o.BodyTypes = append(o.BodyTypes, t)
			}
			if len(o.BodyTypes) == 0 {
				return o, fmt.Errorf("%s's body has a schema the generator does not read", id)
			}
			o.Body = "fhir.Resource"
		}
		if !strings.HasPrefix(o.Body, "*fhir.") && o.Body != "fhir.Resource" {
			return o, fmt.Errorf("%s takes a body that is not a FHIR resource", id)
		}
	}
	return o, nil
}

// expects is the FHIR resource types a 200 answers with; empty for any other answer, which is not checked.
func (s *spec) expects(schema object) ([]string, error) {
	refs := []any{schema}
	if schema["$ref"] == nil {
		refs = asList(schema["oneOf"])
		if refs == nil {
			refs = asList(schema["anyOf"])
		}
	}
	var types []string
	for _, ref := range refs {
		r, _ := ref.(object)["$ref"].(string)
		t := fhirType(s.schemas[refName(r)])
		if t == "" {
			if len(refs) > 1 {
				return nil, fmt.Errorf("an answer that is one of several, not all FHIR resources")
			}
			return nil, nil
		}
		types = append(types, t)
	}
	slices.Sort(types)
	return types, nil
}

func (s *spec) readParam(op string, p object) (param, error) {
	wire, _ := p["name"].(string)
	name, err := paramName(wire)
	if err != nil {
		return param{}, fmt.Errorf("%s: %w", op, err)
	}
	q := param{Wire: wire, Name: name, Example: p["example"]}
	q.Required, _ = p["required"].(bool)
	q.Description, _ = p["description"].(string)
	q.Description = strings.Join(strings.Fields(q.Description), " ")
	schema := p["schema"].(object)
	if d, ok := schema["description"].(string); ok && q.Description == "" {
		q.Description = strings.Join(strings.Fields(d), " ")
	}
	kind, enum, err := queryKind(schema)
	if err != nil {
		return param{}, fmt.Errorf("%s: the query parameter %s: %w", op, wire, err)
	}
	q.Kind, q.Enum = kind, enum
	return q, nil
}

// queryKind is what a query parameter's Go field holds. An array, or a value that may be one or a
// list (`$lookup`'s `property`), is a list: one value sends the parameter once, as a list of one
// does. FHIR's `active`, a string `true` or `false`, is a bool.
func queryKind(schema object) (string, []string, error) {
	if alts := asList(schema["anyOf"]); alts != nil {
		kinds := map[string]bool{}
		for _, alt := range alts {
			k, _, err := queryKind(alt.(object))
			if err != nil {
				return "", nil, err
			}
			kinds[k] = true
		}
		if len(kinds) == 2 && kinds["string"] && kinds["strings"] {
			return "strings", nil, nil
		}
		return "", nil, fmt.Errorf("an anyOf the generator does not read")
	}
	var enum []string
	for _, v := range asList(schema["enum"]) {
		enum = append(enum, fmt.Sprint(v))
	}
	switch schema["type"] {
	case "array":
		item, itemEnum, err := queryKind(schema["items"].(object))
		if err != nil || item != "string" {
			return "", nil, fmt.Errorf("a list of something other than strings")
		}
		return "strings", itemEnum, nil
	case "string":
		sorted := slices.Sorted(slices.Values(enum))
		if slices.Equal(sorted, []string{"false", "true"}) {
			return "bool", nil, nil
		}
		if schema["format"] == "date-time" {
			return "time", nil, nil // a time.Time, sent as RFC 3339 with seconds
		}
		return "string", enum, nil
	case "integer":
		return "int", nil, nil
	case "boolean":
		return "bool", nil, nil
	}
	return "", nil, fmt.Errorf("a schema the generator does not read: %v", schema)
}
