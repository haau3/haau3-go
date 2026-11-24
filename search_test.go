package haau3

import (
	"context"
	"os"
	"testing"
)

// EscapeSearchValue: a value a person typed, with FHIR's special characters escaped.

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
