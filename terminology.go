package haau3

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/haau3/haau3-go/fhir"
)

// Typed readers for the answers of the terminology operations, as FHIR's own SDKs give them in Java
// (HAPI's LookupCodeResult and CodeValidationResult, LinuxForHealth's LookupOutcome, Firely's
// ValidateCodeResult). Package functions, with no client, as Parameters and ReviewFlags are. A part a
// reader does not know is kept, verbatim, in the Other list of the level it was found at, so nothing
// a server sent is lost and nothing is guessed at.

// DesignationUse is how a designation is used: a code from a code system, saying what the name is for.
type DesignationUse struct {
	// System is the code system the use's code is from.
	System string
	// Code is the use's code.
	Code string
	// Display is the code's display, or "" when the answer gives none.
	Display string
}

// The uses of a designation the API sends.
var (
	// PreferredForLanguageUse is the use of a name preferred for its language among several, HL7
	// Terminology's preferredForLanguage. Every answer from a code system haau3 holds names the
	// display again with this use.
	PreferredForLanguageUse = DesignationUse{
		System:  "http://terminology.hl7.org/CodeSystem/hl7TermMaintInfra",
		Code:    "preferredForLanguage",
		Display: "Preferred For Language",
	}
	// SynonymUse is the use of a variant name that is also fit for display, the FHIR terminology
	// ecosystem's synonym. ICD-10-CM's short title has this use, where it differs from the display.
	SynonymUse = DesignationUse{
		System:  "http://fhir.org/packages/fhir.tx.support/CodeSystem/designation-usage",
		Code:    "synonym",
		Display: "Synonym",
	}
)

// Designation is one name for a code, as LookupResult reads it.
type Designation struct {
	// Language is the name's language, such as "en", or "" when the answer does not say.
	Language string
	// Use is what the name is for, or nil when the answer does not say. A name with no use is fit for
	// display.
	Use *DesignationUse
	// Value is the name.
	Value string
	// Other is the designation's parts this reader does not read, such as R5's additionalUse, verbatim.
	Other []fhir.ParametersParameter
}

// Property is one property of a code. Type names the FHIR data type of its value, as $lookup allows
// them, and Value holds it:
//
//   - "code", "string" and "dateTime": a string;
//   - "integer": an int;
//   - "decimal": a json.Number, as the FHIR types hold one;
//   - "boolean": a bool;
//   - "Coding": a fhir.Coding.
//
// A type switch on Value reads it:
//
//	switch value := property.Value.(type) {
//	case fhir.Coding:
//		fmt.Println(property.Code, *value.Code)
//	case bool:
//		fmt.Println(property.Code, value)
//	}
type Property struct {
	// Code is the property's code, in the code system's own words, such as "notSelectable" or "TTY".
	Code string
	// Type is the FHIR data type of Value.
	Type string
	// Value is the property's value, of the Go type Type names.
	Value any
	// Description is the property's description, or "" when the answer gives none.
	Description string
	// Subproperties is the property's subproperties, in answer order, or empty when there are none.
	Subproperties []Property
	// Other is the property's parts this reader does not read, verbatim.
	Other []fhir.ParametersParameter
}

// Lookup is what LookupResult reads from a CodeSystemLookup answer. A part the answer does not carry
// is "", or nil for Abstract.
type Lookup struct {
	// Name is the code system's name, such as "ICD-10-CM".
	Name string
	// Code is the code.
	Code string
	// System is the code system.
	System string
	// Version is the code system's version the answer is from.
	Version string
	// Display is the code's display.
	Display string
	// Abstract says whether the code is abstract, not for use, such as an ICD-10-CM header.
	Abstract *bool
	// Warning is what the answer warns of: for a code system haau3 holds, that another language was
	// asked for and the answer is in English.
	Warning string
	// Designations is every designation, in answer order. The package function Designations finds one
	// by use and language.
	Designations []Designation
	// Properties is every property, in answer order.
	Properties []Property
	// Other is the answer's parts this reader does not read, verbatim.
	Other []fhir.ParametersParameter
}

// Validation is what ValidationResult reads from a validate-code answer. A part the answer does not
// carry is "", or nil for Result.
type Validation struct {
	// Result says whether the code is valid, with the display when one was sent.
	Result *bool
	// Display is the code's display, given even when a display sent did not match it.
	Display string
	// Message is what the server says about the answer, in its words.
	Message string
	// Other is the answer's parts this reader does not read, verbatim.
	Other []fhir.ParametersParameter
}

// DesignationFilter is which designations Designations finds. A field left as its zero value
// matches every designation.
type DesignationFilter struct {
	// Use is a use, matched by its System and Code together.
	Use DesignationUse
	// Language is a language range, matched as RFC 4647's basic filtering does: "en" matches "en"
	// and "en-US".
	Language string
}

// LookupResult reads a CodeSystemLookup answer, typed, with no request: the code, its system and
// version, its display, whether it is abstract, every designation and every property. A part this
// reader does not know stays in Other. It returns an *Error of KindNotSent when a part FHIR allows
// once comes twice, since which one is meant is then ambiguous.
//
//	answer, err := client.CodeSystemLookup(ctx, haau3.CodeSystemLookupParams{System: new("http://hl7.org/fhir/sid/icd-10-cm"), Code: "E08.37X1"})
//	if err != nil {
//		return err
//	}
//	lookup, err := haau3.LookupResult(answer)
//	if err != nil {
//		return err
//	}
//	for _, title := range haau3.Designations(lookup, haau3.DesignationFilter{Use: haau3.SynonymUse}) {
//		fmt.Println(title.Value)
//	}
func LookupResult(answer *fhir.Parameters) (Lookup, error) {
	var lookup Lookup
	if answer == nil {
		return lookup, nil
	}
	values, other, err := readParts("The Parameters", answer.Parameter, map[string]partReader{
		"name": textOf, "code": textOf, "system": textOf, "version": textOf, "display": textOf,
		"abstract": booleanOf, "warning": textOf,
	}, func(part fhir.ParametersParameter) (bool, error) {
		switch deref(part.Name) {
		case "designation":
			designation, ok, err := readDesignation(part)
			if ok {
				lookup.Designations = append(lookup.Designations, designation)
			}
			return ok, err
		case "property":
			property, ok, err := readProperty(part, "A property")
			if ok {
				lookup.Properties = append(lookup.Properties, property)
			}
			return ok, err
		}
		return false, nil
	})
	if err != nil {
		return Lookup{}, err
	}
	lookup.Name, _ = values["name"].(string)
	lookup.Code, _ = values["code"].(string)
	lookup.System, _ = values["system"].(string)
	lookup.Version, _ = values["version"].(string)
	lookup.Display, _ = values["display"].(string)
	lookup.Warning, _ = values["warning"].(string)
	if abstract, ok := values["abstract"].(bool); ok {
		lookup.Abstract = &abstract
	}
	lookup.Other = other
	return lookup, nil
}

// ValidationResult reads a validate-code answer, from a code system or a value set, typed, with no
// request: whether the code is valid, its display, and the server's message. A display that did not
// match answers Result false with the code's display beside it. It returns an *Error of KindNotSent
// when a part FHIR allows once comes twice.
//
//	answer, err := client.CodeSystemValidateCode(ctx, haau3.CodeSystemValidateCodeParams{URL: new("http://loinc.org"), Code: "8867-4", Display: new("Pulse")})
//	if err != nil {
//		return err
//	}
//	check, err := haau3.ValidationResult(answer)
//	if err == nil && check.Result != nil && !*check.Result {
//		fmt.Println(check.Message, check.Display)
//	}
func ValidationResult(answer *fhir.Parameters) (Validation, error) {
	var validation Validation
	if answer == nil {
		return validation, nil
	}
	values, other, err := readParts("The Parameters", answer.Parameter, map[string]partReader{
		"result": booleanOf, "display": textOf, "message": textOf,
	}, nil)
	if err != nil {
		return Validation{}, err
	}
	if result, ok := values["result"].(bool); ok {
		validation.Result = &result
	}
	validation.Display, _ = values["display"].(string)
	validation.Message, _ = values["message"].(string)
	validation.Other = other
	return validation, nil
}

// Designations is a lookup's designations with a use and in a language, in answer order, with no
// request. A use matches by its System and Code together. A language matches as RFC 4647's basic
// filtering says, so "en" finds "en" and "en-US". A field left as its zero value matches every
// designation; a designation that names no use, or no language, is found only when that field is
// left out.
func Designations(lookup Lookup, filter DesignationFilter) []Designation {
	anyUse := filter.Use.System == "" && filter.Use.Code == ""
	var found []Designation
	for _, designation := range lookup.Designations {
		if !anyUse && (designation.Use == nil || designation.Use.System != filter.Use.System || designation.Use.Code != filter.Use.Code) {
			continue
		}
		if filter.Language != "" && (designation.Language == "" || !languageMatches(designation.Language, filter.Language)) {
			continue
		}
		found = append(found, designation)
	}
	return found
}

// languageMatches is RFC 4647's basic filtering: a range matches a tag equal to it, or one that
// starts with it and a hyphen, ignoring case; "*" matches every tag.
func languageMatches(tag, languageRange string) bool {
	tag, languageRange = strings.ToLower(tag), strings.ToLower(languageRange)
	return languageRange == "*" || tag == languageRange || strings.HasPrefix(tag, languageRange+"-")
}

// partReader reads one part's value, or reports that the part does not read as its type.
type partReader func(fhir.ParametersParameter) (any, bool)

// readParts reads one level's parts, in order: each part named in readers is read by its reader, and
// each takes the repeating parts it knows. A part neither reads goes to other, in place. Two parts
// with the name of one FHIR allows once are ambiguous, and an error.
func readParts(
	where string,
	parts []fhir.ParametersParameter,
	readers map[string]partReader,
	each func(fhir.ParametersParameter) (bool, error),
) (map[string]any, []fhir.ParametersParameter, error) {
	counts := map[string]int{}
	for _, part := range parts {
		if name := deref(part.Name); readers[name] != nil {
			counts[name]++
		}
	}
	for _, part := range parts {
		if name := deref(part.Name); counts[name] > 1 {
			hint := ""
			if where == "The Parameters" {
				hint = " Read them with Parameters."
			}
			return nil, nil, notSent(fmt.Sprintf("%s holds %d parts named %s, so which one is meant is ambiguous.%s", where, counts[name], name, hint))
		}
	}
	values := map[string]any{}
	var other []fhir.ParametersParameter
	for _, part := range parts {
		if name := deref(part.Name); readers[name] != nil {
			if value, ok := readers[name](part); ok {
				values[name] = value
			} else {
				other = append(other, part)
			}
			continue
		}
		taken := false
		if each != nil {
			var err error
			if taken, err = each(part); err != nil {
				return nil, nil, err
			}
		}
		if !taken {
			other = append(other, part)
		}
	}
	return values, other, nil
}

func readDesignation(part fhir.ParametersParameter) (Designation, bool, error) {
	values, other, err := readParts("A designation", part.Part, map[string]partReader{
		"language": textOf, "use": useOf, "value": textOf,
	}, nil)
	if err != nil {
		return Designation{}, false, err
	}
	value, ok := values["value"].(string)
	if !ok {
		return Designation{}, false, nil
	}
	designation := Designation{Value: value, Other: other}
	designation.Language, _ = values["language"].(string)
	if use, ok := values["use"].(DesignationUse); ok {
		designation.Use = &use
	}
	return designation, true, nil
}

func readProperty(part fhir.ParametersParameter, where string) (Property, bool, error) {
	var subproperties []Property
	values, other, err := readParts(where, part.Part, map[string]partReader{
		"code": textOf, "value": propertyValueOf, "description": textOf,
	}, func(sub fhir.ParametersParameter) (bool, error) {
		if deref(sub.Name) != "subproperty" {
			return false, nil
		}
		property, ok, err := readProperty(sub, "A subproperty")
		if ok {
			subproperties = append(subproperties, property)
		}
		return ok, err
	})
	if err != nil {
		return Property{}, false, err
	}
	code, hasCode := values["code"].(string)
	value, hasValue := values["value"].(typedValue)
	if !hasCode || !hasValue {
		return Property{}, false, nil
	}
	property := Property{Code: code, Type: value.kind, Value: value.value, Subproperties: subproperties, Other: other}
	property.Description, _ = values["description"].(string)
	return property, true, nil
}

// typedValue is a property's value with the FHIR data type it came as.
type typedValue struct {
	kind  string
	value any
}

// valueFields is the names of the value[x] fields a part carries.
func valueFields(part fhir.ParametersParameter) []string {
	v := reflect.ValueOf(part)
	var set []string
	for i := range v.NumField() {
		name := v.Type().Field(i).Name
		field := v.Field(i)
		if strings.HasPrefix(name, "Value") && !strings.HasSuffix(name, "Ext") && field.Kind() == reflect.Pointer && !field.IsNil() {
			set = append(set, name)
		}
	}
	return set
}

// textOf reads a part whose one value[x] is text: valueString, valueCode, valueUri or valueCanonical.
func textOf(part fhir.ParametersParameter) (any, bool) {
	fields := valueFields(part)
	if len(fields) != 1 {
		return nil, false
	}
	switch fields[0] {
	case "ValueString":
		return *part.ValueString, true
	case "ValueCode":
		return *part.ValueCode, true
	case "ValueURI":
		return *part.ValueURI, true
	case "ValueCanonical":
		return *part.ValueCanonical, true
	}
	return nil, false
}

func booleanOf(part fhir.ParametersParameter) (any, bool) {
	if fields := valueFields(part); len(fields) == 1 && fields[0] == "ValueBoolean" {
		return *part.ValueBoolean, true
	}
	return nil, false
}

func useOf(part fhir.ParametersParameter) (any, bool) {
	fields := valueFields(part)
	if len(fields) != 1 || fields[0] != "ValueCoding" || part.ValueCoding.System == nil || part.ValueCoding.Code == nil {
		return nil, false
	}
	return DesignationUse{System: *part.ValueCoding.System, Code: *part.ValueCoding.Code, Display: deref(part.ValueCoding.Display)}, true
}

func propertyValueOf(part fhir.ParametersParameter) (any, bool) {
	fields := valueFields(part)
	if len(fields) != 1 {
		return nil, false
	}
	switch fields[0] {
	case "ValueCode":
		return typedValue{"code", *part.ValueCode}, true
	case "ValueString":
		return typedValue{"string", *part.ValueString}, true
	case "ValueDateTime":
		return typedValue{"dateTime", *part.ValueDateTime}, true
	case "ValueInteger":
		return typedValue{"integer", *part.ValueInteger}, true
	case "ValueDecimal":
		return typedValue{"decimal", *part.ValueDecimal}, true
	case "ValueBoolean":
		return typedValue{"boolean", *part.ValueBoolean}, true
	case "ValueCoding":
		return typedValue{"Coding", *part.ValueCoding}, true
	}
	return nil, false
}
