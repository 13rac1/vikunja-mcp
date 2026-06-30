package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const version = "0.1.0"

func main() {
	httpAddr := flag.String("http", "", "serve over Streamable HTTP at this address (e.g. :8080) instead of stdio")
	flag.Parse()

	vikunjaURL := os.Getenv("VIKUNJA_URL")
	if vikunjaURL == "" {
		fmt.Fprintln(os.Stderr, "error: VIKUNJA_URL environment variable is required")
		os.Exit(1)
	}
	vikunjaToken := os.Getenv("VIKUNJA_TOKEN")
	if vikunjaToken == "" {
		fmt.Fprintln(os.Stderr, "error: VIKUNJA_TOKEN environment variable is required")
		os.Exit(1)
	}

	client := NewClient(vikunjaURL, vikunjaToken)
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "vikunja-mcp",
		Version: version,
	}, nil)

	registerUserTools(server, client)
	registerProjectTools(server, client)
	registerTaskTools(server, client)
	registerLabelTools(server, client)
	registerCommentTools(server, client)
	registerAssigneeTools(server, client)
	// Time entry tools require a Vikunja license (time tracking feature).
	// They are not registered here because most instances won't have it.
	registerPowerQueryTools(server, client)
	registerRelationTools(server, client)
	registerViewTools(server, client)
	registerBatchTools(server, client)
	registerAttachmentTools(server, client)
	registerResources(server, client)
	server.AddReceivingMiddleware(errorLoggingMiddleware)

	if *httpAddr != "" {
		handler := mcp.NewStreamableHTTPHandler(
			func(_ *http.Request) *mcp.Server { return server },
			nil,
		)
		http.Handle("/mcp", handler)
		log.Printf("vikunja-mcp serving on %s/mcp", *httpAddr)
		srv := &http.Server{
			Addr:              *httpAddr,
			ReadHeaderTimeout: 10 * time.Second,
		}
		log.Fatal(srv.ListenAndServe())
	} else {
		if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			log.Fatal(err)
		}
	}
}
