package haau3

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/haau3/haau3-go/fhir"
)

// Matches and Resolve: a page's matches, and what a reference names in a Bundle, with no request.
// Both are functions of a Bundle alone, so neither can send anything.

const apiFHIRBase = "https://api.haau3.com/v1/fhir/r4"

func ref(reference string) *fhir.Reference { return &fhir.Reference{Reference: new(reference)} }

func entryAt(fullURL string, resource fhir.Resource, mode string) fhir.BundleEntry {
	e := fhir.BundleEntry{Resource: resource}
	if fullURL != "" {
		e.FullURL = new(fullURL)
	}
	if mode != "" {
		e.Search = &fhir.BundleSearch{Mode: new(mode)}
	}
	return e
}

// pharmacyPage is a pharmacy search as the API answers one: the matched service first, then what
// _include brought, each at its RESTful address, the service naming the others by relative
// reference.
func pharmacyPage() (page *fhir.Bundle, service *fhir.HealthcareService, organization *fhir.Organization, store *fhir.Location) {
	service = &fhir.HealthcareService{
		ID: new("HealthcareService-pharmacy-1234567893"), Name: new("EXAMPLE PHARMACY"),
		ProvidedBy: ref("Organization/Organization-1234567893"),
		Location:   []fhir.Reference{*ref("Location/Location-pharmacy-1234567893")},
	}
	organization = &fhir.Organization{ID: new("Organization-1234567893"), Name: new("EXAMPLE PHARMACY")}
	store = &fhir.Location{ID: new("Location-pharmacy-1234567893"), Address: &fhir.Address{City: new("MIAMI")}}
	page = &fhir.Bundle{Type: new("searchset"), Entry: []fhir.BundleEntry{
		entryAt(apiFHIRBase+"/HealthcareService/"+*service.ID, service, "match"),
		entryAt(apiFHIRBase+"/Organization/"+*organization.ID, organization, "include"),
		entryAt(apiFHIRBase+"/Location/"+*store.ID, store, "include"),
		entryAt("", &fhir.OperationOutcome{}, "outcome"),
	}}
	return page, service, organization, store
}

func notSentWith(t *testing.T, err error, words string) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Kind != KindNotSent || !strings.Contains(e.Error(), words) {
		t.Fatalf("want an error of KindNotSent saying %q, got %v", words, err)
	}
}

func TestMatchesGivesThePagesMatchesOfATypeAndNoIncludedResourceOrOutcome(t *testing.T) {
	page, service, _, _ := pharmacyPage()
	if got := Matches[*fhir.HealthcareService](page); len(got) != 1 || got[0] != service {
		t.Errorf("HealthcareService matches: %v", got)
	}
	if got := Matches[*fhir.Organization](page); len(got) != 0 {
		t.Errorf("an included Organization is a match: %v", got)
	}
	if got := Matches[*fhir.OperationOutcome](page); len(got) != 0 {
		t.Errorf("the search's outcome is a match: %v", got)
	}
}

func TestMatchesGivesEveryMatchAsFHIRResourceAnEntryThatSaysNoModeAmongThem(t *testing.T) {
	page, service, _, _ := pharmacyPage()
	noMode := &fhir.Organization{ID: new("no-mode")}
	page.Entry = append(page.Entry, entryAt("", noMode, ""))
	got := Matches[fhir.Resource](page)
	if len(got) != 2 || got[0] != fhir.Resource(service) || got[1] != fhir.Resource(noMode) {
		t.Errorf("every match: %v", got)
	}
	if Matches[fhir.Resource](nil) != nil {
		t.Error("a nil page has matches")
	}
}

func TestResolveFindsWhatARelativeReferenceNamesAtTheAddressOfTheEntryHoldingIt(t *testing.T) {
	page, service, organization, store := pharmacyPage()
	if got, err := Resolve[*fhir.Organization](page, service.ProvidedBy, service); err != nil || got != organization {
		t.Errorf("organization: %v %v", got, err)
	}
	if got, err := Resolve[*fhir.Location](page, &service.Location[0], service); err != nil || got != store {
		t.Errorf("location: %v %v", got, err)
	}
}

func TestResolveFindsItWithoutFromAsTheOneEntryWhoseAddressEndsInThatTypeAndID(t *testing.T) {
	page, _, organization, _ := pharmacyPage()
	if got, err := Resolve[*fhir.Organization](page, ref("Organization/Organization-1234567893"), nil); err != nil || got != organization {
		t.Errorf("%v %v", got, err)
	}
	// The control: an id the page does not hold.
	if got, err := Resolve[*fhir.Organization](page, ref("Organization/Organization-not-on-this-page"), nil); err != nil || got != nil {
		t.Errorf("%v %v", got, err)
	}
}

func TestResolveRefusesToChooseBetweenTwoAddressesEndingInTheSameTypeAndIDUnlessFromSaysWhich(t *testing.T) {
	page, service, organization, _ := pharmacyPage()
	elsewhere := &fhir.Organization{ID: organization.ID, Name: new("ANOTHER SERVER'S COPY")}
	page.Entry = append(page.Entry, entryAt("https://fhir.example.org/r4/Organization/"+*organization.ID, elsewhere, ""))
	_, err := Resolve[fhir.Resource](page, service.ProvidedBy, nil)
	notSentWith(t, err, "ambiguous")
	if got, err := Resolve[*fhir.Organization](page, service.ProvidedBy, service); err != nil || got != organization {
		t.Errorf("with from: %v %v", got, err)
	}
}

func TestResolveMatchesAnAbsoluteReferenceToTheEntryWithThatFullURLAURNUUIDAmongThem(t *testing.T) {
	header := &fhir.MessageHeader{Focus: []fhir.Reference{*ref("urn:uuid:5f0c4a52-0d1e-4b7a-9c2f-3e4d5a6b7c8d")}}
	claim := &fhir.Claim{ID: new("c")}
	message := &fhir.Bundle{Type: new("message"), Entry: []fhir.BundleEntry{
		entryAt("urn:uuid:1a2b3c4d-0000-4000-8000-000000000001", header, ""),
		entryAt("urn:uuid:5f0c4a52-0d1e-4b7a-9c2f-3e4d5a6b7c8d", claim, ""),
	}}
	if got, err := Resolve[*fhir.Claim](message, &header.Focus[0], nil); err != nil || got != claim {
		t.Errorf("urn:uuid: %v %v", got, err)
	}
	page, _, _, store := pharmacyPage()
	if got, err := Resolve[*fhir.Location](page, ref(apiFHIRBase+"/Location/"+*store.ID), nil); err != nil || got != store {
		t.Errorf("RESTful: %v %v", got, err)
	}
	if got, err := Resolve[fhir.Resource](message, ref("urn:uuid:00000000-0000-4000-8000-00000000ffff"), nil); err != nil || got != nil {
		t.Errorf("an address the Bundle does not hold: %v %v", got, err)
	}
}

func TestResolveGivesARelativeReferenceNoMeaningFromAnEntryAtAURNUUIDAsFHIRSays(t *testing.T) {
	page, _, organization, _ := pharmacyPage()
	holder := &fhir.Claim{Provider: ref("Organization/Organization-1234567893")}
	page.Entry = append(page.Entry, entryAt("urn:uuid:1a2b3c4d-0000-4000-8000-000000000002", holder, ""))
	if got, err := Resolve[fhir.Resource](page, holder.Provider, holder); err != nil || got != nil {
		t.Errorf("from an entry at a urn:uuid: %v %v", got, err)
	}
	// The control: the same reference without from finds the one RESTful entry.
	if got, err := Resolve[*fhir.Organization](page, holder.Provider, nil); err != nil || got != organization {
		t.Errorf("without from: %v %v", got, err)
	}
}

func TestResolveRefusesAFromThatIsNotAnEntryOfTheBundle(t *testing.T) {
	page, service, _, _ := pharmacyPage()
	copied := *service
	_, err := Resolve[fhir.Resource](page, service.ProvidedBy, &copied)
	notSentWith(t, err, "not an entry of this Bundle")
	// A resource held as a value has no identity: it is no entry, and comparing it never panics.
	_, err = Resolve[fhir.Resource](page, service.ProvidedBy, copied)
	notSentWith(t, err, "not an entry of this Bundle")
	valued := &fhir.Bundle{Entry: []fhir.BundleEntry{entryAt(apiFHIRBase+"/Organization/o", fhir.Organization{ID: new("o")}, "")}}
	if got, err := Resolve[fhir.Resource](valued, ref("Organization/o"), nil); err != nil || got == nil {
		t.Errorf("a resource held as a value, found by address: %v %v", got, err)
	}
	_, err = Resolve[fhir.Resource](valued, ref("Organization/o"), valued.Entry[0].Resource)
	notSentWith(t, err, "not an entry of this Bundle")
}

func TestResolveFindsAContainedResourceInFromAndRefusesToGuessWithoutIt(t *testing.T) {
	pharmacy := &fhir.Organization{ID: new("pharmacy")}
	holder := &fhir.Claim{Contained: []fhir.Resource{pharmacy}, Provider: ref("#pharmacy")}
	bundle := &fhir.Bundle{Type: new("collection"), Entry: []fhir.BundleEntry{entryAt("", holder, "")}}
	if got, err := Resolve[*fhir.Organization](bundle, holder.Provider, holder); err != nil || got != pharmacy {
		t.Errorf("#pharmacy: %v %v", got, err)
	}
	if got, err := Resolve[*fhir.Claim](bundle, ref("#"), holder); err != nil || got != holder {
		t.Errorf("#: %v %v", got, err)
	}
	if got, err := Resolve[fhir.Resource](bundle, ref("#elsewhere"), holder); err != nil || got != nil {
		t.Errorf("#elsewhere: %v %v", got, err)
	}
	_, err := Resolve[fhir.Resource](bundle, holder.Provider, nil)
	notSentWith(t, err, "pass from")
}

func TestResolveMatchesAVersionToMetaVersionID(t *testing.T) {
	v1 := &fhir.Organization{ID: new("o"), Meta: &fhir.Meta{VersionID: new("1")}}
	v2 := &fhir.Organization{ID: new("o"), Meta: &fhir.Meta{VersionID: new("2")}}
	history := &fhir.Bundle{Type: new("history"), Entry: []fhir.BundleEntry{
		entryAt(apiFHIRBase+"/Organization/o", v2, ""),
		entryAt(apiFHIRBase+"/Organization/o", v1, ""),
		// A deleted version holds no resource, and names nothing.
		{FullURL: new(apiFHIRBase + "/Organization/o"), Request: &fhir.BundleRequest{Method: new("DELETE"), URL: new("Organization/o")}},
	}}
	if got, err := Resolve[*fhir.Organization](history, ref(apiFHIRBase+"/Organization/o/_history/1"), nil); err != nil || got != v1 {
		t.Errorf("version 1: %v %v", got, err)
	}
	if got, err := Resolve[*fhir.Organization](history, ref("Organization/o/_history/2"), nil); err != nil || got != v2 {
		t.Errorf("version 2: %v %v", got, err)
	}
	if got, err := Resolve[*fhir.Organization](history, ref("Organization/o/_history/3"), nil); err != nil || got != nil {
		t.Errorf("version 3: %v %v", got, err)
	}
	// The control: with no version, both versions answer, and which is meant is ambiguous.
	_, err := Resolve[*fhir.Organization](history, ref(apiFHIRBase+"/Organization/o"), nil)
	notSentWith(t, err, "ambiguous")
}

func TestResolveRefusesAResourceOfAnotherTypeThanTheOneAskedFor(t *testing.T) {
	page, service, _, _ := pharmacyPage()
	_, err := Resolve[*fhir.Location](page, service.ProvidedBy, nil)
	notSentWith(t, err, "The reference names a resource of type Organization, not Location.")
	// The control: fhir.Resource takes any type.
	if got, err := Resolve[fhir.Resource](page, service.ProvidedBy, nil); err != nil || got == nil || got.ResourceType() != "Organization" {
		t.Errorf("%v %v", got, err)
	}
}

func TestResolveIsNilForNoReferenceAnIdentifierAloneAndAReferenceFHIRGivesNoMeaning(t *testing.T) {
	page, _, _, _ := pharmacyPage()
	for name, reference := range map[string]*fhir.Reference{
		"nil":             nil,
		"no reference":    {},
		"identifier only": {Identifier: &fhir.Identifier{System: new("http://hl7.org/fhir/sid/us-npi"), Value: new("1234567893")}},
		"no meaning":      ref("not a reference"),
	} {
		if got, err := Resolve[fhir.Resource](page, reference, nil); err != nil || got != nil {
			t.Errorf("%s: %v %v", name, got, err)
		}
	}
	if got, err := Resolve[fhir.Resource](nil, ref("Organization/o"), nil); err != nil || got != nil {
		t.Errorf("a nil Bundle: %v %v", got, err)
	}
}

func TestResolveNeverNamesTheReferenceInAnErrorSoAMessageIsSafeToLog(t *testing.T) {
	page, service, organization, _ := pharmacyPage()
	page.Entry = append(page.Entry, entryAt("https://fhir.example.org/r4/Organization/"+*organization.ID, &fhir.Organization{ID: organization.ID}, ""))
	_, ambiguous := Resolve[fhir.Resource](page, service.ProvidedBy, nil)
	_, wrongType := Resolve[*fhir.Location](page, service.ProvidedBy, service)
	_, noFrom := Resolve[fhir.Resource](page, ref("#Organization-1234567893"), nil)
	_, notAnEntry := Resolve[fhir.Resource](page, service.ProvidedBy, &fhir.HealthcareService{})
	for _, err := range []error{ambiguous, wrongType, noFrom, notAnEntry} {
		if err == nil {
			t.Fatal("no error")
		}
		for _, form := range []string{err.Error(), asError(t, err).GoString()} {
			if strings.Contains(form, "1234567893") {
				t.Errorf("an error names the reference: %s", form)
			}
		}
	}
}

func TestResourcesSharesMatchesRuleForAMatch(t *testing.T) {
	page, service, _, _ := pharmacyPage()
	c := testClient(t, record())
	var got []fhir.Resource
	for r, err := range c.Resources(t.Context(), page) {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if !slices.Equal(got, Matches[fhir.Resource](page)) || len(got) != 1 || got[0] != fhir.Resource(service) {
		t.Errorf("Resources gave %v", got)
	}
}
