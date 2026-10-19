package main

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// initialisms are written in one case in Go names, as Go's own style does: `ID`, not `Id`.
var initialisms = map[string]string{
	"api": "API", "fhir": "FHIR", "html": "HTML", "http": "HTTP", "id": "ID", "json": "JSON",
	"npi": "NPI", "oid": "OID", "uri": "URI", "url": "URL", "uuid": "UUID", "xml": "XML",
}

var wordBoundary = regexp.MustCompile(`[a-z0-9][A-Z]`)

// words splits a name at `-`, `:`, `_` and each lower-to-upper boundary: `name:contains` is
// [name contains], `serviceTypeCode` is [service Type Code].
func words(name string) []string {
	spaced := wordBoundary.ReplaceAllStringFunc(name, func(pair string) string { return pair[:1] + " " + pair[1:] })
	return strings.FieldsFunc(spaced, func(r rune) bool { return r == ' ' || r == '-' || r == ':' || r == '_' })
}

// goName is a name as an exported Go identifier: each word capitalized, initialisms in one case.
// `fhirMetadata` is `FHIRMetadata`, `name:contains` is `NameContains`, `fullUrl` is `FullURL`.
func goName(name string) string {
	var b strings.Builder
	for _, word := range words(name) {
		if up, ok := initialisms[strings.ToLower(word)]; ok {
			b.WriteString(up)
			continue
		}
		b.WriteString(strings.ToUpper(word[:1]) + word[1:])
	}
	return b.String()
}

// localName is a name as an unexported Go identifier, for an argument: the first word all lower
// case, the rest as goName writes them. `id` is `id`, not `iD`; `planId` is `planID`.
func localName(name string) string {
	parts := words(name)
	if len(parts) == 0 {
		return ""
	}
	return strings.ToLower(parts[0]) + goName(strings.Join(parts[1:], "_"))
}

// paramName is a query parameter's wire name as a field of its operation's params struct: a
// leading `_` is dropped, so `_count` is `Count` and `_id` is `ID`.
func paramName(wire string) (string, error) {
	name := goName(strings.TrimPrefix(wire, "_"))
	if name == "" || !unicode.IsUpper(rune(name[0])) || strings.ContainsFunc(name, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		return "", fmt.Errorf("the query parameter %q has no Go name: %q", wire, name)
	}
	return name, nil
}

var notSlug = regexp.MustCompile(`[^a-z0-9]+`)

// slug is the slug docs.haau3.com gives a page: lower-cased, apostrophes dropped, anything else as hyphens.
func slug(text string) string {
	text = strings.NewReplacer("'", "", "’", "").Replace(strings.ToLower(text))
	return strings.Trim(notSlug.ReplaceAllString(text, "-"), "-")
}

// docsURLs gives each operation its page on docs.haau3.com, from its tag and summary. A repeat
// gets `-1`, `-2`, as the docs site numbers them.
type docsURLs map[string]int

func (seen docsURLs) url(tag, summary string) string {
	base := "https://docs.haau3.com/api-reference/" + slug(tag) + "/" + slug(summary)
	n := seen[base]
	seen[base] = n + 1
	if n == 0 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, n)
}

// typeName is an HL7 definition's Go type: `Bundle_Entry` is `BundleEntry`.
func typeName(definition string) string {
	return strings.ReplaceAll(definition, "_", "")
}

// singular is an array property's item name, for a type made from an inline object:
// `serviceTypes` is `serviceType`, `specialties` is `specialty`.
func singular(name string) string {
	switch {
	case strings.HasSuffix(name, "ies"):
		return strings.TrimSuffix(name, "ies") + "y"
	case strings.HasSuffix(name, "s"):
		return strings.TrimSuffix(name, "s")
	}
	return name
}
