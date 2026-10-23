package main

import (
	"encoding/json"
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
