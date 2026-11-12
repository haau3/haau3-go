package main

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
)

// The pinned spec, with one operation's object changed by edit.
func specWith(t *testing.T, path, method string, edit func(op map[string]any)) ([]byte, error) {
	t.Helper()
	data, err := os.ReadFile("../../../openapi/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	op := doc["paths"].(map[string]any)[path].(map[string]any)[method].(map[string]any)
	edit(op)
	return json.Marshal(doc)
}

func TestTheOperationsABatchRefusesComeFromTheSpec(t *testing.T) {
	data, err := os.ReadFile("../../../openapi/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	s, err := parseSpec(data)
	if err != nil {
		t.Fatal(err)
	}
	ids := s.notInABatch()
	if ids[0] != "fhirBatch" || !slices.Contains(ids, "reconcileMedications") {
		t.Errorf("not in a batch: %v", ids)
	}
	for _, o := range s.Operations {
		if o.Batchable && (o.Refused || !strings.HasPrefix(o.Path, fhirBase+"/")) {
			t.Errorf("%s is offered in a batch", o.ID)
		}
	}
}

func TestABatchableMarkOtherThanFalseStopsTheGenerator(t *testing.T) {
	data, _ := specWith(t, "/v1/fhir/r4/metadata", "get", func(op map[string]any) { op[batchableField] = true })
	if _, err := parseSpec(data); err == nil || !strings.Contains(err.Error(), "defines only false") {
		t.Errorf("got %v", err)
	}
}

func TestABatchableMarkOutsideTheFHIRBaseStopsTheGenerator(t *testing.T) {
	data, _ := specWith(t, "/v1/scheduling/publishers", "get", func(op map[string]any) { op[batchableField] = false })
	if _, err := parseSpec(data); err == nil || !strings.Contains(err.Error(), "not on the FHIR base") {
		t.Errorf("got %v", err)
	}
}

// sandboxMetadataSample is the Sandbox metadata's sample, from the pinned spec with edit applied to its
// operation. The operation sends no body, so the sample needs no FHIR schema.
func sandboxMetadataSample(t *testing.T, edit func(op map[string]any)) (string, error) {
	t.Helper()
	data, _ := specWith(t, "/v1/sandbox/fhir/r4/metadata", "get", edit)
	s, err := parseSpec(data)
	if err != nil {
		return "", err
	}
	for _, o := range s.Operations {
		if o.ID == "sandboxMetadata" {
			return goSample(s, nil, o)
		}
	}
	t.Fatal("the spec has no sandboxMetadata")
	return "", nil
}

func TestASampleReadsALiveKeyWhereItsOperationTakesTheDocumentsSecurity(t *testing.T) {
	// With no security of its own, whichever spec is pinned, the operation takes the document's.
	source, err := sandboxMetadataSample(t, func(op map[string]any) { delete(op, "security") })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(source, "client, err := haau3.NewClient() // reads HAAU3_API_KEY\n") {
		t.Errorf("got\n%s", source)
	}
}

func TestASampleReadsATestKeyWhereItsOperationNamesTestKeyAuth(t *testing.T) {
	source, err := sandboxMetadataSample(t, func(op map[string]any) {
		op["security"] = []any{map[string]any{"testKeyAuth": []any{}}}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(source, "client, err := haau3.NewClient() // reads HAAU3_API_KEY, a test key\n") {
		t.Errorf("got\n%s", source)
	}
}

func TestASecurityTheSamplesHaveNoWordsForStopsTheGenerator(t *testing.T) {
	for _, security := range [][]any{
		{map[string]any{"apiKeyAuth": []any{}}},
		{},
		{map[string]any{"bearerAuth": []any{}}, map[string]any{"testKeyAuth": []any{}}},
		{map[string]any{"bearerAuth": []any{}, "testKeyAuth": []any{}}},
	} {
		_, err := sandboxMetadataSample(t, func(op map[string]any) { op["security"] = security })
		if err == nil || !strings.HasPrefix(err.Error(), "sandboxMetadata: security ") || !strings.HasSuffix(err.Error(), " names no key a sample knows") {
			t.Errorf("%v: got %v", security, err)
		}
	}
}

func TestEveryCommittedSampleSaysTheKeyItsOperationsSecurityNames(t *testing.T) {
	data, err := os.ReadFile("../../../openapi/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	s, err := parseSpec(data)
	if err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile("../../../openapi/code-samples.json")
	if err != nil {
		t.Fatal(err)
	}
	var overlay struct {
		Actions []struct {
			Target string `json:"target"`
			Update struct {
				Samples []struct {
					Source string `json:"source"`
				} `json:"x-codeSamples"`
			} `json:"update"`
		} `json:"actions"`
	}
	if err := json.Unmarshal(committed, &overlay); err != nil {
		t.Fatal(err)
	}
	sources := map[string]string{}
	for _, a := range overlay.Actions {
		sources[a.Target] = a.Update.Samples[0].Source
	}
	if len(sources) != len(s.Operations) {
		t.Fatalf("%d samples for %d operations", len(sources), len(s.Operations))
	}
	for _, o := range s.Operations {
		source := sources[fmt.Sprintf("$.paths['%s'].%s", o.Path, strings.ToLower(o.Method))]
		if !strings.Contains(source, "haau3.NewClient() // "+o.KeyComment+"\n") {
			t.Errorf("%s says\n%s", o.ID, source)
		}
	}
}

func TestABodyThatNestsAResourceIsReadFromAFile(t *testing.T) {
	if !nestsResources(json.RawMessage(`{"resourceType":"Bundle","entry":[{"resource":{"resourceType":"Claim"}}]}`)) {
		t.Error("a Bundle with an entry's resource")
	}
	if nestsResources(json.RawMessage(`{"resourceType":"Parameters","parameter":[{"name":"code","valueCode":"x"}]}`)) {
		t.Error("a Parameters resource of values (the control)")
	}
	if got := asPhrase("A pharmacy benefit check"); got != "a pharmacy benefit check" {
		t.Errorf("asPhrase: %q", got)
	}
	if got := asPhrase("FHIR records"); got != "FHIR records" {
		t.Errorf("asPhrase keeps an acronym: %q", got)
	}
}

func withHeader(t *testing.T, path, method string, header map[string]any) []byte {
	t.Helper()
	data, _ := specWith(t, path, method, func(op map[string]any) {
		params, _ := op["parameters"].([]any)
		op["parameters"] = append([]any{header}, params...)
	})
	return data
}

func TestAnOptionalHeaderParameterIsAnOptionOfItsOwn(t *testing.T) {
	param := map[string]any{"name": "Example-Header", "in": "header", "required": false, "description": "An  example.",
		"schema": map[string]any{"type": "string", "maxLength": 9.0, "pattern": "^x+$"}}
	s, err := parseSpec(withHeader(t, "/v1/fhir/r4/$process-message", "post", param))
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range s.Operations {
		if o.ID != "processMessage" {
			continue
		}
		if len(o.Query) != 0 || len(o.PathParams) != 0 {
			t.Errorf("the header became a parameter of the method: %+v %+v", o.Query, o.PathParams)
		}
		want := header{Wire: "Example-Header", Option: "WithExampleHeader", Description: "An example.", MaxLength: 9, Pattern: "^x+$"}
		if !slices.Contains(o.Headers, want) {
			t.Errorf("%+v", o.Headers)
		}
		return
	}
	t.Fatal("no processMessage")
}

func TestAHeaderTheGeneratorCannotSendStopsIt(t *testing.T) {
	for name, tc := range map[string]struct {
		header map[string]any
		said   string
	}{
		"an option the SDK has": {map[string]any{"name": "Timeout", "in": "header", "schema": map[string]any{"type": "string"}}, "would be the option WithTimeout"},
		"not a string":          {map[string]any{"name": "Example-Header", "in": "header", "schema": map[string]any{"type": "integer"}}, "is not a string"},
		"a pattern Go cannot read": {map[string]any{"name": "Example-Header", "in": "header",
			"schema": map[string]any{"type": "string", "pattern": "^(?=x)"}}, "a pattern Go's regexp cannot read"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseSpec(withHeader(t, "/v1/fhir/r4/$process-message", "post", tc.header)); err == nil || !strings.Contains(err.Error(), tc.said) {
				t.Errorf("got %v", err)
			}
		})
	}
}

func TestAHeaderOnAnOperationABatchCanCarryStopsTheGenerator(t *testing.T) {
	param := map[string]any{"name": "Example-Header", "in": "header", "schema": map[string]any{"type": "string"}}
	if _, err := parseSpec(withHeader(t, "/v1/fhir/r4/metadata", "get", param)); err == nil || !strings.Contains(err.Error(), "whose entries carry no headers") {
		t.Errorf("got %v", err)
	}
}

func TestOneHeaderDeclaredWithTwoSchemasStopsTheGenerator(t *testing.T) {
	data, err := os.ReadFile("../../../openapi/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	// Two operations no batch carries, declaring one header with two lengths.
	for path, most := range map[string]float64{"/v1/fhir/r4/$process-message": 9, "/v1/fhir/r4/$reconcile-medications": 10} {
		op := doc["paths"].(map[string]any)[path].(map[string]any)["post"].(map[string]any)
		params, _ := op["parameters"].([]any)
		param := map[string]any{"name": "Example-Header", "in": "header", "schema": map[string]any{"type": "string", "maxLength": most}}
		op["parameters"] = append([]any{param}, params...)
	}
	both, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseSpec(both); err == nil || !strings.Contains(err.Error(), "with another schema") {
		t.Errorf("got %v", err)
	}
}

func TestARequiredHeaderParameterStopsTheGenerator(t *testing.T) {
	header := map[string]any{"name": "Example-Header", "in": "header", "required": true, "schema": map[string]any{"type": "string"}}
	data, _ := specWith(t, "/v1/fhir/r4/$process-message", "post", func(op map[string]any) {
		op["parameters"] = append([]any{header}, op["parameters"].([]any)...)
	})
	if _, err := parseSpec(data); err == nil || !strings.Contains(err.Error(), "the required header Example-Header") {
		t.Errorf("got %v", err)
	}
}

func TestAParameterInACookieStillStopsTheGenerator(t *testing.T) {
	cookie := map[string]any{"name": "example", "in": "cookie", "schema": map[string]any{"type": "string"}}
	data, _ := specWith(t, "/v1/fhir/r4/$process-message", "post", func(op map[string]any) {
		op["parameters"] = append([]any{cookie}, op["parameters"].([]any)...)
	})
	if _, err := parseSpec(data); err == nil || !strings.Contains(err.Error(), "a parameter in cookie, which the generator does not read") {
		t.Errorf("got %v", err)
	}
}

// A deprecated operation's words: the passage from "Deprecated:", each operationId in it written as
// its Go method; anything else stops the generator.
func TestADeprecatedOperationsWordsNameItsReplacementAsAGoMethod(t *testing.T) {
	methods := map[string]string{"oldThing": "OldThing", "otherOldThing": "OtherOldThing", "fhirMetadata": "FHIRMetadata"}
	deprecated := map[string]bool{"oldThing": true, "otherOldThing": true}
	if description, words, err := deprecation("oldThing", false, "Does a thing.", methods, deprecated); description != "Does a thing." || words != "" || err != nil {
		t.Errorf("deprecated: false: %q %q %v", description, words, err)
	}
	description, words, err := deprecation("oldThing", true, "Does a thing. Deprecated: call `fhirMetadata`\ninstead, at `https://example.test`.", methods, deprecated)
	if err != nil || description != "Does a thing." || words != "Call `FHIRMetadata` instead, at `https://example.test`." {
		t.Errorf("%q %q %v", description, words, err)
	}
	for _, tc := range []struct {
		mark        any
		description string
		err         string
	}{
		{"yes", "Deprecated: call `fhirMetadata`.", "oldThing carries deprecated: yes, where the spec defines only true or false"},
		{nil, "Deprecated: call `fhirMetadata`.", "where the spec defines only true or false"},
		{true, "Does a thing.", "needs exactly one passage"},
		{true, "Deprecated: a. Deprecated: `fhirMetadata`.", "needs exactly one passage"},
		{true, "Deprecated: call `somethingElse`.", "that is not deprecated"},
		{true, "Deprecated: call `oldThing`.", "that is not deprecated"},
		{true, "Deprecated: call `otherOldThing`.", "that is not deprecated"},
	} {
		if _, _, err := deprecation("oldThing", tc.mark, tc.description, methods, deprecated); err == nil || !strings.Contains(err.Error(), tc.err) {
			t.Errorf("%v %q: got %v", tc.mark, tc.description, err)
		}
	}
}

// The pinned spec's two deprecated operations end their doc comments with one Deprecated paragraph,
// and their descriptions no longer carry the passage.
func TestEachDeprecatedMethodHasOneDeprecatedParagraph(t *testing.T) {
	data, err := os.ReadFile("../../../operations_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	if got := strings.Count(source, "Deprecated:"); got != 2 {
		t.Errorf("operations_gen.go says Deprecated: %d times, not 2", got)
	}
	for _, method := range []string{"SandboxMetadata", "SandboxProcessMessage"} {
		at := strings.Index(source, "func (c *Client) "+method+"(")
		if at < 0 || !strings.Contains(source[max(0, at-400):at], "// Deprecated: Call `") {
			t.Errorf("%s has no Deprecated paragraph just above it", method)
		}
	}
}
