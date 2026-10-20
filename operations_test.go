package haau3

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestEachOperationSendsItsExampleAndReturnsItsAnswer(t *testing.T) {
	if len(specExamples) != 37 {
		t.Fatalf("the spec has %d operations; this release was generated from 37", len(specExamples))
	}
	for _, ex := range specExamples {
		t.Run(ex.id, func(t *testing.T) {
			fhirPath := isFHIR(ex.path)
			rec := record(reply{status: 200, body: []byte(ex.answer), plain: !fhirPath})
			c := testClient(t, rec)
			var info ResponseInfo
			got, err := ex.call(context.Background(), c, ex, WithResponseInfo(&info))
			if err != nil {
				t.Fatal(err)
			}
			// Written back as JSON, the answer is the example: every element of it is one its type holds.
			written, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			if !sameJSON(t, written, []byte(ex.answer)) {
				t.Errorf("the answer reads as\n%s\nnot\n%s", written, ex.answer)
			}
			if rec.count() != 1 {
				t.Fatalf("%d requests, want 1", rec.count())
			}
			req := rec.last()
			if req.Method != ex.method {
				t.Errorf("method %s, want %s", req.Method, ex.method)
			}
			if wantPath := sentPath(ex); req.URL.EscapedPath() != wantPath {
				t.Errorf("path %s, want %s", req.URL.EscapedPath(), wantPath)
			}
			if got, want := sortedPairs(queryPairs(t, req.URL.RawQuery)), sortedPairs(ex.sent); !slices.Equal(got, want) {
				t.Errorf("query %v, want %v", got, want)
			}
			if got := req.Header.Get("Authorization"); got != "Bearer "+testKey {
				t.Errorf("Authorization %q", got)
			}
			if got := req.Header.Get("User-Agent"); !strings.HasPrefix(got, "haau3-go/"+Version+" (go 1.") {
				t.Errorf("User-Agent %q", got)
			}
			wantAccept := "application/json"
			if fhirPath {
				wantAccept = "application/fhir+json"
			}
			if got := req.Header.Get("Accept"); got != wantAccept {
				t.Errorf("Accept %q, want %q", got, wantAccept)
			}
			if ex.body != "" {
				if got := req.Header.Get("Content-Type"); got != "application/fhir+json" {
					t.Errorf("Content-Type %q", got)
				}
				if !sameJSON(t, rec.lastBody(), []byte(ex.body)) {
					t.Errorf("sent\n%s\nnot the example\n%s", rec.lastBody(), ex.body)
				}
			} else if req.Header.Get("Content-Type") != "" || len(rec.lastBody()) != 0 {
				t.Errorf("a body was sent for an operation that takes none")
			}
			if info.Status != 200 || info.StatusText != "OK" || info.RequestID != requestID || info.Attempts != 1 {
				t.Errorf("ResponseInfo %+v", info)
			}
		})
	}
}

func TestEveryBatchableOperationAnswersInOneBatch(t *testing.T) {
	var probes []entryProbe
	var batchable []specExample
	for _, ex := range specExamples {
		if ex.entry != nil {
			probes = append(probes, ex.entry(ex))
			batchable = append(batchable, ex)
		}
	}
	if len(probes) != 29 {
		t.Fatalf("%d operations can go in a batch; the spec's are 29", len(probes))
	}
	rec := record(reply{handle: specHandler})
	c := testClient(t, rec)
	entries := make([]Entry, len(probes))
	for i, p := range probes {
		entries[i] = p.entry
	}
	if err := c.Batch(context.Background(), entries); err != nil {
		t.Fatal(err)
	}
	for i, p := range probes {
		got, err := p.result()
		if err != nil {
			t.Errorf("%s: %v", batchable[i].id, err)
			continue
		}
		written, _ := json.Marshal(got)
		want, _ := specAnswer(batchable[i].method, strings.SplitN(sentPath(batchable[i]), "?", 2)[0])
		if !sameJSON(t, written, want) {
			t.Errorf("%s: the entry reads as %s", batchable[i].id, written)
		}
	}
	// Each entry's URL is the one a direct call sends, relative to the FHIR base.
	var sent batchBundle
	if err := json.Unmarshal(rec.lastBody(), &sent); err != nil {
		t.Fatal(err)
	}
	for i, e := range sent.Entry {
		direct := record(reply{status: 200, body: []byte(batchable[i].answer)})
		if _, err := batchable[i].call(context.Background(), testClient(t, direct), batchable[i]); err != nil {
			t.Fatal(err)
		}
		u := direct.last().URL
		want := strings.TrimPrefix(u.EscapedPath(), fhirBase+"/")
		if u.RawQuery != "" {
			want += "?" + u.RawQuery
		}
		if e.Request.URL != want || e.Request.Method != batchable[i].method {
			t.Errorf("entry %d is %s %s, a direct call %s %s", i+1, e.Request.Method, e.Request.URL, batchable[i].method, want)
		}
	}
}

// sentPath is the operation's path with its example's values, which need no encoding.
func sentPath(ex specExample) string {
	path := ex.path
	for _, v := range ex.pathValues {
		loc := placeholderPattern.FindStringIndex(path)
		path = path[:loc[0]] + v + path[loc[1]:]
	}
	return path
}

// The client's methods are exactly the spec's operations and the SDK's own few, so a spec change
// that adds or removes an operation shows here.
func TestTheClientSurfaceIsTheSpecsOperations(t *testing.T) {
	own := []string{"BaseURL", "Batch", "Format", "GoString", "Pages", "Request", "Resources", "String"}
	var want []string
	for _, ex := range specExamples {
		want = append(want, ex.name)
	}
	want = append(want, own...)
	slices.Sort(want)
	ct := reflect.TypeFor[*Client]()
	var got []string
	for i := range ct.NumMethod() {
		got = append(got, ct.Method(i).Name)
	}
	if !slices.Equal(got, want) {
		t.Errorf("Client's methods are\n%v\nnot\n%v", got, want)
	}
	calls := reflect.TypeFor[BatchCalls]()
	if calls.NumMethod() != 29 {
		t.Errorf("Calls makes %d kinds of entry, not 29", calls.NumMethod())
	}
	for i := range calls.NumMethod() {
		if slices.Contains(notInABatch, calls.Method(i).Name) {
			t.Errorf("Calls makes %s, which a batch refuses", calls.Method(i).Name)
		}
	}
}
