package haau3

import "strings"

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
// `system|code` token: escaping would make it one literal value.
func EscapeSearchValue(value string) string {
	return searchEscaper.Replace(value)
}
