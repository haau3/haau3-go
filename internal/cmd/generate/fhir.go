package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
)

// HL7's FHIR 4.0.1 JSON Schema, https://hl7.org/fhir/R4/fhir.schema.json.zip, measured 2026-10-19:
// the same file, by the same hashes, the Python SDK generates its FHIR types from.
const (
	fhirZipSHA256    = "75e5560da3cf503895a44c8ca7af17a83b4cca6c2cb5ba1883d2aec0d1cb5ac6"
	fhirSchemaSHA256 = "2230406893b4cf002a4ee1e5e2bbeca22ac5d2d4931b3e9ef7b9594bbc376a01"
	refPrefix        = "#/definitions/"
)

type fhirSchema struct {
	defs      map[string]object
	order     keyOrder
	resources []string // every resource type, in HL7's order
}

func readFHIRSchema(zipped []byte) (*fhirSchema, error) {
	if got := sha256Hex(zipped); got != fhirZipSHA256 {
		return nil, fmt.Errorf("hl7/fhir.schema.json.zip has sha256 %s, not the pinned %s", got, fhirZipSHA256)
	}
	archive, err := zip.NewReader(bytes.NewReader(zipped), int64(len(zipped)))
	if err != nil {
		return nil, err
	}
	if len(archive.File) != 1 || archive.File[0].Name != "fhir.schema.json" {
		return nil, fmt.Errorf("hl7/fhir.schema.json.zip should hold fhir.schema.json alone")
	}
	f, err := archive.File[0].Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	if got := sha256Hex(data); got != fhirSchemaSHA256 {
		return nil, fmt.Errorf("fhir.schema.json has sha256 %s, not the pinned %s", got, fhirSchemaSHA256)
	}
	var doc object
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	order, err := readKeyOrder(data)
	if err != nil {
		return nil, err
	}
	s := &fhirSchema{defs: map[string]object{}, order: order}
	for name, def := range doc["definitions"].(object) {
		s.defs[name] = def.(object)
	}
	for _, ref := range asList(s.defs["ResourceList"]["oneOf"]) {
		s.resources = append(s.resources, strings.TrimPrefix(ref.(object)["$ref"].(string), refPrefix))
	}
	return s, nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

var intPrimitives = []string{"integer", "positiveInt", "unsignedInt"}

func (s *fhirSchema) isPrimitive(name string) bool {
	_, has := s.defs[name]["properties"]
	return !has && name != "ResourceList"
}

func (s *fhirSchema) intPatterns() []string {
	var patterns []string
	for _, name := range intPrimitives {
		patterns = append(patterns, s.defs[name]["pattern"].(string))
	}
	return patterns
}

// primitive is a primitive type's Go type: FHIR's decimal keeps its digits as a json.Number.
func primitive(name string) string {
	switch {
	case name == "boolean":
		return "bool"
	case slices.Contains(intPrimitives, name):
		return "int"
	case name == "decimal":
		return "json.Number"
	}
	return "string"
}

// field is one element of a FHIR type, as a Go struct field.
type field struct {
	JSON, Go, Type, Doc string
	resource            bool // holds any resource: `fhir.Resource`, or a list of them
	list                bool
}

func (s *fhirSchema) field(owner, key string, prop object) (field, error) {
	f := field{JSON: key}
	base := strings.TrimPrefix(key, "_")
	f.Go = goName(base)
	if strings.HasPrefix(key, "_") {
		f.Go += "Ext"
		f.Doc = fmt.Sprintf("%s holds the id and extensions of %s.", f.Go, goName(base))
	} else if d, ok := prop["description"].(string); ok {
		f.Doc = firstSentence(d)
	}
	item := prop
	if prop["type"] == "array" {
		f.list = true
		item = prop["items"].(object)
	}
	var t string
	switch {
	case item["$ref"] != nil:
		target := strings.TrimPrefix(item["$ref"].(string), refPrefix)
		switch {
		case target == "ResourceList":
			t, f.resource = "Resource", true
		case s.isPrimitive(target):
			t = primitive(target)
		default:
			t = typeName(target)
		}
	case item["enum"] != nil, item["type"] == "string":
		t = "string"
	case item["type"] == "boolean":
		t = "bool"
	case item["type"] == "number":
		t = "json.Number"
		if pattern, _ := item["pattern"].(string); slices.Contains(s.intPatterns(), pattern) {
			t = "int"
		}
	default:
		return f, fmt.Errorf("%s.%s has a schema the generator does not read", owner, key)
	}
	switch {
	case f.list && strings.HasPrefix(key, "_"):
		f.Type = "[]*" + t // a list's extensions line up with its values, with null for a value with none
	case f.list:
		f.Type = "[]" + t
	case f.resource:
		f.Type = t
	default:
		f.Type = "*" + t
	}
	return f, nil
}

var sentence = regexp.MustCompile(`^(.+?[.!?])(\s|$)`)

func firstSentence(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if m := sentence.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	return text
}

// elementPath is an HL7 definition's place in FHIR: `Bundle_Entry` is `Bundle.entry`.
func elementPath(def string) string {
	parts := strings.Split(def, "_")
	for i := 1; i < len(parts); i++ {
		parts[i] = strings.ToLower(parts[i][:1]) + parts[i][1:]
	}
	return strings.Join(parts, ".")
}

// emitFHIR writes package fhir: every type of FHIR R4.
func emitFHIR(s *fhirSchema) (string, error) {
	var names []string
	for name := range s.defs {
		if !s.isPrimitive(name) && name != "ResourceList" {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	seen := map[string]string{}
	for _, name := range names {
		t := typeName(name)
		if other, ok := seen[t]; ok {
			return "", fmt.Errorf("%s and %s are both %s in Go", other, name, t)
		}
		if t == "Resource" || t == "OtherResource" {
			return "", fmt.Errorf("an HL7 definition is named %s, which package fhir uses itself", t)
		}
		seen[t] = name
	}

	var b strings.Builder
	b.WriteString(`// Code generated by internal/cmd/generate from HL7's fhir.schema.json (FHIR 4.0.1). DO NOT EDIT.

package fhir

import "encoding/json"

`)
	for _, name := range names {
		if err := s.emitType(&b, name); err != nil {
			return "", err
		}
	}
	b.WriteString("// newResource makes an empty resource of each type, by its resourceType.\n")
	b.WriteString("var newResource = map[string]func() Resource{\n")
	for _, r := range s.resources {
		fmt.Fprintf(&b, "\t%q: func() Resource { return new(%s) },\n", r, typeName(r))
	}
	b.WriteString("}\n")
	return b.String(), nil
}

func (s *fhirSchema) emitType(b *strings.Builder, name string) error {
	def := s.defs[name]
	t := typeName(name)
	isResource := slices.Contains(s.resources, name)
	about := firstSentence(fmt.Sprint(def["description"]))
	switch {
	case isResource:
		fmt.Fprintf(b, "// %s is the FHIR R4 %s resource. %s\n", t, name, about)
	case strings.Contains(name, "_"):
		// HL7's schema gives a backbone element its resource's description, so only its place is said.
		fmt.Fprintf(b, "// %s is %s in FHIR R4.\n", t, elementPath(name))
	default:
		fmt.Fprintf(b, "// %s is the FHIR R4 %s datatype. %s\n", t, name, about)
	}
	props := def["properties"].(object)
	var fields []field
	goNames := map[string]string{}
	for _, key := range s.order.keys(pointer{"definitions", name, "properties"}, props) {
		prop := props[key].(object)
		if prop["const"] != nil {
			continue // resourceType: written by MarshalJSON, read by ResourceType()
		}
		f, err := s.field(name, key, prop)
		if err != nil {
			return err
		}
		if other, ok := goNames[f.Go]; ok {
			return fmt.Errorf("%s.%s and %s.%s are both %s in Go", name, other, name, key, f.Go)
		}
		goNames[f.Go] = key
		fields = append(fields, f)
	}
	fmt.Fprintf(b, "type %s struct {\n", t)
	for _, f := range fields {
		if f.Doc != "" {
			fmt.Fprintf(b, "\t// %s\n", f.Doc)
		}
		fmt.Fprintf(b, "\t%s %s `json:\"%s,omitempty\"`\n", f.Go, f.Type, f.JSON)
	}
	b.WriteString("}\n\n")

	if isResource {
		fmt.Fprintf(b, "// ResourceType is %q.\nfunc (%s) ResourceType() string { return %q }\n\n", name, t, name)
		fmt.Fprintf(b, "// MarshalJSON writes the resource with its resourceType first.\n")
		fmt.Fprintf(b, "func (x %s) MarshalJSON() ([]byte, error) {\n\ttype plain %s\n\treturn marshalResource(%q, plain(x))\n}\n\n", t, t, name)
	}
	var holders []field
	for _, f := range fields {
		if f.resource {
			holders = append(holders, f)
		}
	}
	if len(holders) == 0 {
		return nil
	}
	// A place that holds any resource is read by its resourceType, into the type it names.
	fmt.Fprintf(b, "// UnmarshalJSON reads each resource %s holds as the type its resourceType names.\n", t)
	fmt.Fprintf(b, "func (x *%s) UnmarshalJSON(data []byte) error {\n\ttype plain %s\n\tvar raw struct {\n\t\t*plain\n", t, t)
	for _, f := range holders {
		if f.list {
			fmt.Fprintf(b, "\t\t%s []json.RawMessage `json:\"%s,omitempty\"`\n", f.Go, f.JSON)
		} else {
			fmt.Fprintf(b, "\t\t%s json.RawMessage `json:\"%s,omitempty\"`\n", f.Go, f.JSON)
		}
	}
	b.WriteString("\t}\n\traw.plain = (*plain)(x)\n\tif err := json.Unmarshal(data, &raw); err != nil {\n\t\treturn err\n\t}\n\tvar err error\n")
	for _, f := range holders {
		if f.list {
			fmt.Fprintf(b, "\tif x.%s, err = decodeResources(raw.%s, %q); err != nil {\n\t\treturn err\n\t}\n", f.Go, f.Go, f.JSON)
		} else {
			fmt.Fprintf(b, "\tif x.%s, err = decodeAt(raw.%s, %q); err != nil {\n\t\treturn err\n\t}\n", f.Go, f.Go, f.JSON)
		}
	}
	b.WriteString("\treturn nil\n}\n\n")
	return nil
}
