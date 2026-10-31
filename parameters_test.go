package haau3

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"testing"

	"github.com/haau3/haau3-go/fhir"
)

// The spec's own example of StructureDosage's answer, so a change to that answer's shape fails here.
func structureDosageAnswer(t *testing.T) *fhir.Parameters {
	t.Helper()
	i := slices.IndexFunc(specExamples, func(ex specExample) bool { return ex.id == "structureDosage" })
	if i < 0 {
		t.Fatal("the spec has no structureDosage example")
	}
	return exampleBody[fhir.Parameters](specExamples[i].answer)
}

func TestParametersAndParameterGiveThePartsWithAName(t *testing.T) {
	answer := structureDosageAnswer(t)
	if got := len(Parameters(answer, "dosage")); got != 1 {
		t.Fatalf("%d dosage parts, want 1", got)
	}
	part, err := Parameter(answer, "dosage")
	if err != nil || part == nil || part.ValueDosage == nil || deref(part.ValueDosage.Text) != "Instill 2 drops in the affected ear twice daily" {
		t.Fatalf("Parameter(dosage) = %+v, %v", part, err)
	}
}

func TestParametersGiveNothingForANameTheAnswerDoesNotHold(t *testing.T) {
	answer := structureDosageAnswer(t)
	if got := Parameters(answer, "display"); len(got) != 0 {
		t.Errorf("Parameters(display) = %v, want none", got)
	}
	if part, err := Parameter(answer, "display"); part != nil || err != nil {
		t.Errorf("Parameter(display) = %v, %v, want nil, nil", part, err)
	}
	if got := Parameters(nil, "dosage"); got != nil {
		t.Errorf("Parameters(nil) = %v", got)
	}
}

func TestParameterIsAnErrorWhenTwoPartsShareTheName(t *testing.T) {
	two := &fhir.Parameters{Parameter: []fhir.ParametersParameter{
		{Name: new("property"), ValueString: new("a")},
		{Name: new("property"), ValueString: new("b")},
	}}
	if _, err := Parameter(two, "property"); err == nil {
		t.Error("Parameter of a name two parts share answered, want an error")
	}
	if got := Parameters(two, "property"); len(got) != 2 || deref(got[1].ValueString) != "b" {
		t.Errorf("Parameters(property) = %v, want both", got)
	}
}

func TestReviewFlagsReadsTheExamplesFlagWithItsWordsAndItsVersions(t *testing.T) {
	part, err := Parameter(structureDosageAnswer(t), "dosage")
	if err != nil || part == nil {
		t.Fatalf("Parameter(dosage) = %v, %v", part, err)
	}
	review := ReviewFlags(part.ValueDosage)
	want := []ReviewFlag{{Code: "route-form-mismatch", Display: "Route and dose form differ", Words: "in the affected ear"}}
	if !slices.Equal(review.Flags, want) {
		t.Errorf("Flags = %+v, want %+v", review.Flags, want)
	}
	version := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\.v\d+$`)
	if !version.MatchString(review.EngineVersion) || !version.MatchString(review.VocabularyVersion) {
		t.Errorf("versions %q and %q", review.EngineVersion, review.VocabularyVersion)
	}
	if !regexp.MustCompile(`^\d{8}$`).MatchString(review.RxnormRelease) {
		t.Errorf("RxnormRelease %q", review.RxnormRelease)
	}
}

// The control: a Dosage with no extensions has no flags and names no version.
func TestReviewFlagsGivesNoneForADosageThatCarriesNone(t *testing.T) {
	review := ReviewFlags(&fhir.Dosage{Text: new("Take 1 tablet daily")})
	if len(review.Flags) != 0 || review.EngineVersion != "" || review.VocabularyVersion != "" || review.RxnormRelease != "" {
		t.Errorf("ReviewFlags = %+v, want nothing", review)
	}
	if review := ReviewFlags(nil); len(review.Flags) != 0 {
		t.Errorf("ReviewFlags(nil) = %+v", review)
	}
}

func TestReviewFlagsLeavesOutOtherExtensionsAndAFlagWithNoCode(t *testing.T) {
	dosage := &fhir.Dosage{Extension: []fhir.Extension{
		{URL: new("https://example.org/other"), ValueString: new("x")},
		{URL: new(reviewFlagURL), Extension: []fhir.Extension{{URL: new("words"), ValueString: new("x")}}},
		{URL: new(reviewFlagURL), Extension: []fhir.Extension{{URL: new("code"), ValueCoding: &fhir.Coding{Code: new("as-directed"), Display: new("As directed")}}}},
	}}
	want := []ReviewFlag{{Code: "as-directed", Display: "As directed"}}
	if got := ReviewFlags(dosage).Flags; !slices.Equal(got, want) {
		t.Errorf("Flags = %+v, want %+v", got, want)
	}
}

// The spec's own example of StructureDosage's request, which the builder must reproduce.
func structureDosageExampleRequest(t *testing.T) *fhir.Parameters {
	t.Helper()
	i := slices.IndexFunc(specExamples, func(ex specExample) bool { return ex.id == "structureDosage" })
	if i < 0 || specExamples[i].body == "" {
		t.Fatal("the spec has no structureDosage request example")
	}
	return exampleBody[fhir.Parameters](specExamples[i].body)
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestStructureDosageRequestBuildsTheSpecsOwnExampleRequest(t *testing.T) {
	example := structureDosageExampleRequest(t)
	text, err := Parameter(example, "text")
	if err != nil || text == nil {
		t.Fatalf("text: %v", err)
	}
	medication, err := Parameter(example, "medication")
	if err != nil || medication == nil || medication.ValueCodeableConcept == nil {
		t.Fatalf("medication: %v", err)
	}
	if got := deref(medication.ValueCodeableConcept.Coding[0].System); got != RxNormSystem {
		t.Errorf("the example's medication system is %s, want RxNormSystem", got)
	}
	example.ID = nil
	built := StructureDosageRequest(StructureDosageInput{Text: deref(text.ValueString), Medication: medication.ValueCodeableConcept})
	if got, want := jsonOf(t, built), jsonOf(t, example); got != want {
		t.Errorf("built %s\nwant %s", got, want)
	}
}

func TestStructureDosageRequestSendsATextPartAloneWhenThereIsNoMedication(t *testing.T) {
	got := jsonOf(t, StructureDosageRequest(StructureDosageInput{Text: "Take 1 tablet by mouth twice daily"}))
	want := `{"resourceType":"Parameters","parameter":[{"name":"text","valueString":"Take 1 tablet by mouth twice daily"}]}`
	if got != want {
		t.Errorf("built %s", got)
	}
}

func TestStructureDosageRequestKeepsEveryCodingAndChecksNothing(t *testing.T) {
	medication := &fhir.CodeableConcept{Coding: []fhir.Coding{
		{System: new(NDCSystem), Code: new("0378-0018-01")},
		{System: new(RxNormSystem), Code: new("197361")},
	}}
	built := StructureDosageRequest(StructureDosageInput{Text: "Take 1 tablet daily", Medication: medication})
	if part, err := Parameter(built, "medication"); err != nil || part == nil || part.ValueCodeableConcept != medication {
		t.Errorf("medication part = %+v, %v", part, err)
	}
	// The API's refusals are the one rule: an empty text is sent as it is.
	if got := jsonOf(t, StructureDosageRequest(StructureDosageInput{}).Parameter); got != `[{"name":"text","valueString":""}]` {
		t.Errorf("empty text built %s", got)
	}
}

func TestStructureDosageRequestIsWhatStructureDosageSendsAloneAndInABatch(t *testing.T) {
	answer := structureDosageAnswer(t)
	rec := record(
		fhirReply(200, answer),
		fhirReply(200, batchResponse(map[string]any{"resource": answer, "response": map[string]any{"status": "200"}})),
	)
	client := testClient(t, rec)
	body := StructureDosageRequest(StructureDosageInput{Text: "Take 1 tablet by mouth twice daily"})
	if _, err := client.StructureDosage(context.Background(), body); err != nil {
		t.Fatal(err)
	}
	sent := `{"resourceType":"Parameters","parameter":[{"name":"text","valueString":"Take 1 tablet by mouth twice daily"}]}`
	if got := string(rec.lastBody()); got != sent {
		t.Errorf("sent %s", got)
	}
	entry := Calls.StructureDosage(body)
	if err := client.Batch(context.Background(), []Entry{entry}); err != nil {
		t.Fatal(err)
	}
	want := `{"resourceType":"Bundle","type":"batch","entry":[{"request":{"method":"POST","url":"$structure-dosage"},"resource":` + sent + `}]}`
	if got := string(rec.lastBody()); got != want {
		t.Errorf("batch sent %s", got)
	}
	if _, err := entry.Result(); err != nil {
		t.Errorf("entry: %v", err)
	}
}
