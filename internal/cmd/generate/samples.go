package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// bodyFile is where a sample reads a body that nests resources: a Bundle's entries or a Parameters
// resource's parts. Such a body is too long to show in a sample, and the samples do not write a
// resource inside another; read from a file, it is decoded as what it is.
const bodyFile = "request.json"

// nestsResources is whether an example body holds a resource inside it, below its own level.
func nestsResources(example json.RawMessage) bool {
	var v any
	if json.Unmarshal(example, &v) != nil {
		return false
	}
	var walk func(v any, depth int) bool
	walk = func(v any, depth int) bool {
		switch x := v.(type) {
		case []any:
			for _, item := range x {
				if walk(item, depth+1) {
					return true
				}
			}
		case map[string]any:
			if _, ok := x["resourceType"].(string); ok && depth > 0 {
				return true
			}
			for _, item := range x {
				if walk(item, depth+1) {
					return true
				}
			}
		}
		return false
	}
	return walk(v, 0)
}

// asPhrase is an example's label as it reads after "holds": "A pharmacy ..." and "One patient's ..."
// read on in lower case, and an acronym such as "FHIR" stays as it is.
func asPhrase(label string) string {
	if len(label) > 1 && unicode.IsUpper(rune(label[0])) && !unicode.IsUpper(rune(label[1])) {
		return strings.ToLower(label[:1]) + label[1:]
	}
	return label
}

// named is `a`, `b` and `c`.
func named(quoted []string) string {
	if len(quoted) < 2 {
		return strings.Join(quoted, "")
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + " and " + quoted[len(quoted)-1]
}

// lineWidth is where a composite literal is opened over several lines.
const lineWidth = 100

// emitSamples writes openapi/code-samples.json: an OpenAPI Overlay that adds a Go sample to every
// operation as `x-codeSamples`, for a documentation site to show beside it. Each sample is a whole
// program that sends the operation's example request from the spec and prints one field of the
// answer. samples_test.go builds and runs every one, as it is written, against a stand-in API.
func emitSamples(s *spec, hl7 *fhirSchema) ([]byte, error) {
	type sample struct {
		Lang   string `json:"lang"`
		Label  string `json:"label"`
		Source string `json:"source"`
	}
	type action struct {
		Target string `json:"target"`
		Update struct {
			Samples []sample `json:"x-codeSamples"`
		} `json:"update"`
	}
	var actions []action
	paths := s.doc["paths"].(object)
	for _, path := range s.order.keys(pointer{"paths"}, paths) {
		for _, method := range []string{"get", "post"} {
			if paths[path].(object)[method] == nil {
				continue
			}
			i := slices.IndexFunc(s.Operations, func(o operation) bool { return o.Path == path && o.Method == strings.ToUpper(method) })
			if strings.Contains(path, "'") {
				return nil, fmt.Errorf("%s cannot be written as a JSONPath name selector", path)
			}
			source, err := goSample(s, hl7, s.Operations[i])
			if err != nil {
				return nil, fmt.Errorf("%s's sample: %w", s.Operations[i].ID, err)
			}
			a := action{Target: fmt.Sprintf("$.paths['%s'].%s", path, method)}
			a.Update.Samples = []sample{{Lang: "go", Label: "Go", Source: source}}
			actions = append(actions, a)
		}
	}
	overlay := struct {
		Overlay string `json:"overlay"`
		Info    struct {
			Title   string `json:"title"`
			Version string `json:"version"`
		} `json:"info"`
		Extends string   `json:"extends"`
		Actions []action `json:"actions"`
	}{Overlay: "1.2.0", Extends: "https://api.haau3.com/openapi.json", Actions: actions}
	overlay.Info.Title = "Go samples for every operation of the haau3 platform API"
	overlay.Info.Version = s.Version
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(overlay); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// goSample is one operation's sample, as a whole program, formatted.
func goSample(s *spec, hl7 *fhirSchema, o operation) (string, error) {
	imports := []string{"context", "fmt", "log"}
	module := []string{`"github.com/haau3/haau3-go"`}
	var body []string
	switch o.ID {
	case "fhirBatch":
		lines, err := batchSample(s, o)
		if err != nil {
			return "", err
		}
		body = lines
	default:
		name := answerName(o)
		args := []string{"ctx"}
		for _, v := range o.PathExample {
			args = append(args, strconv.Quote(v))
		}
		if o.Body != "" && nestsResources(o.BodyExample) {
			if o.BodyLabel == "" {
				return "", fmt.Errorf("a body read from a file needs its example's summary, to say what the file holds")
			}
			imports = append(imports, "os")
			module = append(module, `"github.com/haau3/haau3-go/fhir"`)
			body = append(body,
				fmt.Sprintf("data, err := os.ReadFile(%q) // %s", bodyFile, asPhrase(o.BodyLabel)),
				"if err != nil {", "\tlog.Fatal(err)", "}",
			)
			switch {
			case o.Body == "fhir.Resource":
				body = append(body, "body, err := fhir.DecodeResource(data)", "if err != nil {", "\tlog.Fatal(err)", "}")
				args = append(args, "body")
			case strings.HasPrefix(o.Body, "*fhir."):
				imports = append(imports, "encoding/json")
				body = append(body,
					"var body fhir."+strings.TrimPrefix(o.Body, "*fhir."),
					"if err := json.Unmarshal(data, &body); err != nil {", "\tlog.Fatal(err)", "}",
				)
				args = append(args, "&body")
			default:
				return "", fmt.Errorf("a body of type %s is not read from a file", o.Body)
			}
		} else if o.Body != "" {
			if !strings.HasPrefix(o.Body, "*fhir.") {
				return "", fmt.Errorf("a body of type %s has no literal", o.Body)
			}
			def := strings.TrimPrefix(o.Body, "*fhir.")
			literal, err := hl7.literal(def, o.BodyExample, "\t")
			if err != nil {
				return "", err
			}
			module = append(module, `"github.com/haau3/haau3-go/fhir"`)
			args = append(args, "&fhir."+def+literal)
		}
		if len(o.Query) > 0 {
			fields, err := paramFields(o)
			if err != nil {
				return "", err
			}
			args = append(args, fmt.Sprintf("haau3.%s{%s}", o.ParamsType(), strings.Join(fields, ", ")))
		}
		body = append(body,
			fmt.Sprintf("%s, err := client.%s(%s)", name, o.Name, strings.Join(args, ", ")),
			"if err != nil {", "\tlog.Fatal(err)", "}",
		)
		lines, err := printLines(s, o, name)
		if err != nil {
			return "", err
		}
		body = append(body, lines...)
	}
	slices.Sort(imports)
	var b strings.Builder
	b.WriteString("package main\n\nimport (\n")
	for _, p := range imports {
		fmt.Fprintf(&b, "\t%q\n", p)
	}
	b.WriteString("\n")
	for _, p := range module {
		fmt.Fprintf(&b, "\t%s\n", p)
	}
	b.WriteString(")\n\nfunc main() {\n\tctx := context.Background()\n")
	b.WriteString("\tclient, err := haau3.NewClient() // reads HAAU3_API_KEY\n\tif err != nil {\n\t\tlog.Fatal(err)\n\t}\n")
	for _, line := range body {
		b.WriteString("\t" + strings.ReplaceAll(line, "\n", "\n\t") + "\n")
	}
	b.WriteString("}\n")
	if o.ID == "fhirBatch" {
		b.WriteString("\n// show prints that one entry of the batch answered, or why it failed.\n")
		b.WriteString("func show[T any](_ T, err error) {\n\tif err != nil {\n\t\tfmt.Println(err)\n\t\treturn\n\t}\n\tfmt.Println(\"ok\")\n}\n")
	}
	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		return "", fmt.Errorf("does not format: %w\n%s", err, b.String())
	}
	return string(formatted), nil
}

// paramFields is the example query as the fields of the operation's params struct.
func paramFields(o operation) ([]string, error) {
	var fields []string
	for _, p := range o.Query {
		if p.Example == nil {
			if p.Required {
				return nil, fmt.Errorf("the required %s has no example", p.Wire)
			}
			continue
		}
		if p.Kind == "time" {
			return nil, fmt.Errorf("%s's example is a time, which the samples do not write yet", p.Wire)
		}
		fields = append(fields, fmt.Sprintf("%s: %s", p.Name, literal(p.Kind, p.Required, p.Example)))
	}
	return fields, nil
}

// answerName is what a sample names its answer: the resource it is, `metadata` for either of two,
// `found` for any other answer.
func answerName(o operation) string {
	switch len(o.Expects) {
	case 0:
		return "found"
	case 1:
		return localName(o.Expects[0])
	}
	return "metadata"
}

// printLines show the answer: a resource's id, a Bundle's entry count, each output parameter's
// name, a search's total, or how many records a list holds. A CapabilityStatement, which a base's
// metadata answers without an id, prints its resource type.
func printLines(s *spec, o operation, name string) ([]string, error) {
	switch {
	case len(o.Expects) > 1, len(o.Expects) == 1 && o.Expects[0] == "CapabilityStatement":
		return []string{fmt.Sprintf("fmt.Println(%s.ResourceType())", name)}, nil
	case len(o.Expects) == 1 && o.Expects[0] == "Bundle":
		return []string{fmt.Sprintf("fmt.Println(len(%s.Entry))", name)}, nil
	case len(o.Expects) == 1 && o.Expects[0] == "Parameters":
		return []string{
			fmt.Sprintf("for _, parameter := range %s.Parameter {", name),
			"\tif parameter.Name != nil {", "\t\tfmt.Println(*parameter.Name)", "\t}", "}",
		}, nil
	case len(o.Expects) == 1:
		return []string{fmt.Sprintf("if %s.ID != nil {", name), fmt.Sprintf("\tfmt.Println(*%s.ID)", name), "}"}, nil
	}
	component := s.schemas[strings.TrimPrefix(o.Answer, "*")]
	properties, _ := component["properties"].(object)
	if _, ok := properties["total"]; ok {
		return []string{fmt.Sprintf("fmt.Println(%s.Total)", name)}, nil
	}
	var lists []string
	for key, schema := range properties {
		if schema.(object)["type"] == "array" {
			lists = append(lists, key)
		}
	}
	if len(lists) != 1 {
		return nil, fmt.Errorf("no one field to print")
	}
	return []string{fmt.Sprintf("fmt.Println(len(%s.%s))", name, goName(lists[0]))}, nil
}

// batchSample is the batch operation's sample: the example Bundle's requests, each made by
// haau3.Calls, sent as one batch with Batch.
func batchSample(s *spec, o operation) ([]string, error) {
	var bundle struct {
		Entry []struct {
			Request struct{ Method, URL string } `json:"request"`
		} `json:"entry"`
	}
	if err := json.Unmarshal(o.BodyExample, &bundle); err != nil {
		return nil, err
	}
	var lines, names []string
	for n, e := range bundle.Entry {
		call, err := batchCall(s, e.Request.Method, e.Request.URL)
		if err != nil {
			return nil, err
		}
		name := fmt.Sprintf("entry%d", n+1)
		names = append(names, name)
		lines = append(lines, fmt.Sprintf("%s := %s", name, call))
	}
	lines = append(lines,
		fmt.Sprintf("if err := client.Batch(ctx, []haau3.Entry{%s}); err != nil {", strings.Join(names, ", ")),
		"\tlog.Fatal(err)", "}",
	)
	for _, name := range names {
		lines = append(lines, fmt.Sprintf("show(%s.Result())", name))
	}
	return lines, nil
}

// batchCall is one entry of the example batch Bundle as the haau3.Calls method that makes it.
func batchCall(s *spec, method, rawURL string) (string, error) {
	path, query, _ := strings.Cut(rawURL, "?")
	segments := strings.Split("/v1/fhir/r4/"+path, "/")
	i := slices.IndexFunc(s.Operations, func(o operation) bool {
		template := strings.Split(o.Path, "/")
		if !o.Batchable || o.Method != method || len(template) != len(segments) {
			return false
		}
		for j, part := range template {
			if !strings.HasPrefix(part, "{") && part != segments[j] {
				return false
			}
		}
		return true
	})
	if i < 0 {
		return "", fmt.Errorf("the example batch's %s %s is no operation a batch takes", method, rawURL)
	}
	o := s.Operations[i]
	var args []string
	for j, part := range strings.Split(o.Path, "/") {
		if strings.HasPrefix(part, "{") {
			args = append(args, strconv.Quote(segments[j]))
		}
	}
	values, err := url.ParseQuery(query)
	if err != nil {
		return "", err
	}
	if len(o.Query) > 0 {
		var fields []string
		for _, p := range o.Query {
			given, ok := values[p.Wire]
			if !ok {
				continue
			}
			var example any = given[0]
			switch p.Kind {
			case "strings":
				list := []any{}
				for _, v := range given {
					list = append(list, v)
				}
				example = list
			case "int":
				n, err := strconv.Atoi(given[0])
				if err != nil {
					return "", err
				}
				example = float64(n)
			}
			fields = append(fields, fmt.Sprintf("%s: %s", p.Name, literal(p.Kind, p.Required, example)))
			delete(values, p.Wire)
		}
		args = append(args, fmt.Sprintf("haau3.%s{%s}", o.ParamsType(), strings.Join(fields, ", ")))
	}
	if len(values) > 0 {
		return "", fmt.Errorf("the example batch sends parameters %s does not declare", o.ID)
	}
	return fmt.Sprintf("haau3.Calls.%s(%s)", o.Name, strings.Join(args, ", ")), nil
}

// literal is a FHIR JSON object as a Go composite literal of an HL7 definition, without its type
// name, on one line when it fits and over several otherwise.
func (s *fhirSchema) literal(def string, raw json.RawMessage, indent string) (string, error) {
	keys, values, err := orderedObject(raw)
	if err != nil {
		return "", err
	}
	properties, _ := s.defs[def]["properties"].(object)
	var parts []string
	for i, key := range keys {
		if key == "resourceType" {
			continue
		}
		prop, ok := properties[key].(object)
		if !ok {
			return "", fmt.Errorf("%s has no element %s", def, key)
		}
		f, err := s.field(def, key, prop)
		if err != nil {
			return "", err
		}
		value, err := s.fieldLiteral(f, prop, values[i], indent+"\t")
		if err != nil {
			return "", err
		}
		parts = append(parts, f.Go+": "+value)
	}
	one := "{" + strings.Join(parts, ", ") + "}"
	if len(indent)*4+len(one) <= lineWidth && !strings.Contains(one, "\n") {
		return one, nil
	}
	return "{\n" + indent + "\t" + strings.Join(parts, ",\n"+indent+"\t") + ",\n" + indent + "}", nil
}

func (s *fhirSchema) fieldLiteral(f field, prop object, raw json.RawMessage, indent string) (string, error) {
	if f.resource {
		return "", fmt.Errorf("%s holds a resource, which the samples do not write", f.JSON)
	}
	item := prop
	if f.list {
		item = prop["items"].(object)
	}
	target := ""
	if ref, ok := item["$ref"].(string); ok {
		target = strings.TrimPrefix(ref, refPrefix)
	}
	complexType := target != "" && !s.isPrimitive(target)
	scalar := func(r json.RawMessage) (string, error) {
		var v any
		if err := json.Unmarshal(r, &v); err != nil {
			return "", err
		}
		switch v := v.(type) {
		case string:
			return strconv.Quote(v), nil
		case bool:
			return strconv.FormatBool(v), nil
		case float64:
			if strings.Contains(f.Type, "json.Number") {
				return fmt.Sprintf("json.Number(%q)", string(r)), nil
			}
			return string(r), nil
		}
		return "", fmt.Errorf("%s has a value the samples do not write: %s", f.JSON, r)
	}
	if f.list {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return "", err
		}
		var values []string
		for _, r := range items {
			var v string
			var err error
			if complexType {
				v, err = s.literal(target, r, indent)
			} else {
				v, err = scalar(r)
			}
			if err != nil {
				return "", err
			}
			values = append(values, v)
		}
		elem := strings.TrimPrefix(f.Type, "[]")
		if complexType {
			elem = "fhir." + elem
			return "[]" + elem + "{\n" + indent + "\t" + strings.Join(values, ",\n"+indent+"\t") + ",\n" + indent + "}", nil
		}
		return "[]" + elem + "{" + strings.Join(values, ", ") + "}", nil
	}
	if complexType {
		v, err := s.literal(target, raw, indent)
		if err != nil {
			return "", err
		}
		return "&fhir." + typeName(target) + v, nil
	}
	v, err := scalar(raw)
	if err != nil {
		return "", err
	}
	return "new(" + v + ")", nil
}

// orderedObject is a JSON object's keys and values, in the order it writes them.
func orderedObject(raw json.RawMessage) ([]string, []json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, nil, fmt.Errorf("not a JSON object: %s", raw)
	}
	var keys []string
	var values []json.RawMessage
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, nil, err
		}
		keys = append(keys, tok.(string))
		values = append(values, v)
	}
	return keys, values, nil
}
