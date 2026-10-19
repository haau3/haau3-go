// Command gin serves one route with Gin: GET /plans/:id answers a Medicare Advantage or ACA
// Marketplace plan's id and name, read from the haau3 platform API.
//
// One client serves the whole server. Each call takes c.Request.Context(), the request's own
// context. Gin's *gin.Context is a context.Context too, but it is not cancelled when the request
// ends unless the engine sets ContextWithFallback, so a call given it would run on after the
// client went away.
//
//	HAAU3_API_KEY=... go run ./gin
//	curl localhost:8080/plans/H2406-013-000
package main

import (
	"errors"
	"log"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/haau3/haau3-go"
)

func router(client *haau3.Client) *gin.Engine {
	r := gin.New()
	r.GET("/plans/:id", func(c *gin.Context) {
		plan, err := client.ReadInsurancePlan(c.Request.Context(), c.Param("id"))
		var e *haau3.Error
		switch {
		case errors.As(err, &e) && e.Kind == haau3.KindRefused && e.Status == http.StatusNotFound:
			c.JSON(http.StatusNotFound, gin.H{"error": "no such plan"})
			return
		case err != nil:
			// The error logs safely: never the body the API answered with.
			slog.Error("reading a plan", "error", err)
			c.JSON(http.StatusBadGateway, gin.H{"error": "the plan could not be read"})
			return
		}
		name := ""
		if plan.Name != nil {
			name = *plan.Name
		}
		c.JSON(http.StatusOK, gin.H{"id": c.Param("id"), "name": name})
	})
	return r
}

func main() {
	client, err := haau3.NewClient() // reads HAAU3_API_KEY
	if err != nil {
		log.Fatal(err)
	}
	log.Fatal(router(client).Run(":8080"))
}
