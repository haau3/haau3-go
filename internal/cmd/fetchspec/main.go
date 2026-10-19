// Command fetchspec pins the published spec: it fetches https://api.haau3.com/openapi.json and
// writes it to openapi/openapi.json, indented so a change reads as a diff, with the ETag it was
// served with in openapi/etag. When the pinned ETag still matches, the API answers 304 and nothing
// is written.
//
//	go run ./internal/cmd/fetchspec        then   go generate ./...
//
// HAAU3_SPEC_URL fetches from somewhere else, such as a local platform API.
package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fetchspec:", err)
		os.Exit(1)
	}
}

// indent writes a spec as JSON.stringify(document, null, 2) writes it, keeping its keys' order
// and its numbers as the API wrote them, so every SDK pins the same bytes.
func indent(body []byte) ([]byte, error) {
	var out bytes.Buffer
	if err := json.Indent(&out, bytes.TrimSpace(body), "", "  "); err != nil {
		return nil, err
	}
	out.WriteString("\n")
	return out.Bytes(), nil
}

func run() error {
	url := cmp.Or(os.Getenv("HAAU3_SPEC_URL"), "https://api.haau3.com/openapi.json")
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	pinned, _ := os.ReadFile("openapi/etag")
	if etag := strings.TrimSpace(string(pinned)); etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotModified {
		fmt.Printf("Unchanged: %s still answers ETag %s.\n", url, strings.TrimSpace(string(pinned)))
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %d", url, resp.StatusCode)
	}
	etag := resp.Header.Get("ETag")
	if etag == "" {
		return fmt.Errorf("%s sent no ETag", url)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var doc struct {
		OpenAPI string `json:"openapi"`
		Info    struct {
			Version string `json:"version"`
		} `json:"info"`
	}
	if err := json.Unmarshal(body, &doc); err != nil || !strings.HasPrefix(doc.OpenAPI, "3.1") {
		return fmt.Errorf("%s is not an OpenAPI 3.1 document", url)
	}
	written, err := indent(body)
	if err != nil {
		return err
	}
	if err := os.WriteFile("openapi/openapi.json", written, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile("openapi/etag", []byte(etag+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Printf("Pinned %s (ETag %s) from %s.\n", doc.Info.Version, etag, url)
	return nil
}
