package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
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
	gin.SetMode(gin.TestMode)
	api := fakeapi.Server()
	defer api.Close()
	client, err := haau3.NewClient(haau3.WithAPIKey("test"), haau3.WithBaseURL(api.URL))
	if err != nil {
		t.Fatal(err)
	}
	r := router(client)
	if code, body := get(t, r, "/plans/H2406-013-000"); code != 200 || body != `{"id":"H2406-013-000","name":"A plan"}` {
		t.Errorf("%d %s", code, body)
	}
	if code, _ := get(t, r, "/plans/NOPE"); code != 404 {
		t.Errorf("%d", code)
	}
}

// What this example says, measured on the Gin this module pins: a *gin.Context is not cancelled
// with its request unless the engine sets ContextWithFallback, and c.Request.Context() is.
func TestGinsOwnContextIsNotTheRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, fallback := range []bool{false, true} {
		r := gin.New()
		r.ContextWithFallback = fallback
		var ginDone, requestDone <-chan struct{}
		r.GET("/", func(c *gin.Context) { ginDone, requestDone = c.Done(), c.Request.Context().Done() })
		ctx, cancel := context.WithCancel(context.Background())
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil).WithContext(ctx))
		cancel()
		if !closed(requestDone) {
			t.Errorf("fallback %t: the request's context was not cancelled", fallback)
		}
		if closed(ginDone) != fallback {
			t.Errorf("fallback %t: gin's own context cancelled: %t", fallback, closed(ginDone))
		}
	}
}

func closed(done <-chan struct{}) bool {
	if done == nil {
		return false // a nil channel is never closed
	}
	select {
	case <-done:
		return true
	case <-time.After(100 * time.Millisecond):
		return false
	}
}
