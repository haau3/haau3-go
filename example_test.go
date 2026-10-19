package haau3_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/haau3/haau3-go"
	"github.com/haau3/haau3-go/fhir"
)

func ExampleNewClient() {
	client, err := haau3.NewClient(haau3.WithTimeout(20 * time.Second)) // reads HAAU3_API_KEY
	if err != nil {
		slog.Error("no client", "error", err)
		return
	}
	fmt.Println(client)
}

func ExampleClient_ReadInsurancePlan() {
	client, err := haau3.NewClient()
	if err != nil {
		return
	}
	plan, err := client.ReadInsurancePlan(context.Background(), "H2406-013-000")
	var e *haau3.Error
	switch {
	case errors.As(err, &e) && e.Kind == haau3.KindRefused:
		fmt.Println("refused:", e.Status, e.RequestID)
	case err != nil:
		slog.Warn("the plan read failed", "error", err)
	case plan.Name != nil:
		fmt.Println(*plan.Name)
	}
}

func ExampleClient_SearchSlots() {
	client, err := haau3.NewClient()
	if err != nil {
		return
	}
	found, err := client.SearchSlots(context.Background(), haau3.SearchSlotsParams{City: new("Boston"), State: new("MA"), Limit: new(10)})
	if err != nil {
		return
	}
	fmt.Println(found.Total, len(found.Slots))
}

func ExampleClient_Resources() {
	client, err := haau3.NewClient()
	if err != nil {
		return
	}
	ctx := context.Background()
	first, err := client.SearchInsurancePlan(ctx, haau3.SearchInsurancePlanParams{CoverageAreaState: new("VT")})
	if err != nil {
		return
	}
	for resource, err := range client.Resources(ctx, first) {
		if err != nil {
			return
		}
		if plan, ok := resource.(*fhir.InsurancePlan); ok {
			fmt.Println(*plan.ID)
		}
	}
}

func ExampleClient_Batch() {
	client, err := haau3.NewClient()
	if err != nil {
		return
	}
	plan := haau3.Calls.ReadInsurancePlan("H2406-013-000")
	counties := haau3.Calls.ListInsurancePlanCounties(haau3.ListInsurancePlanCountiesParams{State: "NC"})
	if err := client.Batch(context.Background(), []haau3.Entry{plan, counties}); err != nil {
		return // the batch as a whole failed
	}
	if p, err := plan.Result(); err == nil {
		fmt.Println(*p.ID)
	}
}

func ExampleWithResponseInfo() {
	client, err := haau3.NewClient()
	if err != nil {
		return
	}
	var info haau3.ResponseInfo
	if _, err := client.ReadInsurancePlan(context.Background(), "H2406-013-000", haau3.WithResponseInfo(&info)); err != nil {
		return
	}
	fmt.Println(info.RequestID, info.Duration, info.Attempts)
}
