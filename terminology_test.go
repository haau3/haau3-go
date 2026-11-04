package haau3

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/haau3/haau3-go/fhir"
)

// LookupResult, ValidationResult and Designations: a terminology answer, typed, with no request. The
// spec's own examples are read with "every part is typed or kept" checks, so a change to their values
// cannot break these tests and a change to their shape does. The answers of a code system the API
// holds are written here, as the API writes them: an ICD-10-CM code with its short title, and a
// made-up drug in RxNorm's shape.

const (
	icd10cmSystem = "http://hl7.org/fhir/sid/icd-10-cm"
	longTitle     = "Diabetes mellitus due to underlying condition with diabetic macular edema, resolved following treatment, right eye"
	shortTitle    = "Diab with diabetic macular edema, resolved fol trtmt, r eye"
	rxnormWarning = "RxNorm is held in English only, so the display and designations are in English."
)

func useCoding(use DesignationUse) fhir.ParametersParameter {
	return fhir.ParametersParameter{Name: new("use"), ValueCoding: &fhir.Coding{System: new(use.System), Code: new(use.Code), Display: new(use.Display)}}
}

func designationPart(parts ...fhir.ParametersParameter) fhir.ParametersParameter {
	return fhir.ParametersParameter{Name: new("designation"), Part: parts}
}

func propertyPart(parts ...fhir.ParametersParameter) fhir.ParametersParameter {
	return fhir.ParametersParameter{Name: new("property"), Part: parts}
}

func named(name string, part fhir.ParametersParameter) fhir.ParametersParameter {
	part.Name = new(name)
	return part
}

func parametersOf(parts ...fhir.ParametersParameter) *fhir.Parameters {
	return &fhir.Parameters{Parameter: parts}
}

// An ICD-10-CM lookup, as the API answers one: the head, both titles, then notSelectable.
func icd10cmLookup() *fhir.Parameters {
	return parametersOf(
		named("name", fhir.ParametersParameter{ValueString: new("ICD-10-CM")}),
		named("code", fhir.ParametersParameter{ValueCode: new("E08.37X1")}),
		named("system", fhir.ParametersParameter{ValueURI: new(icd10cmSystem)}),
		named("version", fhir.ParametersParameter{ValueString: new("2026-10-01")}),
		named("display", fhir.ParametersParameter{ValueString: new(longTitle)}),
		named("abstract", fhir.ParametersParameter{ValueBoolean: new(false)}),
		designationPart(named("language", fhir.ParametersParameter{ValueCode: new("en")}), useCoding(PreferredForLanguageUse), named("value", fhir.ParametersParameter{ValueString: new(longTitle)})),
		designationPart(named("language", fhir.ParametersParameter{ValueCode: new("en")}), useCoding(SynonymUse), named("value", fhir.ParametersParameter{ValueString: new(shortTitle)})),
		propertyPart(named("code", fhir.ParametersParameter{ValueCode: new("notSelectable")}), named("value", fhir.ParametersParameter{ValueBoolean: new(false)})),
	)
}

// A made-up drug in RxNorm's shape: names with no use, and properties of three types.
func rxnormLookup() *fhir.Parameters {
	return parametersOf(
		named("name", fhir.ParametersParameter{ValueString: new("RxNorm")}),
		named("code", fhir.ParametersParameter{ValueCode: new("R1000")}),
		named("system", fhir.ParametersParameter{ValueURI: new(RxNormSystem)}),
		named("display", fhir.ParametersParameter{ValueString: new("fake drug 10 mg tablet")}),
		named("warning", fhir.ParametersParameter{ValueString: new(rxnormWarning)}),
		designationPart(named("language", fhir.ParametersParameter{ValueCode: new("en")}), useCoding(PreferredForLanguageUse), named("value", fhir.ParametersParameter{ValueString: new("fake drug 10 mg tablet")})),
		designationPart(named("language", fhir.ParametersParameter{ValueCode: new("en")}), named("value", fhir.ParametersParameter{ValueString: new("fake drug 10 mg")})),
		propertyPart(named("code", fhir.ParametersParameter{ValueCode: new("TTY")}), named("value", fhir.ParametersParameter{ValueCode: new("SCD")})),
		propertyPart(named("code", fhir.ParametersParameter{ValueCode: new("inactive")}), named("value", fhir.ParametersParameter{ValueBoolean: new(false)})),
		propertyPart(named("code", fhir.ParametersParameter{ValueCode: new("has_dose_form")}), named("value", fhir.ParametersParameter{ValueCoding: &fhir.Coding{System: new(RxNormSystem), Code: new("R1002"), Display: new("fake form")}})),
	)
}

func exampleParameters(t *testing.T, id string) *fhir.Parameters {
	t.Helper()
	i := slices.IndexFunc(specExamples, func(ex specExample) bool { return ex.id == id })
	if i < 0 {
		t.Fatalf("the spec has no %s example", id)
	}
	return exampleBody[fhir.Parameters](specExamples[i].answer)
}

func values(designations []Designation) []string {
	var found []string
	for _, designation := range designations {
		found = append(found, designation.Value)
	}
	return found
}

func TestLookupResultReadsAnICD10CMLookup(t *testing.T) {
	lookup, err := LookupResult(icd10cmLookup())
	if err != nil {
		t.Fatal(err)
	}
	if lookup.Name != "ICD-10-CM" || lookup.Code != "E08.37X1" || lookup.System != icd10cmSystem || lookup.Version != "2026-10-01" ||
		lookup.Display != longTitle || lookup.Abstract == nil || *lookup.Abstract || lookup.Warning != "" || len(lookup.Other) != 0 {
		t.Errorf("LookupResult = %+v", lookup)
	}
	want := []Designation{
		{Language: "en", Use: &PreferredForLanguageUse, Value: longTitle},
		{Language: "en", Use: &SynonymUse, Value: shortTitle},
	}
	if !reflect.DeepEqual(lookup.Designations, want) {
		t.Errorf("Designations = %+v, want %+v", lookup.Designations, want)
	}
	if want := []Property{{Code: "notSelectable", Type: "boolean", Value: false}}; !reflect.DeepEqual(lookup.Properties, want) {
		t.Errorf("Properties = %+v, want %+v", lookup.Properties, want)
	}
}

func TestLookupResultTypesEachPropertyByItsValueAndReadsTheWarning(t *testing.T) {
	lookup, err := LookupResult(rxnormLookup())
	if err != nil {
		t.Fatal(err)
	}
	if lookup.Warning != rxnormWarning {
		t.Errorf("Warning = %q", lookup.Warning)
	}
	if got := lookup.Properties; len(got) != 3 || got[0].Type != "code" || got[0].Value != "SCD" || got[1].Type != "boolean" || got[1].Value != false || got[2].Type != "Coding" {
		t.Fatalf("Properties = %+v", got)
	}
	// A type switch on Value reads it.
	switch value := lookup.Properties[2].Value.(type) {
	case fhir.Coding:
		if *value.Code != "R1002" {
			t.Errorf("the Coding's code = %q", *value.Code)
		}
	default:
		t.Errorf("Value is a %T, want a fhir.Coding", value)
	}
	if got := lookup.Designations[1]; got.Use != nil || got.Value != "fake drug 10 mg" {
		t.Errorf("the designation with no use = %+v", got)
	}
}

func TestLookupResultReadsTheSpecsOwnExampleTypingOrKeepingEveryPart(t *testing.T) {
	answer := exampleParameters(t, "codeSystemLookup")
	lookup, err := LookupResult(answer)
	if err != nil {
		t.Fatal(err)
	}
	typed := 0
	for _, field := range []string{lookup.Name, lookup.Code, lookup.System, lookup.Version, lookup.Display, lookup.Warning} {
		if field != "" {
			typed++
		}
	}
	if lookup.Abstract != nil {
		typed++
	}
	if got := typed + len(lookup.Designations) + len(lookup.Properties) + len(lookup.Other); got != len(answer.Parameter) {
		t.Errorf("%d parts typed or kept, want the answer's %d", got, len(answer.Parameter))
	}
	// The example names its system as a valueString, which reads as the system all the same.
	if lookup.System != "http://loinc.org" {
		t.Errorf("System = %q", lookup.System)
	}
}

func TestLookupResultReadsSubpropertiesADescriptionAndThePartsItDoesNotKnow(t *testing.T) {
	definition := named("definition", fhir.ParametersParameter{ValueString: new("A made-up definition")})
	additionalUse := named("additionalUse", fhir.ParametersParameter{ValueCoding: &fhir.Coding{System: new("http://example.org/use"), Code: new("extra")}})
	source := named("source", fhir.ParametersParameter{ValueURI: new("http://example.org/source")})
	lookup, err := LookupResult(parametersOf(
		definition,
		designationPart(named("language", fhir.ParametersParameter{ValueCode: new("en")}), additionalUse, named("value", fhir.ParametersParameter{ValueString: new("a name")})),
		propertyPart(
			named("code", fhir.ParametersParameter{ValueCode: new("parent")}),
			named("value", fhir.ParametersParameter{ValueInteger: new(3)}),
			named("description", fhir.ParametersParameter{ValueString: new("how many")}),
			source,
			named("subproperty", fhir.ParametersParameter{Part: []fhir.ParametersParameter{
				named("code", fhir.ParametersParameter{ValueCode: new("child")}),
				named("value", fhir.ParametersParameter{ValueDecimal: new(json.Number("1.5"))}),
			}}),
			named("subproperty", fhir.ParametersParameter{Part: []fhir.ParametersParameter{
				named("code", fhir.ParametersParameter{ValueCode: new("dated")}),
				named("value", fhir.ParametersParameter{ValueDateTime: new("2026-11-04")}),
			}}),
		),
	))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(lookup.Other, []fhir.ParametersParameter{definition}) {
		t.Errorf("Other = %+v", lookup.Other)
	}
	if !reflect.DeepEqual(lookup.Designations[0].Other, []fhir.ParametersParameter{additionalUse}) {
		t.Errorf("the designation's Other = %+v", lookup.Designations[0].Other)
	}
	want := []Property{{
		Code: "parent", Type: "integer", Value: 3, Description: "how many",
		Subproperties: []Property{
			{Code: "child", Type: "decimal", Value: json.Number("1.5")},
			{Code: "dated", Type: "dateTime", Value: "2026-11-04"},
		},
		Other: []fhir.ParametersParameter{source},
	}}
	if !reflect.DeepEqual(lookup.Properties, want) {
		t.Errorf("Properties = %+v, want %+v", lookup.Properties, want)
	}
}

func TestLookupResultKeepsAPartItCannotReadInOtherInAnswerOrder(t *testing.T) {
	noValue := designationPart(named("language", fhir.ParametersParameter{ValueCode: new("en")}))
	otherType := propertyPart(named("code", fhir.ParametersParameter{ValueCode: new("x")}), named("value", fhir.ParametersParameter{ValueAttachment: &fhir.Attachment{}}))
	textAbstract := named("abstract", fhir.ParametersParameter{ValueString: new("false")})
	noCode := propertyPart(named("value", fhir.ParametersParameter{ValueString: new("y")}))
	lookup, err := LookupResult(parametersOf(noValue, named("display", fhir.ParametersParameter{ValueString: new("A")}), otherType, textAbstract, noCode))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(lookup.Other, []fhir.ParametersParameter{noValue, otherType, textAbstract, noCode}) {
		t.Errorf("Other = %+v", lookup.Other)
	}
	if lookup.Abstract != nil || lookup.Display != "A" || len(lookup.Designations) != 0 || len(lookup.Properties) != 0 {
		t.Errorf("LookupResult = %+v", lookup)
	}
}

func TestLookupResultKeepsAUseWithNoCodeInTheDesignationsOther(t *testing.T) {
	noCode := named("use", fhir.ParametersParameter{ValueCoding: &fhir.Coding{System: new("http://example.org/use")}})
	lookup, err := LookupResult(parametersOf(designationPart(noCode, named("value", fhir.ParametersParameter{ValueString: new("a name")}))))
	if err != nil {
		t.Fatal(err)
	}
	want := []Designation{{Value: "a name", Other: []fhir.ParametersParameter{noCode}}}
	if !reflect.DeepEqual(lookup.Designations, want) {
		t.Errorf("Designations = %+v, want %+v", lookup.Designations, want)
	}
}

func TestLookupResultReadsAMissingPartAsEmpty(t *testing.T) {
	for _, answer := range []*fhir.Parameters{nil, {}} {
		lookup, err := LookupResult(answer)
		if err != nil || !reflect.DeepEqual(lookup, Lookup{}) {
			t.Errorf("LookupResult(%v) = %+v, %v, want the zero Lookup", answer, lookup, err)
		}
	}
}

func TestLookupResultIsAnErrorWhenAPartFHIRAllowsOnceComesTwice(t *testing.T) {
	_, err := LookupResult(parametersOf(
		named("display", fhir.ParametersParameter{ValueString: new("A")}),
		named("display", fhir.ParametersParameter{ValueString: new("B")}),
	))
	if e := asError(t, err); e.Kind != KindNotSent || e.Error() != "The Parameters holds 2 parts named display, so which one is meant is ambiguous. Read them with Parameters." {
		t.Errorf("two displays = %#v (%v)", e, e)
	}
	_, err = LookupResult(parametersOf(designationPart(
		named("value", fhir.ParametersParameter{ValueString: new("A")}),
		named("value", fhir.ParametersParameter{ValueString: new("B")}),
	)))
	if e := asError(t, err); e.Error() != "A designation holds 2 parts named value, so which one is meant is ambiguous." {
		t.Errorf("two values in a designation = %v", e)
	}
}

func TestValidationResultReadsADisplayThatDidNotMatch(t *testing.T) {
	check, err := ValidationResult(parametersOf(
		named("result", fhir.ParametersParameter{ValueBoolean: new(false)}),
		named("display", fhir.ParametersParameter{ValueString: new("Heart rate")}),
		named("message", fhir.ParametersParameter{ValueString: new("The display sent is not the code's.")}),
	))
	if err != nil || check.Result == nil || *check.Result || check.Display != "Heart rate" || check.Message != "The display sent is not the code's." || len(check.Other) != 0 {
		t.Errorf("ValidationResult = %+v, %v", check, err)
	}
}

func TestValidationResultReadsTheSpecsOwnExamplesTypingOrKeepingEveryPart(t *testing.T) {
	for _, id := range []string{"codeSystemValidateCode", "valueSetValidateCode"} {
		answer := exampleParameters(t, id)
		check, err := ValidationResult(answer)
		if err != nil || check.Result == nil || !*check.Result {
			t.Fatalf("%s: ValidationResult = %+v, %v", id, check, err)
		}
		typed := 1
		for _, field := range []string{check.Display, check.Message} {
			if field != "" {
				typed++
			}
		}
		if got := typed + len(check.Other); got != len(answer.Parameter) {
			t.Errorf("%s: %d parts typed or kept, want the answer's %d", id, got, len(answer.Parameter))
		}
	}
}

func TestValidationResultIsAnErrorOnTwoResults(t *testing.T) {
	_, err := ValidationResult(parametersOf(
		named("result", fhir.ParametersParameter{ValueBoolean: new(true)}),
		named("result", fhir.ParametersParameter{ValueBoolean: new(false)}),
	))
	if e := asError(t, err); e.Kind != KindNotSent {
		t.Errorf("two results = %#v", e)
	}
}

func TestDesignationsFindsTheShortTitleByItsUseAndLanguage(t *testing.T) {
	lookup, _ := LookupResult(icd10cmLookup())
	if got := values(Designations(lookup, DesignationFilter{Use: SynonymUse, Language: "en"})); !slices.Equal(got, []string{shortTitle}) {
		t.Errorf("the synonym = %v", got)
	}
	if got := values(Designations(lookup, DesignationFilter{Use: PreferredForLanguageUse})); !slices.Equal(got, []string{longTitle}) {
		t.Errorf("the preferred name = %v", got)
	}
	if got := Designations(lookup, DesignationFilter{}); !reflect.DeepEqual(got, lookup.Designations) {
		t.Errorf("no filter = %+v, want every designation", got)
	}
}

func TestDesignationsMatchesAUseByItsSystemAndCodeTogether(t *testing.T) {
	lookup, _ := LookupResult(icd10cmLookup())
	if got := Designations(lookup, DesignationFilter{Use: DesignationUse{System: "http://snomed.info/sct", Code: "synonym"}}); len(got) != 0 {
		t.Errorf("the code under another system = %v, want none", values(got))
	}
	if got := Designations(lookup, DesignationFilter{Use: DesignationUse{System: SynonymUse.System, Code: "synonym", Display: "another"}}); len(got) != 1 {
		t.Errorf("another display = %v, want the synonym", values(got))
	}
}

func TestDesignationsMatchesALanguageAsRFC4647BasicFilteringDoes(t *testing.T) {
	var parts []fhir.ParametersParameter
	for _, tag := range []string{"en", "en-US", "EN-gb", "eng", "de"} {
		parts = append(parts, designationPart(named("language", fhir.ParametersParameter{ValueCode: new(tag)}), named("value", fhir.ParametersParameter{ValueString: new(tag)})))
	}
	parts = append(parts, designationPart(named("value", fhir.ParametersParameter{ValueString: new("no language")})))
	lookup, _ := LookupResult(parametersOf(parts...))
	for language, want := range map[string][]string{
		"en":    {"en", "en-US", "EN-gb"},
		"EN-US": {"en-US"},
		"*":     {"en", "en-US", "EN-gb", "eng", "de"},
	} {
		if got := values(Designations(lookup, DesignationFilter{Language: language})); !slices.Equal(got, want) {
			t.Errorf("language %q = %v, want %v", language, got, want)
		}
	}
	if got := values(Designations(lookup, DesignationFilter{})); !slices.Contains(got, "no language") {
		t.Errorf("no filter = %v, want the designation with no language too", got)
	}
}

func TestDesignationsFindsOneWithNoUseOnlyWhenNoUseIsAskedFor(t *testing.T) {
	lookup, _ := LookupResult(rxnormLookup())
	if got := values(Designations(lookup, DesignationFilter{Language: "en"})); !slices.Contains(got, "fake drug 10 mg") {
		t.Errorf("in English = %v, want the name with no use", got)
	}
	if got := Designations(lookup, DesignationFilter{Use: SynonymUse}); len(got) != 0 {
		t.Errorf("the synonym = %v, want none", values(got))
	}
}
