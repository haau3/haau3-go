package main

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

// The pinned spec, as the API serves it (compact), is written back byte for byte as the other
// SDKs pin it with JSON.stringify(document, null, 2).
func TestTheSpecIsWrittenAsTheOtherSDKsPinIt(t *testing.T) {
	pinned, err := os.ReadFile("../../../openapi/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var served bytes.Buffer
	if err := json.Compact(&served, pinned); err != nil {
		t.Fatal(err)
	}
	written, err := indent(served.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(written, pinned) {
		t.Error("the spec is not written as it is pinned")
	}
}
