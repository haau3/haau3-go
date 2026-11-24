package haau3

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/haau3/haau3-go/fhir"
	"github.com/haau3/haau3-go/internal/includes"
)

// The slot and schedule views: each matched Slot or Schedule with the resources the page holds for
// it, side by side as FHIR resources, read with FHIR's reference rules and no request; and the two
// search helpers, which send every include their search serves and nothing else.

const (
	sslURL = "http://hl7.org/fhir/uv/smart-scheduling-links/StructureDefinition/"
	// earlierURL is SMART's own registry, where the extensions were published before the HL7 guide.
	earlierURL   = "http://fhir-registry.smarthealthit.org/StructureDefinition/"
	physicalType = "http://terminology.hl7.org/CodeSystem/location-physical-type"
	accepting    = "http://terminology.hl7.org/CodeSystem/accepting-patients"
)

// searchPage is a search page: the matches, then what the includes brought, each at its address on
// the base.
func searchPage(matches []fhir.Resource, included ...fhir.Resource) *fhir.Bundle {
	page := &fhir.Bundle{Type: new("searchset"), Total: new(len(matches))}
	for _, group := range []struct {
		resources []fhir.Resource
		mode      string
	}{{matches, "match"}, {included, "include"}} {
		for _, r := range group.resources {
			page.Entry = append(page.Entry, entryAt(apiFHIRBase+"/"+r.ResourceType()+"/"+idOf(r), r, group.mode))
		}
	}
	return page
}

func viewLocation(id string, coding ...fhir.Coding) *fhir.Location {
	l := &fhir.Location{ID: new(id), Name: new("Place " + id)}
	if len(coding) > 0 {
		l.PhysicalType = &fhir.CodeableConcept{Coding: coding}
	}
	return l
}

func viewPractitioner(id string) *fhir.Practitioner {
	return &fhir.Practitioner{ID: new(id), Name: []fhir.HumanName{{Text: new("Clinician " + id)}}}
}

func viewRole(id, practitionerID string) *fhir.PractitionerRole {
	return &fhir.PractitionerRole{ID: new(id), Practitioner: ref("Practitioner/" + practitionerID)}
}

func viewService(id string, locationIDs ...string) *fhir.HealthcareService {
	s := &fhir.HealthcareService{ID: new(id)}
	for _, l := range locationIDs {
		s.Location = append(s.Location, *ref("Location/" + l))
	}
	return s
}

func viewSchedule(id string, actors ...string) *fhir.Schedule {
	s := &fhir.Schedule{ID: new(id)}
	for _, a := range actors {
		s.Actor = append(s.Actor, *ref(a))
	}
	return s
}

func viewSlot(id, scheduleID string, extension ...fhir.Extension) *fhir.Slot {
	return &fhir.Slot{
		ID: new(id), Schedule: ref("Schedule/" + scheduleID), Status: new("free"),
		Start: new("2027-01-04T09:00:00-05:00"), End: new("2027-01-04T09:30:00-05:00"), Extension: extension,
	}
}

func resources(rs ...fhir.Resource) []fhir.Resource { return rs }

func ids[T fhir.Resource](list []T) []string {
	out := []string{}
	for _, r := range list {
		out = append(out, idOf(r))
	}
	return out
}

// exampleAnswer is the spec's example answer for an operation, as the SDK decodes it.
func exampleAnswer(t *testing.T, operationID string) *fhir.Bundle {
	t.Helper()
	for _, ex := range specExamples {
		if ex.id == operationID {
			var bundle fhir.Bundle
			if err := json.Unmarshal([]byte(ex.answer), &bundle); err != nil {
				t.Fatal(err)
			}
			return &bundle
		}
	}
	t.Fatalf("no example answer for %s", operationID)
	return nil
}

func mustSlotViews(t *testing.T, page *fhir.Bundle) []SlotView {
	t.Helper()
	views, err := SlotViews(page)
	if err != nil {
		t.Fatal(err)
	}
	return views
}

func mustScheduleViews(t *testing.T, page *fhir.Bundle) []ScheduleView {
	t.Helper()
	views, err := ScheduleViews(page)
	if err != nil {
		t.Fatal(err)
	}
	return views
}

func heldIn(page *fhir.Bundle, r fhir.Resource) bool {
	return slices.ContainsFunc(page.Entry, func(e fhir.BundleEntry) bool { return sameResource(e.Resource, r) })
}

// The spec's own example answers.

func TestTheSlotSearchsExampleGivesOneSlotWithItsScheduleLocationRoleAndPractitioner(t *testing.T) {
	answer := exampleAnswer(t, "searchSlot")
	views := mustSlotViews(t, answer)
	if len(views) != 1 {
		t.Fatalf("%d views", len(views))
	}
	v := views[0]
	if deref(v.Slot.Schedule.Reference) != "Schedule/"+deref(v.Schedule.ID) {
		t.Errorf("the Slot's Schedule: %s, %s", deref(v.Slot.Schedule.Reference), deref(v.Schedule.ID))
	}
	if len(v.Locations) != 1 || deref(v.Locations[0].Name) != "haau3 Cambridge Imaging Center" {
		t.Errorf("locations: %v", ids(v.Locations))
	}
	if len(v.PractitionerRoles) != 1 || len(v.Practitioners) != 1 || deref(v.Practitioners[0].Name[0].Text) != "Dr. Wei Example" {
		t.Errorf("provider: %v %v", ids(v.PractitionerRoles), ids(v.Practitioners))
	}
	if len(v.HealthcareServices) != 0 {
		t.Errorf("services: %v", ids(v.HealthcareServices))
	}
	// The guide's own `ph` is not `vi`, so the place is physical.
	if v.Virtual {
		t.Error("virtual")
	}
	if v.PatientType != AcceptingExistingPatients || !strings.HasPrefix(v.BookingLink, "https://docs.haau3.com/") || v.BookingPhone != "+1-617-555-0100" {
		t.Errorf("carries %q %q %q", v.PatientType, v.BookingLink, v.BookingPhone)
	}
	// Each is the resource the page holds, not a copy.
	all := []fhir.Resource{v.Slot, v.Schedule}
	for _, l := range v.Locations {
		all = append(all, l)
	}
	for _, r := range v.PractitionerRoles {
		all = append(all, r)
	}
	for _, p := range v.Practitioners {
		all = append(all, p)
	}
	for _, r := range all {
		if !heldIn(answer, r) {
			t.Errorf("%s is not the page's own", r.ResourceType())
		}
	}
}

func TestTheScheduleSearchsExampleGivesOneScheduleWithItsLocationRoleAndPractitioner(t *testing.T) {
	views := mustScheduleViews(t, exampleAnswer(t, "searchSchedule"))
	if len(views) != 1 {
		t.Fatalf("%d views", len(views))
	}
	v := views[0]
	if len(v.Locations) != 1 || deref(v.Locations[0].Name) != "haau3 Cambridge Imaging Center" {
		t.Errorf("locations: %v", ids(v.Locations))
	}
	if len(v.Practitioners) != 1 || deref(v.Practitioners[0].Name[0].Text) != "Dr. Wei Example" || v.Virtual {
		t.Errorf("practitioners %v, virtual %v", ids(v.Practitioners), v.Virtual)
	}
}

// What a Schedule reaches.

func TestAScheduleTakesItsLocationActorsThenEachServicesLocationsEachOnceWhateverOrderItNamesThemIn(t *testing.T) {
	s := viewSchedule("s", "HealthcareService/svc", "Location/a")
	v := mustScheduleViews(t, searchPage(resources(s), viewLocation("a"), viewService("svc", "b", "a"), viewLocation("b")))[0]
	if got := ids(v.Locations); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("locations %v", got)
	}
	if got := ids(v.HealthcareServices); !slices.Equal(got, []string{"svc"}) {
		t.Errorf("services %v", got)
	}
}

func TestAScheduleLeavesOutAnActorOfATypeTheViewDoesNotListHeldInThePage(t *testing.T) {
	v := mustScheduleViews(t, searchPage(resources(viewSchedule("s", "Device/d", "Location/a")), &fhir.Device{ID: new("d")}, viewLocation("a")))[0]
	if got := ids(v.Locations); !slices.Equal(got, []string{"a"}) {
		t.Errorf("locations %v", got)
	}
	if len(v.PractitionerRoles)+len(v.Practitioners)+len(v.HealthcareServices) != 0 {
		t.Errorf("a Device became a provider: %+v", v)
	}
}

func TestAScheduleLeavesOutATargetOfAnotherTypeThanItsElementAllowsAndKeepsTheRestOfThePage(t *testing.T) {
	r := &fhir.PractitionerRole{ID: new("r"), Practitioner: ref("Organization/o")}
	views := mustScheduleViews(t, searchPage(resources(viewSchedule("s", "PractitionerRole/r", "Location/a"), viewSchedule("t", "Location/a")),
		r, &fhir.Organization{ID: new("o")}, viewLocation("a")))
	if got := []string{deref(views[0].Schedule.ID), deref(views[1].Schedule.ID)}; !slices.Equal(got, []string{"s", "t"}) {
		t.Errorf("schedules %v", got)
	}
	if got := ids(views[0].PractitionerRoles); !slices.Equal(got, []string{"r"}) || len(views[0].Practitioners) != 0 {
		t.Errorf("roles %v, practitioners %v", got, ids(views[0].Practitioners))
	}
}

func TestAScheduleResolvesARelativeReferenceAgainstTheBaseOfTheEntryThatHoldsIt(t *testing.T) {
	const other = "https://other.example/fhir"
	onAPI, onOther := viewLocation("a"), viewLocation("a")
	onAPI.Name, onOther.Name = new("on the API's base"), new("on the other base")
	page := &fhir.Bundle{Type: new("searchset"), Entry: []fhir.BundleEntry{
		entryAt(other+"/Schedule/s", viewSchedule("s", "Location/a"), "match"),
		entryAt(apiFHIRBase+"/Location/a", onAPI, "include"),
		entryAt(other+"/Location/a", onOther, "include"),
	}}
	if got := mustScheduleViews(t, page)[0].Locations; len(got) != 1 || got[0] != onOther {
		t.Errorf("locations %v", ids(got))
	}
}

func TestAScheduleDoesNotFollowAPractitionerRolesOwnLocations(t *testing.T) {
	r := viewRole("r", "p")
	r.Location = []fhir.Reference{*ref("Location/elsewhere")}
	v := mustScheduleViews(t, searchPage(resources(viewSchedule("s", "PractitionerRole/r")), r, viewPractitioner("p"), viewLocation("elsewhere")))[0]
	if len(v.Locations) != 0 || !slices.Equal(ids(v.Practitioners), []string{"p"}) {
		t.Errorf("locations %v, practitioners %v", ids(v.Locations), ids(v.Practitioners))
	}
}

func TestAScheduleThatNamesNoProviderGivesEmptyLists(t *testing.T) {
	v := mustScheduleViews(t, searchPage(resources(viewSchedule("s", "Location/a")), viewLocation("a")))[0]
	if !slices.Equal(ids(v.Locations), []string{"a"}) {
		t.Errorf("locations %v", ids(v.Locations))
	}
	// Empty, never nil, so a view marshals each list as [] as the TypeScript SDK's does.
	if v.PractitionerRoles == nil || v.Practitioners == nil || v.HealthcareServices == nil ||
		len(v.PractitionerRoles)+len(v.Practitioners)+len(v.HealthcareServices) != 0 {
		t.Errorf("%+v", v)
	}
	out, err := json.Marshal(v)
	if err != nil || !strings.Contains(string(out), `"practitionerRoles":[]`) || !strings.Contains(string(out), `"virtual":false`) {
		t.Errorf("%s %v", out, err)
	}
}

func TestAScheduleListsAPractitionerActorFirstThenTheOneBehindEachRoleAndOneNamedTwiceOnce(t *testing.T) {
	s := viewSchedule("s", "PractitionerRole/r", "Practitioner/q", "Practitioner/p")
	v := mustScheduleViews(t, searchPage(resources(s), viewRole("r", "p"), viewPractitioner("p"), viewPractitioner("q")))[0]
	if got := ids(v.Practitioners); !slices.Equal(got, []string{"q", "p"}) {
		t.Errorf("practitioners %v", got)
	}
}

func TestAScheduleLeavesOutAResourceThePageDoesNotHoldAndAsksTheAPIForNothing(t *testing.T) {
	rec := record()
	_ = testClient(t, rec)
	v := mustScheduleViews(t, searchPage(resources(viewSchedule("s", "Location/a", "PractitionerRole/gone", "Device/d")), viewLocation("a")))[0]
	if !slices.Equal(ids(v.Locations), []string{"a"}) || len(v.PractitionerRoles) != 0 {
		t.Errorf("locations %v, roles %v", ids(v.Locations), ids(v.PractitionerRoles))
	}
	if rec.count() != 0 {
		t.Errorf("%d requests", rec.count())
	}
}

func TestTheViewsReturnAnErrorWhenThePageHoldsTwoEntriesAReferenceCouldNameAsResolveDoes(t *testing.T) {
	twice := &fhir.Bundle{Type: new("searchset"), Entry: []fhir.BundleEntry{
		entryAt(apiFHIRBase+"/Schedule/s", viewSchedule("s", "Location/a"), "match"),
		entryAt(apiFHIRBase+"/Location/a", viewLocation("a"), "include"),
		entryAt(apiFHIRBase+"/Location/a", viewLocation("a"), "include"),
	}}
	_, err := ScheduleViews(twice)
	notSentWith(t, err, "matches 2 entries")
	slotPage := &fhir.Bundle{Type: new("searchset"), Entry: append([]fhir.BundleEntry{entryAt(apiFHIRBase+"/Slot/x", viewSlot("x", "s"), "match")}, twice.Entry...)}
	slotPage.Entry[1].Search.Mode = new("include")
	_, err = SlotViews(slotPage)
	notSentWith(t, err, "matches 2 entries")
}

// Contained resources.

func TestContainedResourcesResolveFromTheEntryThatContainsThemAndWhatTheyNameToo(t *testing.T) {
	role := &fhir.PractitionerRole{ID: new("r"), Practitioner: ref("Practitioner/p")}
	service := &fhir.HealthcareService{ID: new("h"), Location: []fhir.Reference{*ref("Location/b")}}
	s := viewSchedule("s", "#r", "#h", "Location/a")
	s.Contained = []fhir.Resource{role, service}
	v := mustScheduleViews(t, searchPage(resources(s), viewLocation("a"), viewLocation("b"), viewPractitioner("p")))[0]
	if len(v.PractitionerRoles) != 1 || v.PractitionerRoles[0] != role || len(v.HealthcareServices) != 1 || v.HealthcareServices[0] != service {
		t.Errorf("roles %v, services %v", ids(v.PractitionerRoles), ids(v.HealthcareServices))
	}
	if !slices.Equal(ids(v.Practitioners), []string{"p"}) || !slices.Equal(ids(v.Locations), []string{"a", "b"}) {
		t.Errorf("practitioners %v, locations %v", ids(v.Practitioners), ids(v.Locations))
	}
}

func TestASlotsContainedScheduleIsItsScheduleAndWhatThatScheduleNamesToo(t *testing.T) {
	contained := viewSchedule("s", "Location/a")
	x := viewSlot("x", "s")
	x.Schedule, x.Contained = ref("#s"), []fhir.Resource{contained}
	v := mustSlotViews(t, searchPage(resources(x), viewLocation("a")))[0]
	if v.Schedule != contained || !slices.Equal(ids(v.Locations), []string{"a"}) {
		t.Errorf("schedule %v, locations %v", v.Schedule, ids(v.Locations))
	}
}

// Go holds resources as pointers; one held as a value has no identity, and the views never panic on
// it.
func TestAResourceHeldAsAValueIsReadWithoutPanicking(t *testing.T) {
	page := &fhir.Bundle{Type: new("searchset"), Entry: []fhir.BundleEntry{
		entryAt(apiFHIRBase+"/Schedule/s", viewSchedule("s", "Location/a", "Practitioner/p"), "match"),
		entryAt(apiFHIRBase+"/Location/a", *viewLocation("a"), "include"),
		entryAt(apiFHIRBase+"/Practitioner/p", viewPractitioner("p"), "include"),
	}}
	v := mustScheduleViews(t, page)[0]
	if len(v.Locations) != 0 || !slices.Equal(ids(v.Practitioners), []string{"p"}) {
		t.Errorf("locations %v, practitioners %v", ids(v.Locations), ids(v.Practitioners))
	}
}

// Whether the place is virtual.

func virtualOf(t *testing.T, locations ...*fhir.Location) bool {
	t.Helper()
	var actors []string
	var included []fhir.Resource
	for _, l := range locations {
		actors = append(actors, "Location/"+deref(l.ID))
		included = append(included, l)
	}
	return mustScheduleViews(t, searchPage(resources(viewSchedule("s", actors...)), included...))[0].Virtual
}

func TestThePlaceIsVirtualWhenAnyLocationIsViAndPhysicalOtherwise(t *testing.T) {
	vi := fhir.Coding{System: new(physicalType), Code: new("vi")}
	bu := fhir.Coding{System: new(physicalType), Code: new("bu")}
	if !virtualOf(t, viewLocation("a", vi)) || !virtualOf(t, viewLocation("a", bu), viewLocation("b", vi)) {
		t.Error("a vi Location is not virtual")
	}
	if virtualOf(t, viewLocation("a", bu)) {
		t.Error("another type is virtual")
	}
	// A Location is physical unless it says it is virtual.
	if virtualOf(t, viewLocation("a")) || virtualOf(t) {
		t.Error("no type stated, or no Location, is virtual")
	}
}

func TestThePlaceIsVirtualOnlyForViInHL7sPhysicalTypeCodeSystem(t *testing.T) {
	if virtualOf(t, viewLocation("a", fhir.Coding{System: new("http://example.org/other"), Code: new("vi")})) {
		t.Error("vi in another code system is virtual")
	}
}

// What a Slot carries.

func viewOf(t *testing.T, extension ...fhir.Extension) SlotView {
	t.Helper()
	return mustSlotViews(t, searchPage(resources(viewSlot("x", "s", extension...)), viewSchedule("s")))[0]
}

func patientType(system, code string) fhir.Extension {
	return fhir.Extension{URL: new(sslURL + "patient-type"), ValueCoding: &fhir.Coding{System: new(system), Code: new(code)}}
}

func TestASlotCarriesItsBookingLinkPhoneAndPatientTypeFromSMARTSchedulingLinks100(t *testing.T) {
	v := viewOf(t,
		fhir.Extension{URL: new(sslURL + "booking-deep-link"), ValueURL: new("https://book.example/x")},
		fhir.Extension{URL: new(sslURL + "booking-phone"), ValueContactPoint: &fhir.ContactPoint{System: new("phone"), Value: new("+1-555-0100")}},
		patientType(accepting, "newpt"),
	)
	if v.BookingLink != "https://book.example/x" || v.BookingPhone != "+1-555-0100" || v.PatientType != AcceptingNewPatients {
		t.Errorf("%q %q %q", v.BookingLink, v.BookingPhone, v.PatientType)
	}
}

func TestASlotCarriesNoneUnderSMARTsEarlierRegistryURLs(t *testing.T) {
	v := viewOf(t,
		fhir.Extension{URL: new(earlierURL + "booking-deep-link"), ValueURL: new("https://book.example/x")},
		fhir.Extension{URL: new(earlierURL + "booking-phone"), ValueString: new("+1-555-0100")},
	)
	if v.BookingLink != "" || v.BookingPhone != "" {
		t.Errorf("%q %q", v.BookingLink, v.BookingPhone)
	}
	// The resource still carries both, as the API served it.
	if len(v.Slot.Extension) != 2 {
		t.Errorf("%d extensions", len(v.Slot.Extension))
	}
}

func TestASlotCarriesNoPatientTypeFromAnotherCodeSystemNorACodeHL7sDoesNotHave(t *testing.T) {
	if v := viewOf(t, patientType("http://example.org/other", "newpt")); v.PatientType != "" {
		t.Errorf("another code system: %q", v.PatientType)
	}
	if v := viewOf(t, patientType(accepting, "somept")); v.PatientType != "" {
		t.Errorf("an unknown code: %q", v.PatientType)
	}
	// Control: each of HL7's four is read, nopt included.
	for _, code := range []PatientType{AcceptingNewPatients, AcceptingExistingPatients, AcceptingExistingPatientsAndFamilies, NotAcceptingPatients} {
		if v := viewOf(t, patientType(accepting, string(code))); v.PatientType != code {
			t.Errorf("%s: %q", code, v.PatientType)
		}
	}
}

func TestASlotReadsTheFirstExtensionOfItsURLThatCarriesTheValueItIsReadFor(t *testing.T) {
	v := viewOf(t,
		fhir.Extension{URL: new(sslURL + "booking-deep-link"), ValueString: new("not a url value")},
		fhir.Extension{URL: new(sslURL + "booking-deep-link"), ValueURL: new("https://book.example/second")},
		fhir.Extension{URL: new(sslURL + "booking-phone"), ValueString: new("+1-555-0199")},
		fhir.Extension{URL: new(sslURL + "booking-phone"), ValueContactPoint: &fhir.ContactPoint{System: new("phone"), Value: new("+1-555-0100")}},
		patientType("http://example.org/other", "newpt"),
		patientType(accepting, "existptfam"),
	)
	if v.BookingLink != "https://book.example/second" || v.BookingPhone != "+1-555-0100" || v.PatientType != AcceptingExistingPatientsAndFamilies {
		t.Errorf("%q %q %q", v.BookingLink, v.BookingPhone, v.PatientType)
	}
}

func TestASlotWithNoExtensionsCarriesNone(t *testing.T) {
	v := viewOf(t)
	if v.BookingLink != "" || v.BookingPhone != "" || v.PatientType != "" {
		t.Errorf("%q %q %q", v.BookingLink, v.BookingPhone, v.PatientType)
	}
	out, err := json.Marshal(v)
	if err != nil || strings.Contains(string(out), "bookingLink") || !strings.Contains(string(out), `"slot":`) || !strings.Contains(string(out), `"schedule":`) {
		t.Errorf("%s %v", out, err)
	}
}

// Which entries are views.

func TestTheViewsAreOnlyMatchesOfTheirOwnTypeNeverAnIncludedEntryOrAnotherType(t *testing.T) {
	s := viewSchedule("s", "Location/a")
	page := searchPage(resources(viewSlot("x", "s"), viewLocation("m")), s, viewLocation("a"), viewSlot("included", "s"))
	slots := mustSlotViews(t, page)
	if len(slots) != 1 || deref(slots[0].Slot.ID) != "x" {
		t.Errorf("slot views %v", slots)
	}
	if got := mustScheduleViews(t, page); len(got) != 0 {
		t.Errorf("schedule views %v", got)
	}
}

func TestTheViewsOfAnEmptyPageAreNone(t *testing.T) {
	for _, page := range []*fhir.Bundle{{Type: new("searchset")}, nil} {
		slots, err := SlotViews(page)
		schedules, err2 := ScheduleViews(page)
		if err != nil || err2 != nil || slots == nil || schedules == nil || len(slots)+len(schedules) != 0 {
			t.Errorf("%v %v %v %v", slots, schedules, err, err2)
		}
	}
}

func TestSlotViewsNamesTheFixWhenASlotsScheduleIsNotInThePage(t *testing.T) {
	_, err := SlotViews(searchPage(resources(viewSlot("x", "s"))))
	notSentWith(t, err, "search with SearchSlotWithIncludes, or send _include=Slot:schedule")
	// A Slot whose schedule names a resource of another type holds no Schedule either.
	elsewhere := viewSlot("x", "s")
	elsewhere.Schedule = ref("Location/a")
	_, err = SlotViews(searchPage(resources(elsewhere), viewLocation("a")))
	notSentWith(t, err, "names a Schedule the page does not hold")
	// Control: the same page with its Schedule included gives its view.
	if got := mustSlotViews(t, searchPage(resources(viewSlot("x", "s")), viewSchedule("s"))); len(got) != 1 {
		t.Errorf("%d views", len(got))
	}
}

// The search helpers.

func helperQuery(t *testing.T, rec *recorder) map[string][]string {
	t.Helper()
	return map[string][]string(rec.last().URL.Query())
}

var emptySearch = map[string]any{"resourceType": "Bundle", "type": "searchset", "entry": []any{}}

func TestSearchSlotWithIncludesSendsTheCallersParametersAndEveryIncludeAndNothingElse(t *testing.T) {
	rec := record(fhirReply(200, emptySearch))
	if _, err := testClient(t, rec).SearchSlotWithIncludes(t.Context(), SearchSlotWithIncludesParams{LocationAddressState: new("MA"), Count: new(10)}); err != nil {
		t.Fatal(err)
	}
	if rec.last().URL.Path != "/v1/fhir/r4/Slot" {
		t.Errorf("path %s", rec.last().URL.Path)
	}
	want := url.Values{
		"location-address-state": {"MA"}, "_count": {"10"}, "_include": {"Slot:schedule"},
		"_include:iterate": {"Schedule:actor", "PractitionerRole:practitioner", "HealthcareService:location"},
	}
	if got := helperQuery(t, rec); !reflect.DeepEqual(got, map[string][]string(want)) {
		t.Errorf("sent %v", got)
	}
}

func TestSearchScheduleWithIncludesDoesTheSameForTheScheduleSearch(t *testing.T) {
	rec := record(fhirReply(200, emptySearch))
	if _, err := testClient(t, rec).SearchScheduleWithIncludes(t.Context(), SearchScheduleWithIncludesParams{LocationAddressCity: new("Boston"), Page: new("next-page")}); err != nil {
		t.Fatal(err)
	}
	if rec.last().URL.Path != "/v1/fhir/r4/Schedule" {
		t.Errorf("path %s", rec.last().URL.Path)
	}
	want := url.Values{
		"location-address-city": {"Boston"}, "page": {"next-page"}, "_include": {"Schedule:actor"},
		"_include:iterate": {"PractitionerRole:practitioner", "HealthcareService:location"},
	}
	if got := helperQuery(t, rec); !reflect.DeepEqual(got, map[string][]string(want)) {
		t.Errorf("sent %v", got)
	}
}

func TestTheSearchHelpersPassACallsOptionsOnAsTheSearchDoes(t *testing.T) {
	rec := record(fhirReply(200, emptySearch))
	c := testClient(t, rec)
	var info ResponseInfo
	if _, err := c.SearchSlotWithIncludes(t.Context(), SearchSlotWithIncludesParams{}, WithHeader("X-Extra", "slot"), WithResponseInfo(&info)); err != nil {
		t.Fatal(err)
	}
	if rec.last().Header.Get("X-Extra") != "slot" || info.Status != 200 || info.RequestID != requestID {
		t.Errorf("header %q, info %+v", rec.last().Header.Get("X-Extra"), info)
	}
	if _, err := c.SearchScheduleWithIncludes(t.Context(), SearchScheduleWithIncludesParams{}, WithHeader("X-Extra", "schedule")); err != nil {
		t.Fatal(err)
	}
	if rec.last().Header.Get("X-Extra") != "schedule" {
		t.Errorf("header %q", rec.last().Header.Get("X-Extra"))
	}
}

// served is every include a search serves, as the pinned spec lists them.
func served(t *testing.T, operationID string) []string {
	t.Helper()
	var doc struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
			Parameters  []struct {
				Name   string `json:"name"`
				Schema struct {
					Enum  []string `json:"enum"`
					Items struct {
						Enum []string `json:"enum"`
					} `json:"items"`
				} `json:"schema"`
			} `json:"parameters"`
		} `json:"paths"`
	}
	readJSON(t, filepath.Join("openapi", "openapi.json"), &doc)
	var names []string
	for _, item := range doc.Paths {
		op, ok := item["get"]
		if !ok || op.OperationID != operationID {
			continue
		}
		for _, p := range op.Parameters {
			if p.Name == "_include" || p.Name == "_include:iterate" {
				names = append(append(names, p.Schema.Items.Enum...), p.Schema.Enum...)
			}
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// An include the API serves later fails this until the helper sends it.
func TestTheSearchHelpersSendEveryIncludeTheirSearchServesAsTheSpecListsThem(t *testing.T) {
	for id, sent := range map[string]includes.Includes{"searchSlot": includes.Slot, "searchSchedule": includes.Schedule} {
		got := slices.Sorted(slices.Values(sent.All()))
		if want := served(t, id); !slices.Equal(got, want) {
			t.Errorf("%s: the helper sends %v, the search serves %v", id, got, want)
		}
	}
	if got := served(t, "searchSlot"); len(got) != 4 {
		t.Errorf("the Slot search serves %v", got)
	}
	if !reflect.DeepEqual(includes.ByOperation, map[string]includes.Includes{"searchSlot": includes.Slot, "searchSchedule": includes.Schedule}) {
		t.Errorf("ByOperation %v", includes.ByOperation)
	}
}

// The helpers' parameters are the search's own less the two includes, so a value that holds an
// include does not compile.
func TestTheSearchHelpersParametersAreTheSearchsLessItsIncludes(t *testing.T) {
	for helper, search := range map[reflect.Type]reflect.Type{
		reflect.TypeFor[SearchSlotWithIncludesParams]():     reflect.TypeFor[SearchSlotParams](),
		reflect.TypeFor[SearchScheduleWithIncludesParams](): reflect.TypeFor[SearchScheduleParams](),
	} {
		var want []reflect.StructField
		for i := range search.NumField() {
			if f := search.Field(i); f.Name != "Include" && f.Name != "IncludeIterate" {
				want = append(want, f)
			}
		}
		if helper.NumField() != len(want) {
			t.Fatalf("%s has %d fields, %s less its includes %d", helper, helper.NumField(), search, len(want))
		}
		for i, f := range want {
			if got := helper.Field(i); got.Name != f.Name || got.Type != f.Type {
				t.Errorf("%s field %d is %s %s, not %s %s", helper, i, got.Name, got.Type, f.Name, f.Type)
			}
		}
		if _, ok := helper.FieldByName("Include"); ok {
			t.Errorf("%s takes an include", helper)
		}
		if search.ConvertibleTo(helper) {
			t.Errorf("a %s converts to %s", search, helper)
		}
	}
}

// Each field of the helper's parameters reaches the search's, and the includes are a copy, so a
// caller cannot change what the next call sends.
func TestTheSearchHelpersCopyEveryFieldAndANewListOfIncludes(t *testing.T) {
	p := SearchSlotWithIncludesParams{Status: new("busy"), Start: []string{"ge2027-01-01"}, Count: new(5), Page: new("c")}
	got := p.search(includes.Slot)
	if deref(got.Status) != "busy" || !slices.Equal(got.Start, []string{"ge2027-01-01"}) || *got.Count != 5 || deref(got.Page) != "c" {
		t.Errorf("%+v", got)
	}
	got.Include[0] = "changed"
	if includes.Slot.Include[0] != "Slot:schedule" {
		t.Error("the helper's includes are shared with a call")
	}
}
