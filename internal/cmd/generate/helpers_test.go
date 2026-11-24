package main

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// The pinned spec, parsed.
func pinnedSpec(t *testing.T) *spec {
	t.Helper()
	data, err := os.ReadFile("../../../openapi/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	s, err := parseSpec(data)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func operationByID(t *testing.T, s *spec, id string) operation {
	t.Helper()
	i := slices.IndexFunc(s.Operations, func(o operation) bool { return o.ID == id })
	if i < 0 {
		t.Fatalf("no %s", id)
	}
	return s.Operations[i]
}

// withExample is o with one query parameter's example replaced; nil removes it.
func withExample(o operation, wire string, example any) operation {
	o.Query = slices.Clone(o.Query)
	for i := range o.Query {
		if o.Query[i].Wire == wire {
			o.Query[i].Example = example
		}
	}
	return o
}

func TestTheFirstCallsParametersAreTheExamplesLessTheIncludesItsHelperSends(t *testing.T) {
	fields, err := firstCallFields(operationByID(t, pinnedSpec(t), "searchSlot"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{`ProviderIdentifier: new("http://hl7.org/fhir/sid/us-npi|2900000227")`, "Count: new(1)"}
	if !slices.Equal(fields, want) {
		t.Errorf("fields %v", fields)
	}
}

func TestTheFirstCallTakesTheIncludesInAnyOrderAsOneValueOrAList(t *testing.T) {
	o := operationByID(t, pinnedSpec(t), "searchSlot")
	o = withExample(o, "_include", []any{"Slot:schedule"})
	o = withExample(o, "_include:iterate", []any{"HealthcareService:location", "Schedule:actor", "PractitionerRole:practitioner"})
	if _, err := firstCallFields(o); err != nil {
		t.Errorf("a list and another order: %v", err)
	}
}

func TestTheFirstCallIsRefusedWhenTheExampleSendsOneIncludeFewerOrOneMoreThanTheHelper(t *testing.T) {
	o := operationByID(t, pinnedSpec(t), "searchSlot")
	for name, edited := range map[string]operation{
		"one fewer":           withExample(o, "_include:iterate", []any{"Schedule:actor", "PractitionerRole:practitioner"}),
		"one more":            withExample(o, "_include:iterate", []any{"Slot:schedule", "Schedule:actor", "PractitionerRole:practitioner", "HealthcareService:location"}),
		"no _include":         withExample(o, "_include", nil),
		"another _include":    withExample(o, "_include", "Schedule:actor"),
		"no _include:iterate": withExample(o, "_include:iterate", nil),
	} {
		_, err := firstCallFields(edited)
		if err == nil || !strings.Contains(err.Error(), "is not what SearchSlotWithIncludes sends") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestTheFirstCallIsRefusedWhenTheExampleSendsNoQueryAtAll(t *testing.T) {
	o := operationByID(t, pinnedSpec(t), "searchSlot")
	for _, p := range o.Query {
		o = withExample(o, p.Wire, nil)
	}
	if _, err := firstCallFields(o); err == nil || !strings.Contains(err.Error(), "sends no query parameters") {
		t.Errorf("got %v", err)
	}
	other := operationByID(t, pinnedSpec(t), "searchPractitioner")
	if _, err := firstCallFields(other); err == nil || !strings.Contains(err.Error(), "no helper") {
		t.Errorf("an operation with no helper: %v", err)
	}
}

func TestTheGeneratorStopsWhenASearchAHelperMakesIsGoneOrTakesNoIncludesAsAList(t *testing.T) {
	s := pinnedSpec(t)
	if err := checkHelpers(s); err != nil {
		t.Fatalf("the pinned spec: %v", err)
	}
	gone := *s
	gone.Operations = slices.DeleteFunc(slices.Clone(s.Operations), func(o operation) bool { return o.ID == "searchSchedule" })
	if err := checkHelpers(&gone); err == nil || !strings.Contains(err.Error(), "searchSchedule has a helper") {
		t.Errorf("a search gone: %v", err)
	}
	for _, wire := range []string{"_include", "_include:iterate"} {
		required := *s
		required.Operations = slices.Clone(s.Operations)
		i := slices.IndexFunc(required.Operations, func(o operation) bool { return o.ID == "searchSlot" })
		required.Operations[i].Query = slices.Clone(required.Operations[i].Query)
		for j := range required.Operations[i].Query {
			if required.Operations[i].Query[j].Wire == wire {
				required.Operations[i].Query[j].Required = true
			}
		}
		if err := checkHelpers(&required); err == nil || !strings.Contains(err.Error(), "sends "+wire) {
			t.Errorf("%s required: %v", wire, err)
		}
		single := *s
		single.Operations = slices.Clone(s.Operations)
		i = slices.IndexFunc(single.Operations, func(o operation) bool { return o.ID == "searchSlot" })
		single.Operations[i].Query = slices.Clone(single.Operations[i].Query)
		for j := range single.Operations[i].Query {
			if single.Operations[i].Query[j].Wire == wire {
				single.Operations[i].Query[j].Kind = "string"
			}
		}
		if err := checkHelpers(&single); err == nil || !strings.Contains(err.Error(), "sends "+wire) {
			t.Errorf("%s as one value: %v", wire, err)
		}
	}
}
