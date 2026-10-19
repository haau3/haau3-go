package haau3

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"testing"

	"github.com/haau3/haau3-go/fhir"
)

// The production API, on a real key. Runs only with HAAU3_LIVE_TEST=1 and a key in HAAU3_API_KEY.
func TestLive(t *testing.T) {
	if os.Getenv("HAAU3_LIVE_TEST") != "1" || os.Getenv("HAAU3_API_KEY") == "" {
		t.Skip("set HAAU3_LIVE_TEST=1 and HAAU3_API_KEY to call the production API")
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

func uuidV4() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
