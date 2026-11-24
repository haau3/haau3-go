package main

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/haau3/haau3-go/internal/includes"
)

// The query parameters a search helper sends itself, so its parameters struct has no field for
// either.
const (
	includeWire        = "_include"
	includeIterateWire = "_include:iterate"
)

// hasHelper reports whether a search has a helper that sends every include it serves
// (SearchSlotWithIncludes, say), written by hand beside the views.
func hasHelper(o operation) bool {
	_, ok := includes.ByOperation[o.ID]
	return ok
}

// helperParamsType is the helper's parameters struct: SearchSlotWithIncludesParams.
func (o operation) helperParamsType() string { return o.Name + "WithIncludesParams" }

// checkHelpers refuses a spec in which a search a helper makes is gone, or takes no includes as a
// list, since the helper and its parameters would then not compile, or not send what they say.
func checkHelpers(s *spec) error {
	ids := make([]string, 0, len(includes.ByOperation))
	for id := range includes.ByOperation {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		i := slices.IndexFunc(s.Operations, func(o operation) bool { return o.ID == id })
		if i < 0 {
			return fmt.Errorf("%s has a helper that sends its includes, and the spec no longer has it", id)
		}
		for _, wire := range []string{includeWire, includeIterateWire} {
			j := slices.IndexFunc(s.Operations[i].Query, func(p param) bool { return p.Wire == wire })
			if j < 0 || s.Operations[i].Query[j].Kind != "strings" || s.Operations[i].Query[j].Required {
				return fmt.Errorf("%s's helper sends %s, which the spec no longer takes as an optional list", id, wire)
			}
		}
	}
	return nil
}

// emitFields writes a parameters struct's fields, each with the API's own name and description.
func emitFields(b *strings.Builder, params []param) {
	for i, p := range params {
		if i > 0 {
			b.WriteString("\n")
		}
		doc := fmt.Sprintf("%s is `%s`", p.Name, p.Wire)
		if p.Required {
			doc += ", required"
		}
		doc += "."
		if p.Description != "" {
			doc += " " + p.Description
		}
		if len(p.Enum) > 0 {
			doc += " One of: " + strings.Join(p.Enum, ", ") + "."
		}
		comment(b, "\t", doc)
		fmt.Fprintf(b, "\t%s %s\n", p.Name, fieldType(p.Kind, p.Required))
	}
}

// emitHelperParams writes a search helper's parameters struct, the search's own less its includes,
// so a value that may hold an include does not compile, and the conversion back to the search's.
func emitHelperParams(b *strings.Builder, o operation) {
	var fields []param
	for _, p := range o.Query {
		if p.Wire != includeWire && p.Wire != includeIterateWire {
			fields = append(fields, p)
		}
	}
	helper := o.Name + "WithIncludes"
	comment(b, "", fmt.Sprintf("%s holds %s's query parameters: %s's, less `%s` and `%s`, which %s sends itself, "+
		"so a value that holds an include does not compile. A nil field is not sent.",
		o.helperParamsType(), helper, o.Name, includeWire, includeIterateWire, helper))
	fmt.Fprintf(b, "type %s struct {\n", o.helperParamsType())
	emitFields(b, fields)
	b.WriteString("}\n\n")
	fmt.Fprintf(b, "// search is these parameters as %s's, with the includes given.\nfunc (p %s) search(with includes.Includes) %s {\n\treturn %s{\n",
		o.Name, o.helperParamsType(), o.ParamsType(), o.ParamsType())
	for _, p := range o.Query {
		switch p.Wire {
		case includeWire:
			fmt.Fprintf(b, "\t\t%s: slices.Clone(with.Include),\n", p.Name)
		case includeIterateWire:
			fmt.Fprintf(b, "\t\t%s: slices.Clone(with.Iterate),\n", p.Name)
		default:
			fmt.Fprintf(b, "\t\t%s: p.%s,\n", p.Name, p.Name)
		}
	}
	b.WriteString("\t}\n}\n\n")
}
