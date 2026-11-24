package haau3

import (
	"context"
	"fmt"
	"iter"
	"net/url"

	"github.com/haau3/haau3-go/fhir"
)

// Pages is every page of a FHIR search: the one you have, then each page its `next` link leads to,
// on the client's own host. A link to any other host is refused, so the key is never sent
// elsewhere, and so is a link to a page already read. An error ends the pages, as the last item.
//
//	first, err := client.SearchInsurancePlan(ctx, haau3.SearchInsurancePlanParams{CoverageAreaState: new("NC")})
//	for page, err := range client.Pages(ctx, first) {
//		if err != nil {
//			return err
//		}
//		for _, entry := range page.Entry { ... }
//	}
func (c *Client) Pages(ctx context.Context, first *fhir.Bundle, opts ...Option) iter.Seq2[*fhir.Bundle, error] {
	return func(yield func(*fhir.Bundle, error) bool) {
		s, err := c.settingsFor("GET (the next page)", opts)
		if err != nil {
			yield(nil, err)
			return
		}
		followed := map[string]bool{}
		for page := first; page != nil; {
			if !yield(page, nil) {
				return
			}
			link := nextLink(page)
			if link == "" {
				return
			}
			url, err := c.urlOnBase(link, "", "GET (the next page)")
			if err != nil {
				yield(nil, err)
				return
			}
			name := "GET " + c.pathOf(url)
			if followed[url] {
				yield(nil, &Error{
					Kind: KindNoAnswer, Reason: ReasonUnreadable,
					message: name + " answered with a next link to a page already read, so paging stopped.",
				})
				return
			}
			followed[url] = true
			if page, err = c.nextPage(ctx, url, s, name); err != nil {
				yield(nil, err)
				return
			}
		}
	}
}

// Resources is every resource a FHIR search found, across every page, in order: each entry whose
// search mode is "match", or says none. A resource an `_include` or `_revinclude` brought, and a
// search's own OperationOutcome, are not among them; Pages gives every entry.
//
//	for resource, err := range client.Resources(ctx, first) {
//		if err != nil {
//			return err
//		}
//		plan := resource.(*fhir.InsurancePlan)
//	}
func (c *Client) Resources(ctx context.Context, first *fhir.Bundle, opts ...Option) iter.Seq2[fhir.Resource, error] {
	return func(yield func(fhir.Resource, error) bool) {
		for page, err := range c.Pages(ctx, first, opts...) {
			if err != nil {
				yield(nil, err)
				return
			}
			for _, entry := range page.Entry {
				if !isMatch(entry) {
					continue
				}
				if !yield(entry.Resource, nil) {
					return
				}
			}
		}
	}
}

// NextCursor is the cursor for the page after this one, with no request: the `page` value of the
// page's `next` link, or "" on the last page. It is for a search continued in a later request, such
// as a web page's "Load more", where Pages cannot hold the page between requests.
//
//	// The search, its moment chosen once and kept: the cursor belongs to it.
//	params := haau3.SearchSlotWithIncludesParams{
//		Status: new("free"),
//		Start:  []string{"ge" + time.Now().UTC().Format(time.RFC3339)},
//	}
//	first, err := client.SearchSlotWithIncludes(ctx, params)
//	cursor, err := haau3.NextCursor(first) // "" on the last page
//	// Later, in another request, with the same search:
//	if cursor != "" {
//		params.Page = &cursor
//		second, err := client.SearchSlotWithIncludes(ctx, params)
//	}
//
// A cursor belongs to its search: send it as Page with every other parameter exactly as before. A
// Slot, Schedule or plan search refuses a cursor sent with its search changed (a 400), and that
// includes a parameter the API filled in for you, as the page's `self` link shows: send a Slot
// search's Status and Start yourself when you will page this way. A cursor does not last: the
// provider directory's searches say their cursors expire, and a plan search answers 410 once the
// plans are updated, so start the search again. Pages follows the link itself.
//
// It returns an error when the next link is not a URL, or carries no cursor or two: reading any of
// those as the last page would end a caller's paging with no error.
func NextCursor(page *fhir.Bundle) (string, error) {
	link := nextLink(page)
	// No next link: this is the last page.
	if link == "" {
		return "", nil
	}
	parsed, err := url.Parse(link)
	if err != nil {
		return "", notSent("The page's next link is not a URL, so it names no next page.")
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return "", notSent("The page's next link is not a URL, so it names no next page.")
	}
	cursors := query["page"]
	if len(cursors) > 1 {
		return "", notSent(fmt.Sprintf("The page's next link carries %d page cursors, so which one continues the search is ambiguous.", len(cursors)))
	}
	if len(cursors) == 0 || cursors[0] == "" {
		return "", notSent("The page's next link carries no page cursor, so the next page is reached only by following the link, as Pages does.")
	}
	return cursors[0], nil
}

func nextLink(page *fhir.Bundle) string {
	for _, link := range page.Link {
		if link.Relation != nil && *link.Relation == "next" && link.URL != nil {
			return *link.URL
		}
	}
	return ""
}

func (c *Client) nextPage(ctx context.Context, url string, s *callSettings, name string) (*fhir.Bundle, error) {
	a, err := c.send(ctx, "GET", url, nil, s, name)
	if err != nil {
		return nil, err
	}
	expects := []string{"Bundle"}
	data, jc, e := answerOf(name, expects, a)
	if e != nil {
		return nil, e
	}
	page, err := decodeAs[fhir.Bundle](data)
	if err != nil {
		return nil, unreadable(name, what(expects, data), err, jc, a.status)
	}
	s.fill(a)
	return page, nil
}
