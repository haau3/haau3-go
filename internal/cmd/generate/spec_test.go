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
