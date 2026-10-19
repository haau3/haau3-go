package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/haau3/haau3-go"
	"github.com/haau3/haau3-go/examples/fakeapi"
)

func get(t *testing.T, h http.Handler, path string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	body, _ := io.ReadAll(rec.Result().Body)
	return rec.Code, strings.TrimSpace(string(body))
}

func TestTheRoute(t *testing.T) {
	api := fakeapi.Server()
	defer api.Close()
	client, err := haau3.NewClient(haau3.WithAPIKey("test"), haau3.WithBaseURL(api.URL))
	if err != nil {
		t.Fatal(err)
	}
	h := handler(client)
	if code, body := get(t, h, "/plans/H2406-013-000"); code != 200 || body != `{"id":"H2406-013-000","name":"A plan"}` {
		t.Errorf("%d %s", code, body)
	}
	if code, _ := get(t, h, "/plans/NOPE"); code != 404 {
		t.Errorf("%d", code)
	}
}
