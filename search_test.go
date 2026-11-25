package haau3

import (
	"context"
	"os"
	"testing"

	"github.com/haau3/haau3-go/fhir"
)

// EscapeSearchValue: a value a person typed, with FHIR's special characters escaped. SearchToken: a
// coding as the value a token search parameter takes.

func TestEscapeSearchValuePutsABackslashBeforeEachSpecialCharacter(t *testing.T) {
	for in, want := range map[string]string{
		"Austin,":           `Austin\,`,
		"a|b":               `a\|b`,
		"$5":                `\$5`,
		`C:\temp`:           `C:\\temp`,
		`Austin, TX | $ \`:  `Austin\, TX \| \$ \\`,
		`\,`:                `\\\,`,
		"San José (East) 😀": "San José (East) 😀",
		"":                  "",
	} {
		if got := EscapeSearchValue(in); got != want {
			t.Errorf("EscapeSearchValue(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAnEscapedValueReachesTheAPIAsItWas(t *testing.T) {
	rec := record(fhirReply(200, map[string]any{"resourceType": "Bundle", "type": "searchset", "total": 0}))
	city := EscapeSearchValue("Austin, TX")
	if _, err := testClient(t, rec).SearchSlot(context.Background(), SearchSlotParams{LocationAddressCity: &city}); err != nil {
		t.Fatal(err)
	}
	if got := rec.last().URL.Query().Get("location-address-city"); got != `Austin\, TX` {
		t.Errorf("sent %q", got)
	}
}

// Live, on a key. Measured 2026-11-24: "Austin," unescaped is a 400 (an empty value), escaped a
// search that finds none.
func TestLiveACityWithACommaEscaped(t *testing.T) {
	if os.Getenv("HAAU3_LIVE_TEST") != "1" || os.Getenv("HAAU3_API_KEY") == "" {
		t.Skip("set HAAU3_LIVE_TEST=1 and HAAU3_API_KEY to call the production API")
	}
	if keyMode(os.Getenv("HAAU3_API_KEY")) == "test" {
		t.Skip("a test key works only in the Sandbox")
	}
	ctx := context.Background()
	client, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	escaped := EscapeSearchValue("Austin,")
	page, err := client.SearchSlot(ctx, SearchSlotParams{LocationAddressCity: &escaped, Count: new(1)})
	if err != nil || page.Total == nil || *page.Total != 0 {
		t.Fatalf("escaped: %v, %v", page, err)
	}
	raw := "Austin,"
	_, err = client.SearchSlot(ctx, SearchSlotParams{LocationAddressCity: &raw, Count: new(1)})
	if e := asError(t, err); e.Status != 400 {
		t.Errorf("unescaped answered %d, want 400", e.Status)
	}
}

const hl7ServiceType = "http://terminology.hl7.org/CodeSystem/service-type"

func coding(system, code string) fhir.Coding {
	return fhir.Coding{System: &system, Code: &code}
}

func TestSearchTokenWritesACodingAsSystemBarCode(t *testing.T) {
	display, version := "General Practice", "1"
	full := coding(hl7ServiceType, "124")
	full.Display, full.Version = &display, &version
	for _, c := range []fhir.Coding{coding(hl7ServiceType, "124"), full} {
		if got, err := SearchToken(c); err != nil || got != hl7ServiceType+"|124" {
			t.Errorf("SearchToken = %q, %v", got, err)
		}
	}
}

func TestSearchTokenTakesAnExpansionEntryAsACoding(t *testing.T) {
	system, code, display := hl7ServiceType, "124", "General Practice"
	entry := fhir.ValueSetContains{System: &system, Code: &code, Display: &display}
	if got, err := SearchToken(fhir.Coding{System: entry.System, Code: entry.Code}); err != nil || got != hl7ServiceType+"|124" {
		t.Errorf("SearchToken = %q, %v", got, err)
	}
}

func TestSearchTokenEscapesEachPartAndKeepsTheBarBetweenThem(t *testing.T) {
	if got, err := SearchToken(coding("urn:a|b", `x,y$z\w`)); err != nil || got != `urn:a\|b|x\,y\$z\\w` {
		t.Errorf("SearchToken = %q, %v", got, err)
	}
}

func TestSearchTokenIsAnyOfSeveralCodingsJoinedByCommasInOrder(t *testing.T) {
	got, err := SearchToken(coding(hl7ServiceType, "124"), coding("urn:a", "1,2"))
	if want := hl7ServiceType + `|124,urn:a|1\,2`; err != nil || got != want {
		t.Errorf("SearchToken = %q, %v; want %q", got, err, want)
	}
}

func TestSearchTokenRefusesNamingWhatIsMissing(t *testing.T) {
	empty := ""
	code := "124"
	system := hl7ServiceType
	for _, c := range []struct {
		codings []fhir.Coding
		message string
	}{
		{nil, "SearchToken needs a coding: it was given none."},
		{[]fhir.Coding{{Code: &code}}, "SearchToken needs a system: the coding has none."},
		{[]fhir.Coding{{System: &system}}, "SearchToken needs a code: the coding has none."},
		{[]fhir.Coding{{System: &empty, Code: &code}}, "SearchToken needs a system: the coding has none."},
		{[]fhir.Coding{coding(hl7ServiceType, "124"), {System: &system, Code: &empty}}, "SearchToken needs a code: coding 2 has none."},
	} {
		got, err := SearchToken(c.codings...)
		e := asError(t, err)
		if got != "" || e.Kind != KindNotSent || e.Error() != c.message {
			t.Errorf("SearchToken(%v) = %q, %v; want %q", c.codings, got, err, c.message)
		}
	}
}

func TestASearchTokenReachesTheAPIAsOneValue(t *testing.T) {
	rec := record(fhirReply(200, map[string]any{"resourceType": "Bundle", "type": "searchset", "total": 0}))
	token, err := SearchToken(coding("urn:a", "1,2"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testClient(t, rec).SearchSlot(context.Background(), SearchSlotParams{ScheduleServiceType: &token}); err != nil {
		t.Fatal(err)
	}
	if got := rec.last().URL.Query()["schedule.service-type"]; len(got) != 1 || got[0] != `urn:a|1\,2` {
		t.Errorf("sent %q", got)
	}
}

// Live, on a key: a service type picked from its value set finds slots through SearchToken.
func TestLiveSlotsFoundByAServiceTypeFromItsValueSet(t *testing.T) {
	if os.Getenv("HAAU3_LIVE_TEST") != "1" || os.Getenv("HAAU3_API_KEY") == "" {
		t.Skip("set HAAU3_LIVE_TEST=1 and HAAU3_API_KEY to call the production API")
	}
	if keyMode(os.Getenv("HAAU3_API_KEY")) == "test" {
		t.Skip("a test key works only in the Sandbox")
	}
	ctx := context.Background()
	client, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	url := "https://fhir.haau3.com/platform/ValueSet/scheduling-service-types"
	serviceTypes, err := client.ValueSetExpand(ctx, ValueSetExpandParams{URL: &url})
	if err != nil || serviceTypes.Expansion == nil || len(serviceTypes.Expansion.Contains) == 0 {
		t.Fatalf("expand: %v, %v", serviceTypes, err)
	}
	chosen := serviceTypes.Expansion.Contains[0]
	for _, entry := range serviceTypes.Expansion.Contains {
		if entry.System != nil && *entry.System == hl7ServiceType {
			chosen = entry
			break
		}
	}
	token, err := SearchToken(fhir.Coding{System: chosen.System, Code: chosen.Code})
	if err != nil {
		t.Fatal(err)
	}
	page, err := client.SearchSlot(ctx, SearchSlotParams{ScheduleServiceType: &token, Count: new(1)})
	if err != nil || page.Total == nil || *page.Total == 0 {
		t.Fatalf("search: %v, %v", page, err)
	}
}
