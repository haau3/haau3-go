// Package fakeapi is a stand-in for the haau3 platform API, for the examples' tests: a few plans,
// a search that answers in two pages, and a 404 for any other plan.
package fakeapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
)

// Server serves the stand-in. Close it when done.
func Server() *httptest.Server {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/fhir+json")
		w.Header().Set("X-Request-Id", "6b1f3c2e-4d5a-4e6f-8a9b-0c1d2e3f4a5b")
		switch {
		case r.URL.Path == "/v1/fhir/r4/InsurancePlan/H2406-013-000":
			_, _ = fmt.Fprint(w, `{"resourceType":"InsurancePlan","id":"H2406-013-000","name":"A plan","status":"active"}`)
		case r.URL.Path == "/v1/fhir/r4/InsurancePlan" && r.URL.Query().Get("page") == "":
			_, _ = fmt.Fprintf(w, `{"resourceType":"Bundle","type":"searchset","link":[{"relation":"next","url":"%s/v1/fhir/r4/InsurancePlan?page=2"}],`+
				`"entry":[{"resource":{"resourceType":"InsurancePlan","id":"P1","name":"One, with a comma","status":"active"},"search":{"mode":"match"}}]}`, srv.URL)
		case r.URL.Path == "/v1/fhir/r4/InsurancePlan":
			_, _ = fmt.Fprint(w, `{"resourceType":"Bundle","type":"searchset","entry":[`+
				`{"resource":{"resourceType":"InsurancePlan","id":"P2","name":"Two","status":"draft"},"search":{"mode":"match"}},`+
				`{"resource":{"resourceType":"Organization","id":"O1"},"search":{"mode":"include"}}]}`)
		case strings.HasPrefix(r.URL.Path, "/v1/fhir/r4/InsurancePlan/"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprint(w, `{"resourceType":"OperationOutcome","issue":[{"severity":"error","code":"not-found"}]}`)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	return srv
}
