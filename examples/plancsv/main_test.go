package main

import (
	"context"
	"strings"
	"testing"

	"github.com/haau3/haau3-go"
	"github.com/haau3/haau3-go/examples/fakeapi"
)

func TestEveryPlanOfEveryPage(t *testing.T) {
	api := fakeapi.Server()
	defer api.Close()
	client, err := haau3.NewClient(haau3.WithAPIKey("test"), haau3.WithBaseURL(api.URL))
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	n, err := writePlans(context.Background(), client, "VT", &out)
	if err != nil || n != 2 {
		t.Fatalf("%d %v", n, err)
	}
	if out.String() != "id,name,status\nP1,\"One, with a comma\",active\nP2,Two,draft\n" {
		t.Errorf("%q", out.String())
	}
}
