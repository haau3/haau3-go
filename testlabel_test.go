package haau3

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/haau3/haau3-go/fhir"
)

// MarkAsTestData labels exactly the resources the API reads for the test data label: the request,
// each Bundle entry, each Parameters resource part at any depth, and the same again inside them.

const htestSystem = "http://terminology.hl7.org/CodeSystem/v3-ActReason"

func isHTEST(v any) bool {
	m, ok := v.(map[string]any)
	return ok && m["system"] == htestSystem && m["code"] == "HTEST"
}

// nodes is every map and slice in a JSON tree, walked with a stack.
func nodes(tree any) []any {
	var found []any
	stack := []any{tree}
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch n := node.(type) {
		case map[string]any:
			found = append(found, n)
			for _, v := range n {
				stack = append(stack, v)
			}
		case []any:
			found = append(found, n)
			stack = append(stack, n...)
		}
	}
	return found
}

func countLabels(tree any) int {
	count := 0
	for _, n := range nodes(tree) {
		if isHTEST(n) {
			count++
		}
	}
	return count
}

// stripLabels takes every HTEST label out of a JSON tree, and a meta or security it leaves empty.
func stripLabels(tree any) {
	for _, n := range nodes(tree) {
		m, ok := n.(map[string]any)
		if !ok {
			continue
		}
		meta, ok := m["meta"].(map[string]any)
		if !ok {
			continue
		}
		if security, ok := meta["security"].([]any); ok {
			kept := []any{}
			for _, c := range security {
				if !isHTEST(c) {
					kept = append(kept, c)
				}
			}
			if len(kept) == 0 {
				delete(meta, "security")
			} else {
				meta["security"] = kept
			}
		}
		if len(meta) == 0 {
			delete(m, "meta")
		}
	}
}

// tree reads JSON as a tree, numbers as written.
func tree(t *testing.T, data []byte) any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// asTree is a resource as the SDK would send it, read back as a tree.
func asTree(t *testing.T, r any) any {
	t.Helper()
	data, err := jsonBytes(r)
	if err != nil {
		t.Fatal(err)
	}
	return tree(t, data)
}

// labelledExamples is every request example in the pinned spec that carries the label, by name.
func labelledExamples(t *testing.T) map[string][]byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("openapi", "openapi.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
			RequestBody struct {
				Content map[string]struct {
					Examples map[string]struct {
						Value json.RawMessage `json:"value"`
					} `json:"examples"`
				} `json:"content"`
			} `json:"requestBody"`
		}
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	found := map[string][]byte{}
	for _, item := range doc.Paths {
		for _, op := range item {
			for _, media := range op.RequestBody.Content {
				for name, example := range media.Examples {
					if countLabels(tree(t, example.Value)) > 0 {
						found[op.OperationID+": "+name] = example.Value
					}
				}
			}
		}
	}
	return found
}

func TestMarkAsTestDataLabelsEachSpecExampleExactlyAsTheSpecLabelsIt(t *testing.T) {
	examples := labelledExamples(t)
	var ops []string
	for name := range examples {
		ops = append(ops, strings.SplitN(name, ":", 2)[0])
	}
	sort.Strings(ops)
	for _, want := range []string{"reconcileMedications", "sandboxProcessMessage"} {
		if i := sort.SearchStrings(ops, want); i == len(ops) || ops[i] != want {
			t.Fatalf("the spec has no labelled %s example to check against: %v", want, ops)
		}
	}
	for name, raw := range examples {
		t.Run(name, func(t *testing.T) {
			original, err := fhir.DecodeResource(raw)
			if err != nil {
				t.Fatal(err)
			}
			stripped := tree(t, raw)
			stripLabels(stripped)
			// The control: stripping took every label out, so the match below is the helper's work.
			if countLabels(stripped) != 0 {
				t.Fatal("labels left after stripping")
			}
			unlabelled, err := fhir.DecodeResource([]byte(mustJSON(t, stripped)))
			if err != nil {
				t.Fatal(err)
			}
			marked, err := MarkAsTestData(unlabelled)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := asTree(t, marked), asTree(t, original); !reflect.DeepEqual(got, want) {
				t.Errorf("marked:\n%s\nthe spec's:\n%s", mustJSON(t, got), mustJSON(t, want))
			}
		})
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := jsonBytes(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func securityOf(t *testing.T, r any) []fhir.Coding {
	t.Helper()
	var holder struct {
		Meta *fhir.Meta `json:"meta"`
	}
	if err := json.Unmarshal([]byte(mustJSON(t, r)), &holder); err != nil {
		t.Fatal(err)
	}
	if holder.Meta == nil {
		return nil
	}
	return holder.Meta.Security
}

func isOnlyHTEST(t *testing.T, r any) bool {
	t.Helper()
	security := securityOf(t, r)
	return len(security) == 1 && *security[0].System == htestSystem && *security[0].Code == "HTEST" && *security[0].Display == "test health data"
}

func TestMarkAsTestDataKeepsTheTypeItIsGiven(t *testing.T) {
	bundle := &fhir.Bundle{Type: new("collection"), Entry: []fhir.BundleEntry{{Resource: &fhir.Patient{}}}}
	marked, err := MarkAsTestData(bundle)
	if err != nil {
		t.Fatal(err)
	}
	// A *fhir.Bundle in is a *fhir.Bundle out, so ProcessMessage takes it with no assertion.
	var _ *fhir.Bundle = marked
	if !isOnlyHTEST(t, marked) || !isOnlyHTEST(t, marked.Entry[0].Resource) {
		t.Errorf("%s", mustJSON(t, marked))
	}
	var asResource fhir.Resource = &fhir.Parameters{}
	markedResource, err := MarkAsTestData(asResource)
	if _, ok := markedResource.(*fhir.Parameters); err != nil || !ok || !isOnlyHTEST(t, markedResource) {
		t.Errorf("%T %v", markedResource, err)
	}
	value, err := MarkAsTestData(fhir.Patient{})
	if err != nil || !isOnlyHTEST(t, value) {
		t.Errorf("%v %s", err, mustJSON(t, value))
	}
}

func TestMarkAsTestDataLabelsEachResourcePartAtAnyDepthAndTheEntriesOfABundleSentAsAPart(t *testing.T) {
	patient := &fhir.Patient{}
	parameters := &fhir.Parameters{Parameter: []fhir.ParametersParameter{
		{Name: new("bundle"), Resource: &fhir.Bundle{Type: new("collection"), Entry: []fhir.BundleEntry{{Resource: patient}, {FullURL: new("urn:uuid:no-resource")}}}},
		{Name: new("outer"), Part: []fhir.ParametersParameter{{Name: new("inner"), Part: []fhir.ParametersParameter{{Name: new("resource"), Resource: patient}}}}},
		{Name: new("asOf"), ValueDateTime: new("2026-10-28")},
	}}
	marked, err := MarkAsTestData(parameters)
	if err != nil {
		t.Fatal(err)
	}
	bundle := marked.Parameter[0].Resource.(*fhir.Bundle)
	for _, r := range []any{marked, bundle, bundle.Entry[0].Resource, marked.Parameter[1].Part[0].Part[0].Resource} {
		if !isOnlyHTEST(t, r) {
			t.Errorf("%s", mustJSON(t, r))
		}
	}
	if bundle.Entry[1].Resource != nil || *marked.Parameter[2].ValueDateTime != "2026-10-28" {
		t.Errorf("%s", mustJSON(t, marked))
	}
	if n := countLabels(asTree(t, marked)); n != 4 {
		t.Errorf("%d labels", n)
	}
}

func TestMarkAsTestDataLeavesAContainedResourceToTheOneThatContainsIt(t *testing.T) {
	order := &fhir.MedicationRequest{
		Status: new("active"), Intent: new("order"), Subject: &fhir.Reference{Reference: new("Patient/p")},
		Contained: []fhir.Resource{&fhir.Medication{ID: new("med")}},
	}
	marked, err := MarkAsTestData(&fhir.Bundle{Type: new("collection"), Entry: []fhir.BundleEntry{{Resource: order}}})
	if err != nil {
		t.Fatal(err)
	}
	labelled := marked.Entry[0].Resource.(*fhir.MedicationRequest)
	if !isOnlyHTEST(t, labelled) || securityOf(t, labelled.Contained[0]) != nil {
		t.Errorf("%s", mustJSON(t, marked))
	}
}

func TestMarkAsTestDataKeepsTheLabelsAndTheMetaAlreadyThereAndNeverAddsASecondHTEST(t *testing.T) {
	confidential := fhir.Coding{System: new("http://terminology.hl7.org/CodeSystem/v3-Confidentiality"), Code: new("N")}
	bundle := &fhir.Bundle{
		Type: new("collection"),
		Meta: &fhir.Meta{Security: []fhir.Coding{{System: new(htestSystem), Code: new("HTEST")}}},
		Entry: []fhir.BundleEntry{{Resource: &fhir.Patient{Meta: &fhir.Meta{
			Profile: []string{"http://example.org/p"}, Security: []fhir.Coding{confidential},
		}}}},
	}
	marked, err := MarkAsTestData(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if security := marked.Meta.Security; len(security) != 1 || security[0].Display != nil {
		t.Errorf("%s", mustJSON(t, marked.Meta))
	}
	meta := marked.Entry[0].Resource.(*fhir.Patient).Meta
	if len(meta.Profile) != 1 || len(meta.Security) != 2 || *meta.Security[0].Code != "N" || *meta.Security[1].Code != "HTEST" {
		t.Errorf("%s", mustJSON(t, meta))
	}
}

func TestMarkAsTestDataNeverChangesTheRequestItIsGiven(t *testing.T) {
	bundle := &fhir.Bundle{Type: new("collection"), Entry: []fhir.BundleEntry{{Resource: &fhir.Patient{}}}}
	before := mustJSON(t, bundle)
	marked, err := MarkAsTestData(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if mustJSON(t, bundle) != before || marked == bundle || bundle.Meta != nil {
		t.Errorf("the request changed: %s", mustJSON(t, bundle))
	}
	again, err := MarkAsTestData(marked)
	if err != nil || mustJSON(t, again) != mustJSON(t, marked) {
		t.Errorf("marking twice is not marking once: %v", err)
	}
}

// Go's stacks grow, so depth is no risk to the walk; this checks every level is reached. Reading
// nested resources back costs time at each level, so the depth is kept to what runs quickly.
func TestMarkAsTestDataLabelsEveryLevelOfADeeplyNestedRequest(t *testing.T) {
	const depth = 200
	root := &fhir.Parameters{}
	current := root
	for range depth {
		next := &fhir.Parameters{}
		current.Parameter = append(current.Parameter, fhir.ParametersParameter{Name: new("level"), Resource: next})
		current = next
	}
	marked, err := MarkAsTestData(root)
	if err != nil {
		t.Fatal(err)
	}
	if n := countLabels(asTree(t, marked)); n != depth+1 {
		t.Errorf("%d labels", n)
	}
}

func TestMarkAsTestDataPassesANilRequestThrough(t *testing.T) {
	marked, err := MarkAsTestData((*fhir.Bundle)(nil))
	if err != nil || marked != nil {
		t.Errorf("%v %v", marked, err)
	}
	var none fhir.Resource
	if r, err := MarkAsTestData(none); err != nil || r != nil {
		t.Errorf("%v %v", r, err)
	}
}

// The control for the error: a body that cannot be written as JSON is refused as the call would
// refuse it, never sent half-labelled.
func TestMarkAsTestDataRefusesABodyThatCannotBeWrittenAsJSON(t *testing.T) {
	broken := &fhir.OtherResource{Type: "Basic", JSON: json.RawMessage(`{"resourceType":`)}
	_, err := MarkAsTestData(broken)
	var e *Error
	if !errors.As(err, &e) || e.Kind != KindNotSent || !errors.Is(err, ErrNotSent) {
		t.Fatalf("%v", err)
	}
	if e.Error() != "MarkAsTestData did not mark the request: its body cannot be written as JSON." {
		t.Errorf("%q", e.Error())
	}
}
