package haau3

import (
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
