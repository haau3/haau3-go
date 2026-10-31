package haau3

import (
	"fmt"

	"github.com/haau3/haau3-go/fhir"
)

// The extensions Structured Sig writes on each Dosage it answers with, as its implementation guide at
// https://fhir.haau3.com names them.
const (
	profileBase      = "https://haau3.com/fhir/StructureDefinition"
	reviewFlagURL    = profileBase + "/structured-sig-review-flag"
	engineVersionURL = profileBase + "/structured-sig-engine-version"
	vocabularyURL    = profileBase + "/structured-sig-vocabulary-version"
	rxnormReleaseURL = profileBase + "/structured-sig-rxnorm-release"
)

// Parameters is every part of a Parameters answer with that name, in order, with no request: the way
// FHIR operations answer, CodeSystemLookup's "property" parts or StructureDosage's "dosage" part. Each
// part holds its value in the value[x] its operation names, so read ValueDosage, ValueString and so on
// from it. It is empty when there is none. It reads an answer as FHIR's own SDKs do (Firely's Get,
// HAPI's ParametersUtil).
//
//	answer, err := client.CodeSystemLookup(ctx, haau3.CodeSystemLookupParams{System: new("http://loinc.org"), Code: "2345-7", Property: []string{"COMPONENT"}})
//	if err != nil {
//		return err
//	}
//	for _, property := range haau3.Parameters(answer, "property") {
//		fmt.Println(property.Part)
//	}
func Parameters(answer *fhir.Parameters, name string) []fhir.ParametersParameter {
	if answer == nil {
		return nil
	}
	var found []fhir.ParametersParameter
	for _, part := range answer.Parameter {
		if part.Name != nil && *part.Name == name {
			found = append(found, part)
		}
	}
	return found
}

// Parameter is the one part of a Parameters answer with that name, with no request, or nil when there
// is none. It returns an error when there are two, since which one is meant is then ambiguous;
// Parameters reads a name that repeats. It reads an answer as FHIR's own SDKs do (Firely's
// GetSingleValue, HAPI's ParametersUtil).
//
//	answer, err := client.CodeSystemLookup(ctx, haau3.CodeSystemLookupParams{System: new("http://loinc.org"), Code: "2345-7"})
//	if err != nil {
//		return err
//	}
//	display, err := haau3.Parameter(answer, "display")
//	if err == nil && display != nil && display.ValueString != nil {
//		fmt.Println(*display.ValueString)
//	}
func Parameter(answer *fhir.Parameters, name string) (*fhir.ParametersParameter, error) {
	found := Parameters(answer, name)
	switch len(found) {
	case 0:
		return nil, nil
	case 1:
		return &found[0], nil
	default:
		return nil, fmt.Errorf("haau3: the Parameters holds %d parts named %s, so which one is meant is ambiguous; read them with Parameters", len(found), name)
	}
}

// ReviewFlag is one thing a person should check about a Dosage.
type ReviewFlag struct {
	// Code is the flag's code, from https://haau3.com/fhir/CodeSystem/structured-sig-review-flag.
	Code string
	// Display is the flag's display, such as "Route and dose form differ".
	Display string
	// Words are the words of the directions the flag is about, exactly as written, or "" when the
	// flag is not about words.
	Words string
}

// DosageReview is what ReviewFlags reads from a Dosage. A version it does not name is "".
type DosageReview struct {
	// Flags is every flag on the Dosage, in the order the answer gives them. It is empty when nothing
	// needs checking.
	Flags []ReviewFlag
	// EngineVersion is how the directions were read, in the form YYYY-MM-DD.vN.
	EngineVersion string
	// VocabularyVersion is the codes the directions were coded with, in the form YYYY-MM-DD.vN.
	VocabularyVersion string
	// RxnormRelease is the RxNorm release the medication's code was looked up in, when it carried one.
	RxnormRelease string
}

// ReviewFlags reads the review flags on a Dosage StructureDosage answered with, and the versions it
// names, with no request. Each flag is something a person should check: its code, its display, and
// the words of the directions it is about, when it is about words.
//
//	answer, err := client.StructureDosage(ctx, &fhir.Parameters{Parameter: []fhir.ParametersParameter{
//		{Name: new("text"), ValueString: new("Take 1 tablet by mouth twice daily")},
//	}})
//	if err != nil {
//		return err
//	}
//	part, err := haau3.Parameter(answer, "dosage")
//	if err == nil && part != nil && part.ValueDosage != nil {
//		for _, flag := range haau3.ReviewFlags(part.ValueDosage).Flags {
//			fmt.Println(flag.Code, flag.Words)
//		}
//	}
func ReviewFlags(dosage *fhir.Dosage) DosageReview {
	var review DosageReview
	if dosage == nil {
		return review
	}
	for _, extension := range dosage.Extension {
		url := deref(extension.URL)
		switch url {
		case reviewFlagURL:
			var flag ReviewFlag
			for _, part := range extension.Extension {
				switch deref(part.URL) {
				case "code":
					if part.ValueCoding != nil {
						flag.Code, flag.Display = deref(part.ValueCoding.Code), deref(part.ValueCoding.Display)
					}
				case "words":
					flag.Words = deref(part.ValueString)
				}
			}
			// A flag is its code; one without a code says nothing a caller could act on.
			if flag.Code != "" {
				review.Flags = append(review.Flags, flag)
			}
		case engineVersionURL:
			review.EngineVersion = deref(extension.ValueString)
		case vocabularyURL:
			review.VocabularyVersion = deref(extension.ValueString)
		case rxnormReleaseURL:
			review.RxnormRelease = deref(extension.ValueString)
		}
	}
	return review
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
