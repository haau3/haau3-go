package haau3

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/haau3/haau3-go/fhir"
)

// The production API, on a real key. Runs only with HAAU3_LIVE_TEST=1 and a key in HAAU3_API_KEY.
// The calls are the API's services outside the Sandbox, so they skip on a test key
// (haau3_sk_test_), whose calls TestEachSampleRunsAsWritten makes.
func TestLive(t *testing.T) {
	if os.Getenv("HAAU3_LIVE_TEST") != "1" || os.Getenv("HAAU3_API_KEY") == "" {
		t.Skip("set HAAU3_LIVE_TEST=1 and HAAU3_API_KEY to call the production API")
	}
	if keyMode(os.Getenv("HAAU3_API_KEY")) == "test" {
		t.Skip("a test key works only in the Sandbox: TestEachSampleRunsAsWritten calls it")
	}
	ctx := context.Background()
	client, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	found, err := client.SearchSlots(ctx, SearchSlotsParams{State: new("MA"), Limit: new(3)})
	if err != nil || found.Total < 0 {
		t.Fatalf("slots: %v", err)
	}
	plan, err := client.ReadInsurancePlan(ctx, "H2406-013-000")
	if err != nil || *plan.ID != "H2406-013-000" {
		t.Fatalf("plan: %v", err)
	}
	glucose, err := client.CodeSystemLookup(ctx, CodeSystemLookupParams{System: new("http://loinc.org"), Code: "2345-7"})
	if err != nil || glucose.ResourceType() != "Parameters" {
		t.Fatalf("lookup: %v", err)
	}
	text := "Instill 2 drops in the affected ear twice daily"
	structured, err := client.StructureDosage(ctx, StructureDosageRequest(StructureDosageInput{
		Text:       text,
		Medication: &fhir.CodeableConcept{Coding: []fhir.Coding{{System: new(RxNormSystem), Code: new("1923428")}}},
	}))
	if err != nil {
		t.Fatalf("structure dosage: %v", err)
	}
	if part, err := Parameter(structured, "dosage"); err != nil || part == nil || deref(part.ValueDosage.Text) != text {
		t.Fatalf("dosage part: %+v, %v", part, err)
	} else if flags := ReviewFlags(part.ValueDosage).Flags; len(flags) != 1 || flags[0].Code != "route-form-mismatch" {
		t.Errorf("flags: %+v", flags)
	}
	publishers, err := client.ListPublishers(ctx)
	if err != nil || publishers.Publishers == nil {
		t.Fatalf("publishers: %v", err)
	}
	first, err := client.SearchInsurancePlan(ctx, SearchInsurancePlanParams{CoverageAreaState: new("NC"), Count: new(2)})
	if err != nil {
		t.Fatal(err)
	}
	pages := 0
	for _, err := range client.Pages(ctx, first) {
		if err != nil {
			t.Fatalf("pages: %v", err)
		}
		if pages++; pages == 2 {
			break
		}
	}
	if pages != 2 {
		t.Errorf("%d pages", pages)
	}
	found2, missing := Calls.ReadInsurancePlan("H2406-013-000"), Calls.ReadInsurancePlan("no-such-plan")
	if err := client.Batch(ctx, []Entry{found2, missing}); err != nil {
		t.Fatal(err)
	}
	if p, err := found2.Result(); err != nil || p.ResourceType() != "InsurancePlan" {
		t.Errorf("batch entry: %v", err)
	}
	if _, err := missing.Result(); asError(t, err).Kind != KindRefused {
		t.Errorf("batch entry: %v", err)
	}
	job := uuidV4()
	var info ResponseInfo
	if _, err := client.ReadInsurancePlan(ctx, "H2406-013-000", WithCorrelationID(job), WithResponseInfo(&info)); err != nil || info.CorrelationID != job || info.RequestID == "" {
		t.Errorf("correlation: %+v %v", info, err)
	}
	_, err = client.ReadInsurancePlan(ctx, "no-such-plan")
	if e := asError(t, err); e.Kind != KindRefused || e.Status != 404 || e.RequestID == "" {
		t.Errorf("refusal: %#v", e)
	}
	metadata, err := client.FHIRMetadata(ctx, FHIRMetadataParams{})
	if _, ok := metadata.(*fhir.CapabilityStatement); err != nil || !ok {
		t.Errorf("metadata: %T %v", metadata, err)
	}
}

// Patient data on the production API, without an agreement: MedList's example request from the
// spec, made-up records, answered once MarkAsTestData labels them and refused unlabelled. Needs a
// live key (haau3_sk_live_) from an organization with no agreement on file.
func TestLiveWithoutAnAgreement(t *testing.T) {
	if os.Getenv("HAAU3_LIVE_TEST") != "1" || keyMode(os.Getenv("HAAU3_API_KEY")) != "live" {
		t.Skip("set HAAU3_LIVE_TEST=1 and a live key from an organization with no agreement on file in HAAU3_API_KEY")
	}
	ctx := context.Background()
	client, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	var example any
	for _, o := range specOperations(t) {
		if o.id == "reconcileMedications" {
			example = o.body
		}
	}
	data, err := json.Marshal(withoutTestLabels(example))
	if err != nil || strings.Contains(string(data), "HTEST") {
		t.Fatalf("the example still carries a test-data label: %v", err)
	}
	records, err := fhir.DecodeResource(data)
	if err != nil {
		t.Fatal(err)
	}
	labelled, err := MarkAsTestData(records)
	if err != nil {
		t.Fatal(err)
	}
	if list, err := client.ReconcileMedications(ctx, labelled); err != nil || list.ResourceType() != "Bundle" {
		t.Errorf("labelled: %v", err)
	}
	_, err = client.ReconcileMedications(ctx, records)
	if e := asError(t, err); e.Kind != KindForbidden || e.Code != "phi-agreement-required" {
		t.Errorf("unlabelled: %#v", e)
	}
}

// withoutTestLabels is a copy of decoded JSON with every HTEST label taken off, so made-up records
// read as real patient data.
func withoutTestLabels(value any) any {
	switch v := value.(type) {
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = withoutTestLabels(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, item := range v {
			out[k] = withoutTestLabels(item)
		}
		meta, _ := out["meta"].(map[string]any)
		if security, ok := meta["security"].([]any); ok {
			kept := []any{}
			for _, c := range security {
				if coding, _ := c.(map[string]any); coding["code"] != "HTEST" {
					kept = append(kept, c)
				}
			}
			meta["security"] = kept
			if len(kept) == 0 {
				delete(meta, "security")
			}
			if len(meta) == 0 {
				delete(out, "meta")
			}
		}
		return out
	}
	return value
}

func uuidV4() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
