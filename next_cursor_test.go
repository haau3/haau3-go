package haau3

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/haau3/haau3-go/fhir"
)

// NextCursor: the cursor in a search page's next link, for a search continued in a later request.

const apiBase = "https://api.haau3.com/v1/fhir/r4"

// linkPage is a search page with the links given, relation then URL.
func linkPage(t *testing.T, links ...string) *fhir.Bundle {
	t.Helper()
	raw := map[string]any{"resourceType": "Bundle", "type": "searchset", "total": 0}
	var list []any
	for i := 0; i+1 < len(links); i += 2 {
		list = append(list, map[string]any{"relation": links[i], "url": links[i+1]})
	}
	if list != nil {
		raw["link"] = list
	}
	return firstPage(t, raw)
}

func TestNextCursorIsTheNextLinksPageValue(t *testing.T) {
	got, err := NextCursor(linkPage(t, "self", apiBase+"/Slot?status=free", "next", apiBase+"/Slot?status=free&_count=25&page=eyJ2IjoxfQ"))
	if err != nil || got != "eyJ2IjoxfQ" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestNextCursorIsEmptyOnTheLastPage(t *testing.T) {
	for _, page := range []*fhir.Bundle{linkPage(t, "self", apiBase+"/Slot?page=eyJ2IjoxfQ"), linkPage(t)} {
		if got, err := NextCursor(page); err != nil || got != "" {
			t.Errorf("got %q, %v, want the last page", got, err)
		}
	}
}

func TestNextCursorRefusesANextLinkItCannotRead(t *testing.T) {
	for link, want := range map[string]string{
		apiBase + "/Slot?status=free":       "carries no page cursor",
		apiBase + "/Slot?status=free&page=": "carries no page cursor",
		apiBase + "/Slot?page=one&page=two": "carries 2 page cursors",
		"https://[not-a-host":               "is not a URL",
		apiBase + "/Slot?page=%zz":          "is not a URL",
	} {
		got, err := NextCursor(linkPage(t, "next", link))
		if err == nil || got != "" || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %q, %v, want an error saying it %s", link, got, err, want)
		}
	}
}

func TestNextCursorReadsARelativeLinkAndTheFirstNextLink(t *testing.T) {
	for _, page := range []*fhir.Bundle{
		linkPage(t, "next", "Slot?status=free&page=abc"),
		linkPage(t, "next", "?page=abc"),
		linkPage(t, "next", "?page=abc", "next", "?page=other"),
	} {
		if got, err := NextCursor(page); err != nil || got != "abc" {
			t.Errorf("got %q, %v", got, err)
		}
	}
}

func TestNextCursorDecodesACursorThatIsSentBackAsItWas(t *testing.T) {
	cursor, err := NextCursor(linkPage(t, "next", apiBase+"/Slot?page=a%2Fb%3D%3D"))
	if err != nil || cursor != "a/b==" {
		t.Fatalf("got %q, %v", cursor, err)
	}
	rec := record(fhirReply(200, map[string]any{"resourceType": "Bundle", "type": "searchset", "total": 0}))
	if _, err := testClient(t, rec).SearchSlot(context.Background(), SearchSlotParams{Page: &cursor}); err != nil {
		t.Fatal(err)
	}
	if got := rec.last().URL.Query().Get("page"); got != "a/b==" {
		t.Errorf("sent %q", got)
	}
}

// sortedQuery is a URL's query as sorted name=value pairs.
func sortedQuery(t *testing.T, raw string) []string {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	var pairs []string
	for name, values := range parsed.Query() {
		for _, v := range values {
			pairs = append(pairs, name+"="+v)
		}
	}
	slices.Sort(pairs)
	return pairs
}

// A search continued with the cursor sends the request the API's own example next link names: the
// example's self search resent with Page.
func TestASearchContinuedWithNextCursorSendsTheRequestTheAPIsNextLinkNames(t *testing.T) {
	// The example as the spec holds it, its next link kept (specAnswer drops it for the other tests).
	var example json.RawMessage
	for _, ex := range specExamples {
		if ex.method == "GET" && templateMatches(ex.path, "/v1/fhir/r4/Slot") {
			example = json.RawMessage(ex.answer)
		}
	}
	if example == nil {
		t.Fatal("the spec has no example Slot search answer")
	}
	var links struct {
		Link []struct{ Relation, URL string } `json:"link"`
	}
	if err := json.Unmarshal(example, &links); err != nil {
		t.Fatal(err)
	}
	linkOf := func(relation string) string {
		for _, l := range links.Link {
			if l.Relation == relation {
				return l.URL
			}
		}
		t.Fatalf("the example has no %s link", relation)
		return ""
	}
	self, err := url.Parse(linkOf("self"))
	if err != nil {
		t.Fatal(err)
	}
	// The search as its caller keeps it: every parameter `self` shows, but the includes the helper sends.
	query := self.Query()
	params := SearchSlotWithIncludesParams{Status: new(query.Get("status")), Start: query["start"], ProviderIdentifier: new(query.Get("provider-identifier"))}
	if n, err := strconv.Atoi(query.Get("_count")); err == nil {
		params.Count = &n
	}
	rec := record(fhirReply(200, example), fhirReply(200, map[string]any{"resourceType": "Bundle", "type": "searchset", "total": 0}))
	c := testClient(t, rec)
	first, err := c.SearchSlotWithIncludes(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := sortedQuery(t, rec.requests[0].URL.String()), sortedQuery(t, linkOf("self")); !slices.Equal(got, want) {
		t.Fatalf("the first request\n%v\nis not the example's self\n%v", got, want)
	}
	cursor, err := NextCursor(first)
	if err != nil || cursor == "" {
		t.Fatalf("got %q, %v", cursor, err)
	}
	params.Page = &cursor
	second, err := c.SearchSlotWithIncludes(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if next, err := NextCursor(second); err != nil || next != "" {
		t.Errorf("got %q, %v on the last page", next, err)
	}
	if got, want := sortedQuery(t, rec.requests[1].URL.String()), sortedQuery(t, linkOf("next")); !slices.Equal(got, want) {
		t.Errorf("the request after the cursor\n%v\nis not the example's next link\n%v", got, want)
	}
}

// Live, on a key: a Slot search continued with NextCursor answers its next Slot, and the cursor with
// a filter changed is refused. Measured 2026-11-24: the page size may change with a cursor, and a
// filter may not.
func TestLiveASlotSearchContinuedWithNextCursor(t *testing.T) {
	if os.Getenv("HAAU3_LIVE_TEST") != "1" || os.Getenv("HAAU3_API_KEY") == "" {
		t.Skip("set HAAU3_LIVE_TEST=1 and HAAU3_API_KEY to call the production API")
	}
	if keyMode(os.Getenv("HAAU3_API_KEY")) == "test" {
		t.Skip("a test key works only in the Sandbox")
	}
	ctx := context.Background()
	client, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	params := SearchSlotWithIncludesParams{Status: new("free"), Start: []string{"ge" + time.Now().UTC().Format(time.RFC3339)}, Count: new(1)}
	first, err := client.SearchSlotWithIncludes(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := NextCursor(first)
	if err != nil || cursor == "" {
		t.Fatalf("got %q, %v", cursor, err)
	}
	params.Page = &cursor
	second, err := client.SearchSlotWithIncludes(ctx, params)
	if err != nil {
		t.Fatal(err)
	}
	one, two := Matches[*fhir.Slot](first), Matches[*fhir.Slot](second)
	if len(one) != 1 || len(two) != 1 || deref(one[0].ID) == deref(two[0].ID) {
		t.Errorf("page two did not answer another Slot: %d, %d", len(one), len(two))
	}
	params.Status = new("free,busy")
	_, err = client.SearchSlotWithIncludes(ctx, params)
	if e := asError(t, err); e.Status != 400 {
		t.Errorf("the cursor with a filter changed answered %d, want 400", e.Status)
	}
}
