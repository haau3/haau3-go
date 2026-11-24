// Package includes holds what each search helper sends as _include and _include:iterate: every
// include its search serves. The SDK sends these, and the generator checks the first call's example
// against them, so the two cannot drift apart.
package includes

// Includes is what one helper sends.
type Includes struct {
	// Include is sent as _include.
	Include []string
	// Iterate is sent as _include:iterate.
	Iterate []string
}

// All is every include a helper sends, Include then Iterate.
func (i Includes) All() []string {
	return append(append([]string(nil), i.Include...), i.Iterate...)
}

// Slot is what SearchSlotWithIncludes sends: each Slot's Schedule, the Locations and provider the
// Schedule names, the Practitioner behind a PractitionerRole and the Location of a
// HealthcareService.
var Slot = Includes{
	Include: []string{"Slot:schedule"},
	Iterate: []string{"Schedule:actor", "PractitionerRole:practitioner", "HealthcareService:location"},
}

// Schedule is what SearchScheduleWithIncludes sends: the Locations and provider each Schedule
// names, the Practitioner behind a PractitionerRole and the Location of a HealthcareService.
var Schedule = Includes{
	Include: []string{"Schedule:actor"},
	Iterate: []string{"PractitionerRole:practitioner", "HealthcareService:location"},
}

// ByOperation is each helper's includes by the operationId of the search it makes. The generator
// writes a parameters struct without the includes for each of these searches.
var ByOperation = map[string]Includes{
	"searchSlot":     Slot,
	"searchSchedule": Schedule,
}
