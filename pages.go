package haau3

import (
	"context"
	"iter"

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
//
// Scheduling's searches page with Limit and Offset instead, and answer with a Total.
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
				if entry.Resource == nil || (entry.Search != nil && entry.Search.Mode != nil && *entry.Search.Mode != "match") {
					continue
				}
				if !yield(entry.Resource, nil) {
					return
				}
			}
		}
	}
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
