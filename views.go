package haau3

import (
	"context"
	"slices"

	"github.com/haau3/haau3-go/fhir"
	"github.com/haau3/haau3-go/internal/includes"
)

// Where SMART Scheduling Links 1.0.0 publishes its extensions. The views read these URLs only.
const (
	smartSchedulingLinks = "http://hl7.org/fhir/uv/smart-scheduling-links/StructureDefinition/"
	bookingDeepLinkURL   = smartSchedulingLinks + "booking-deep-link"
	bookingPhoneURL      = smartSchedulingLinks + "booking-phone"
	patientTypeURL       = smartSchedulingLinks + "patient-type"

	// acceptingPatients is HL7's code system for whom a provider accepts.
	acceptingPatients = "http://terminology.hl7.org/CodeSystem/accepting-patients"
	// locationPhysicalType is HL7's code system for a Location's physical type, in which vi is a
	// virtual location.
	locationPhysicalType = "http://terminology.hl7.org/CodeSystem/location-physical-type"
)

// PatientType is whom a Slot accepts, as a code of HL7's accepting-patients code system
// (http://terminology.hl7.org/CodeSystem/accepting-patients), each constant named from the code's
// display there. "" is none.
type PatientType string

const (
	// AcceptingNewPatients is newpt: new and existing patients.
	AcceptingNewPatients PatientType = "newpt"
	// AcceptingExistingPatients is existptonly: existing patients only.
	AcceptingExistingPatients PatientType = "existptonly"
	// AcceptingExistingPatientsAndFamilies is existptfam: existing patients and their families.
	AcceptingExistingPatientsAndFamilies PatientType = "existptfam"
	// NotAcceptingPatients is nopt: no patients. The API serves no Slot that says so.
	NotAcceptingPatients PatientType = "nopt"
)

var patientTypes = []PatientType{AcceptingNewPatients, AcceptingExistingPatients, AcceptingExistingPatientsAndFamilies, NotAcceptingPatients}

// ScheduleView is one Schedule of a search page with the resources the page holds for it, side by
// side, each the FHIR resource the API served: the Locations it reaches and its provider, however
// its publisher named it. Each list keeps the order the Schedule names them in, holds each
// resource once, and is empty when the page holds none. The resources are the page's own; nothing
// is copied or replaced.
type ScheduleView struct {
	// Schedule is the Schedule.
	Schedule *fhir.Schedule `json:"schedule"`
	// Locations is every Location the Schedule reaches: its Location actors, then the Locations of
	// each HealthcareService it names. A PractitionerRole's own locations are not followed.
	Locations []*fhir.Location `json:"locations"`
	// PractitionerRoles is the PractitionerRoles the Schedule names.
	PractitionerRoles []*fhir.PractitionerRole `json:"practitionerRoles"`
	// Practitioners is the Practitioners the Schedule names, then the Practitioner behind each of its
	// PractitionerRoles.
	Practitioners []*fhir.Practitioner `json:"practitioners"`
	// HealthcareServices is the HealthcareServices the Schedule names.
	HealthcareServices []*fhir.HealthcareService `json:"healthcareServices"`
	// Virtual says whether the place is virtual: true when any of Locations has the physical type vi
	// (http://terminology.hl7.org/CodeSystem/location-physical-type), and false otherwise. A Location
	// is physical unless it says it is virtual, whether it states another physical type or none, and
	// a place that is both is two Locations, as SMART Scheduling Links models it.
	Virtual bool `json:"virtual"`
}

// SlotView is one matched Slot of a search page with its Schedule and everything a ScheduleView
// holds, side by side as FHIR resources, and three values read from the Slot's SMART Scheduling
// Links 1.0.0 extensions.
type SlotView struct {
	// Slot is the Slot.
	Slot *fhir.Slot `json:"slot"`
	ScheduleView
	// BookingLink is the Slot's booking link: its booking-deep-link extension's URL, or "".
	BookingLink string `json:"bookingLink,omitempty"`
	// BookingPhone is the phone number to book the Slot by: its booking-phone extension's value, or "".
	BookingPhone string `json:"bookingPhone,omitempty"`
	// PatientType is whom the Slot accepts, from its patient-type extension, as a code of HL7's
	// accepting-patients code system. "" when the Slot carries none, or one from another code
	// system, or a code that system does not have.
	PatientType PatientType `json:"patientType,omitempty"`
}

// SlotViews is each Slot a search page matched, in page order, with its Schedule, Locations and
// provider, and no request. Search with SearchSlotWithIncludes so the page holds them, and page with
// Pages. References resolve by FHIR's rules for a Bundle, as Resolve does, a contained resource from
// the entry that contains it. A resource the page does not hold, or one of another type than its
// element allows, is left out, so one bad record never fails the page.
//
// It returns an *Error of KindNotSent when a Slot names a Schedule the page does not hold (every
// Slot names one, so the search was made without its includes), and when two entries could each be
// the one a reference names.
//
//	page, err := client.SearchSlotWithIncludes(ctx, haau3.SearchSlotWithIncludesParams{LocationAddressState: new("MA")})
//	if err != nil {
//		return err
//	}
//	views, err := haau3.SlotViews(page)
//	if err != nil {
//		return err
//	}
//	for _, view := range views {
//		fmt.Println(*view.Slot.Start, view.BookingLink)
//	}
func SlotViews(page *fhir.Bundle) ([]SlotView, error) {
	views := []SlotView{}
	for _, slot := range Matches[*fhir.Slot](page) {
		found, err := follow(page, slot.Schedule, slot)
		if err != nil {
			return nil, err
		}
		schedule, ok := found.(*fhir.Schedule)
		if !ok {
			return nil, notSent("A Slot in this page names a Schedule the page does not hold. Every Slot names one, and a " +
				"search with its includes holds it: search with SearchSlotWithIncludes, or send _include=Slot:schedule.")
		}
		reached, err := reach(page, schedule)
		if err != nil {
			return nil, err
		}
		views = append(views, SlotView{
			Slot:         slot,
			ScheduleView: reached,
			BookingLink:  extensionValue(slot, bookingDeepLinkURL, func(e fhir.Extension) string { return deref(e.ValueURL) }),
			BookingPhone: extensionValue(slot, bookingPhoneURL, func(e fhir.Extension) string {
				if e.ValueContactPoint == nil {
					return ""
				}
				return deref(e.ValueContactPoint.Value)
			}),
			PatientType: extensionValue(slot, patientTypeURL, func(e fhir.Extension) PatientType {
				if e.ValueCoding == nil || deref(e.ValueCoding.System) != acceptingPatients {
					return ""
				}
				if code := PatientType(deref(e.ValueCoding.Code)); slices.Contains(patientTypes, code) {
					return code
				}
				return ""
			}),
		})
	}
	return views, nil
}

// ScheduleViews is each Schedule a search page matched, in page order, with its Locations and
// provider, by the rules SlotViews follows, and no request. Search with SearchScheduleWithIncludes so
// the page holds them. It returns an *Error of KindNotSent when two entries could each be the one a
// reference names.
func ScheduleViews(page *fhir.Bundle) ([]ScheduleView, error) {
	views := []ScheduleView{}
	for _, schedule := range Matches[*fhir.Schedule](page) {
		reached, err := reach(page, schedule)
		if err != nil {
			return nil, err
		}
		views = append(views, reached)
	}
	return views, nil
}

// SearchSlotWithIncludes is SearchSlot sending every include the Slot search serves: each Slot's
// Schedule, the Locations and provider the Schedule names, the Practitioner behind a PractitionerRole
// and the Location of a HealthcareService, so SlotViews finds them in the page. It sends nothing else:
// the status, the start and the page size keep the API's defaults. Its parameters are the Slot
// search's, less the includes, so it takes none; to send your own, call SearchSlot. Options are a
// call's, as for SearchSlot.
func (c *Client) SearchSlotWithIncludes(ctx context.Context, params SearchSlotWithIncludesParams, opts ...Option) (*fhir.Bundle, error) {
	return c.SearchSlot(ctx, params.search(includes.Slot), opts...)
}

// SearchScheduleWithIncludes is SearchSchedule sending every include the Schedule search serves: the
// Locations and provider each Schedule names, the Practitioner behind a PractitionerRole and the
// Location of a HealthcareService, so ScheduleViews finds them in the page. It sends nothing else.
// Its parameters are the Schedule search's, less the includes; to send your own, call
// SearchSchedule.
func (c *Client) SearchScheduleWithIncludes(ctx context.Context, params SearchScheduleWithIncludesParams, opts ...Option) (*fhir.Bundle, error) {
	return c.SearchSchedule(ctx, params.search(includes.Schedule), opts...)
}

// follow is the resource a reference names, found from `from` by FHIR's rules for a Bundle, with no
// request, or nil. A contained resource resolves from the entry that contains it (resolveIn finds
// it), so what a contained PractitionerRole names is found from the Schedule that contains both.
func follow(page *fhir.Bundle, reference *fhir.Reference, from fhir.Resource) (fhir.Resource, error) {
	found, err := resolveIn(page, referenceTarget(reference), from)
	if err != nil || isNilResource(found) {
		return nil, err
	}
	return found, nil
}

// reach is what a Schedule names and reaches in one page, found by FHIR's reference rules, with no
// request. A target of another type than its element allows is left out.
func reach(page *fhir.Bundle, schedule *fhir.Schedule) (ScheduleView, error) {
	view := ScheduleView{
		Schedule: schedule, Locations: []*fhir.Location{}, PractitionerRoles: []*fhir.PractitionerRole{},
		Practitioners: []*fhir.Practitioner{}, HealthcareServices: []*fhir.HealthcareService{},
	}
	for i := range schedule.Actor {
		found, err := follow(page, &schedule.Actor[i], schedule)
		if err != nil {
			return view, err
		}
		switch r := found.(type) {
		case *fhir.Location:
			view.Locations = once(view.Locations, r)
		case *fhir.PractitionerRole:
			view.PractitionerRoles = once(view.PractitionerRoles, r)
		case *fhir.Practitioner:
			view.Practitioners = once(view.Practitioners, r)
		case *fhir.HealthcareService:
			view.HealthcareServices = once(view.HealthcareServices, r)
		}
	}
	for _, role := range view.PractitionerRoles {
		found, err := follow(page, role.Practitioner, role)
		if err != nil {
			return view, err
		}
		if practitioner, ok := found.(*fhir.Practitioner); ok {
			view.Practitioners = once(view.Practitioners, practitioner)
		}
	}
	for _, service := range view.HealthcareServices {
		for i := range service.Location {
			found, err := follow(page, &service.Location[i], service)
			if err != nil {
				return view, err
			}
			if location, ok := found.(*fhir.Location); ok {
				view.Locations = once(view.Locations, location)
			}
		}
	}
	view.Virtual = slices.ContainsFunc(view.Locations, isVirtual)
	return view, nil
}

func once[T comparable](list []T, item T) []T {
	if slices.Contains(list, item) {
		return list
	}
	return append(list, item)
}

// isVirtual reports whether a Location says it is virtual: its physical type is HL7's vi. Otherwise
// it is physical.
func isVirtual(location *fhir.Location) bool {
	if location.PhysicalType == nil {
		return false
	}
	return slices.ContainsFunc(location.PhysicalType.Coding, func(c fhir.Coding) bool {
		return deref(c.System) == locationPhysicalType && deref(c.Code) == "vi"
	})
}

// extensionValue is the value of the first extension under url that carries the value it is read
// for, or "".
func extensionValue[T ~string](slot *fhir.Slot, url string, read func(fhir.Extension) T) T {
	for _, e := range slot.Extension {
		if deref(e.URL) != url {
			continue
		}
		if value := read(e); value != "" {
			return value
		}
	}
	return ""
}
