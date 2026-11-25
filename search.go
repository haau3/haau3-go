package haau3

import (
	"fmt"
	"strings"

	"github.com/haau3/haau3-go/fhir"
)

// searchEscaper puts a backslash before each of FHIR's special characters in a search value.
var searchEscaper = strings.NewReplacer(`\`, `\\`, `,`, `\,`, `$`, `\$`, `|`, `\|`)

// EscapeSearchValue is a value for a FHIR search parameter, with FHIR's special characters escaped:
// `\`, `,`, `$` and `|` each take a `\` before them, as FHIR R4 says ("Escaping Search Parameters",
// https://hl7.org/fhir/R4/search.html#escaping), so the API reads the value as the one value it is.
// Unescaped, a comma separates values the API reads as "or" (so "Austin, TX" asks for two cities), a
// value with an empty part is refused (a trailing comma), and a `|` splits a token's system from its
// code.
//
//	city := haau3.EscapeSearchValue(typedCity)
//	page, err := client.SearchSlotWithIncludes(ctx, haau3.SearchSlotWithIncludesParams{LocationAddressCity: &city})
//
// Never escape a value you built with `,` or `|` on purpose, such as a list of codes or a
// `system|code` token: escaping would make it one literal value. Build a token with SearchToken.
func EscapeSearchValue(value string) string {
	return searchEscaper.Replace(value)
}

// SearchToken is the value a FHIR token search parameter takes for a code in its code system:
// `system|code`, with each part escaped (EscapeSearchValue), as FHIR R4 writes a token ("token",
// https://hl7.org/fhir/R4/search.html#token). Given several codings, it is any of them: their tokens
// joined by commas.
//
// An entry of a ValueSet expansion becomes a Coding with its System and Code, so a code picked from
// an expansion goes straight into a search:
//
//	chosen := serviceTypes.Expansion.Contains[0] // after checking the expansion holds one
//	token, err := haau3.SearchToken(fhir.Coding{System: chosen.System, Code: chosen.Code})
//	page, err := client.SearchSlotWithIncludes(ctx, haau3.SearchSlotWithIncludesParams{ScheduleServiceType: &token})
//
// Each coding needs both a system and a code: it returns an error of KindNotSent naming the one
// missing, since a token without its system would match the code in every system, and another when
// given no coding. For a code in any system, send EscapeSearchValue(code) instead.
func SearchToken(codings ...fhir.Coding) (string, error) {
	if len(codings) == 0 {
		return "", notSent("SearchToken needs a coding: it was given none.")
	}
	tokens := make([]string, 0, len(codings))
	for i, coding := range codings {
		which := "the coding"
		if len(codings) > 1 {
			which = fmt.Sprintf("coding %d", i+1)
		}
		if coding.System == nil || *coding.System == "" {
			return "", notSent(fmt.Sprintf("SearchToken needs a system: %s has none.", which))
		}
		if coding.Code == nil || *coding.Code == "" {
			return "", notSent(fmt.Sprintf("SearchToken needs a code: %s has none.", which))
		}
		tokens = append(tokens, EscapeSearchValue(*coding.System)+"|"+EscapeSearchValue(*coding.Code))
	}
	return strings.Join(tokens, ","), nil
}
