// Command plancsv writes every Medicare Advantage and ACA Marketplace plan sold in one state to CSV:
// each plan a search finds, across every page.
//
//	HAAU3_API_KEY=... go run ./plancsv VT > plans.csv
package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/haau3/haau3-go"
	"github.com/haau3/haau3-go/fhir"
)

// writePlans writes the plans sold in state to out, one row each, and says how many.
func writePlans(ctx context.Context, client *haau3.Client, state string, out io.Writer) (int, error) {
	first, err := client.SearchInsurancePlan(ctx, haau3.SearchInsurancePlanParams{CoverageAreaState: &state, Count: new(100)})
	if err != nil {
		return 0, err
	}
	w := csv.NewWriter(out)
	if err := w.Write([]string{"id", "name", "status"}); err != nil {
		return 0, err
	}
	n := 0
	for resource, err := range client.Resources(ctx, first) {
		if err != nil {
			return n, err
		}
		plan, ok := resource.(*fhir.InsurancePlan)
		if !ok {
			continue
		}
		if err := w.Write([]string{text(plan.ID), text(plan.Name), text(plan.Status)}); err != nil {
			return n, err
		}
		n++
	}
	w.Flush()
	return n, w.Error()
}

func text(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: plancsv <state>, such as VT")
	}
	client, err := haau3.NewClient() // reads HAAU3_API_KEY
	if err != nil {
		log.Fatal(err)
	}
	n, err := writePlans(context.Background(), client, os.Args[1], os.Stdout)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Fprintf(os.Stderr, "%d plans\n", n)
}
