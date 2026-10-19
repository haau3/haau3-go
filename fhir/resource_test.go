package fhir

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

func TestAResourceMarshalsWithItsResourceTypeFirst(t *testing.T) {
	got, err := json.Marshal(&MedicationStatement{ID: new("m1"), Status: new("active")})
	if err != nil || string(got) != `{"resourceType":"MedicationStatement","id":"m1","status":"active"}` {
		t.Errorf("%s %v", got, err)
	}
	if got, _ := json.Marshal(Patient{}); string(got) != `{"resourceType":"Patient"}` {
		t.Errorf("%s", got)
	}
	// XHTML in a narrative stays as it is through an encoder that keeps HTML, as the SDK sends.
	// (json.Marshal escapes `<` as \u003c in anything it writes, which is the same JSON.)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(&Patient{Text: &Narrative{Div: new(`<div xmlns="http://www.w3.org/1999/xhtml">a &amp; b</div>`)}}); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != `{"resourceType":"Patient","text":{"div":"<div xmlns=\"http://www.w3.org/1999/xhtml\">a &amp; b</div>"}}`+"\n" {
		t.Errorf("%s", got)
	}
}

func TestADecimalKeepsItsDigits(t *testing.T) {
	in := `{"resourceType":"Observation","valueQuantity":{"value":1.50,"unit":"mg"}}`
	r, err := DecodeResource([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if v := *r.(*Observation).ValueQuantity.Value; v != "1.50" {
		t.Errorf("%s", v)
	}
	out, _ := json.Marshal(r)
	if string(out) != `{"resourceType":"Observation","valueQuantity":{"value":1.50,"unit":"mg"}}` {
		t.Errorf("%s", out)
	}
}

func TestAPlaceThatHoldsAnyResourceReadsItsType(t *testing.T) {
	in := `{"resourceType":"Bundle","type":"searchset","entry":[
		{"resource":{"resourceType":"InsurancePlan","id":"H1","contained":[{"resourceType":"Organization","id":"o"}]}},
		{"resource":{"resourceType":"SubscriptionStatus","id":"r5"}},
		{"response":{"status":"404","outcome":{"resourceType":"OperationOutcome"}}}]}`
	var b Bundle
	if err := json.Unmarshal([]byte(in), &b); err != nil {
		t.Fatal(err)
	}
	plan, ok := b.Entry[0].Resource.(*InsurancePlan)
	if !ok || *plan.ID != "H1" || plan.Contained[0].(*Organization).ID == nil {
		t.Fatalf("%#v", b.Entry[0].Resource)
	}
	other, ok := b.Entry[1].Resource.(*OtherResource)
	if !ok || other.ResourceType() != "SubscriptionStatus" || string(other.JSON) != `{"resourceType":"SubscriptionStatus","id":"r5"}` {
		t.Fatalf("%#v", b.Entry[1].Resource)
	}
	if _, ok := b.Entry[2].Response.Outcome.(*OperationOutcome); !ok {
		t.Errorf("%#v", b.Entry[2].Response.Outcome)
	}
	if r, err := DecodeResource([]byte("null")); r != nil || err != nil {
		t.Errorf("null read as %#v, %v", r, err)
	}
	// And written back as it was read.
	out, _ := json.Marshal(&b)
	var x, y any
	_ = json.Unmarshal([]byte(in), &x)
	_ = json.Unmarshal(out, &y)
	a, _ := json.Marshal(x)
	c, _ := json.Marshal(y)
	if string(a) != string(c) {
		t.Errorf("\n%s\n%s", a, c)
	}
}

func TestAResourceThatCannotBeReadSaysWhere(t *testing.T) {
	in := `{"resourceType":"Bundle","entry":[{"resource":{"resourceType":"Patient","active":"yes"}}]}`
	var b Bundle
	err := json.Unmarshal([]byte(in), &b)
	var el *ElementError
	var typeErr *json.UnmarshalTypeError
	if !errors.As(err, &el) || el.Element != "resource" || !errors.As(err, &typeErr) {
		t.Errorf("%v", err)
	}
}

func TestEveryResourceTypeIsKnownAndNamesItself(t *testing.T) {
	if len(newResource) != 146 {
		t.Errorf("%d resource types; FHIR R4 has 146", len(newResource))
	}
	for name, make := range newResource {
		if got := make().ResourceType(); got != name {
			t.Errorf("%s says it is %s", name, got)
		}
		data, _ := json.Marshal(make())
		if r, err := DecodeResource(data); err != nil || r.ResourceType() != name {
			t.Errorf("%s: %v %v", name, r, err)
		}
	}
}

func TestAPrimitivesExtensionsHaveTheirOwnField(t *testing.T) {
	in := `{"resourceType":"Patient","birthDate":"1970","_birthDate":{"extension":[{"url":"x","valueCode":"y"}]},"name":[{"given":["A",null],"_given":[null,{"id":"g2"}]}]}`
	r, err := DecodeResource([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	p := r.(*Patient)
	if *p.BirthDate != "1970" || *p.BirthDateExt.Extension[0].ValueCode != "y" || p.Name[0].GivenExt[0] != nil || *p.Name[0].GivenExt[1].ID != "g2" {
		t.Errorf("%+v", p)
	}
}
