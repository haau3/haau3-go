// Package haau3 is the Go SDK for the haau3 platform API (https://docs.haau3.com), as typed calls:
//
//   - Scheduling: open appointment slots from many SMART Scheduling Links publishers, in one search.
//   - Plan Benefits: Medicare Advantage and ACA Marketplace plans as FHIR InsurancePlan, searchable
//     by state or county, and which providers are in a plan's network.
//   - MedList: one reconciled medication list from one patient's FHIR medication records
//     (medication reconciliation).
//
// On the same key and FHIR base: RxNorm and LOINC lookups, value sets and code validation
// (Terminology), practitioners and organizations from CMS's National Provider Directory (Provider
// directory), and standard FHIR Questionnaires such as the PHQ-9.
//
// The haau3 platform API is in preview: it may change as we build, usually to add features or improve performance.
//
// # A first call
//
//	client, err := haau3.NewClient() // reads HAAU3_API_KEY
//	if err != nil {
//		return err
//	}
//	plan, err := client.ReadInsurancePlan(ctx, "H2406-013-000")
//	if err != nil {
//		return err
//	}
//	fmt.Println(*plan.Name)
//
// Every operation of the API is a method on Client, named by its operationId: api.md in the
// repository lists them by service. FHIR answers are package fhir's types, FHIR R4 from HL7's own
// schema; the other services' answers are this package's.
//
// # Errors
//
// Every error the SDK returns is an *Error, but for your own context's, with a Kind: KindAuth,
// KindRefused, KindNoAnswer, or KindNotSent for a request it refused to send. No answer is never
// reported as an empty result, and an empty result is never an error. An error never prints the
// body the API answered with.
//
// # With an AI coding agent
//
// https://docs.haau3.com/llms.txt lists every docs page as Markdown, and https://docs.haau3.com/mcp
// is an MCP server that searches the docs.
package haau3

//go:generate go run ./internal/cmd/generate
