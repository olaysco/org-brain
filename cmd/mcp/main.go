package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockagentruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockagentruntime/types"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/neptunedata"
	"github.com/olaysco/org-brain-aws/internal/ai"
	"github.com/olaysco/org-brain-aws/internal/mcpserver"
	"github.com/olaysco/org-brain-aws/internal/retrieval"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
func run() error {
	kb := flag.String("kb", "", "Knowledge base ID")
	graphEndpoint := flag.String("graph-endpoint", "", "Neptune HTTPS endpoint")
	guardrail := flag.String("guardrail", "", "Guardrail ID")
	version := flag.String("guardrail-version", "2", "Numbered Guardrail version")
	group := flag.String("group", "sales", "Fixed demo group")
	source := flag.String("source", "", "Comma-separated allowed S3 source URIs")
	address := flag.String("listen", "127.0.0.1:8080", "Local HTTP address")
	flag.Parse()
	for name, value := range map[string]string{"kb": *kb, "graph-endpoint": *graphEndpoint, "guardrail": *guardrail, "source": *source} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}

	allowedSources := make([]string, 0)
	for _, uri := range strings.Split(*source, ",") {
		uri = strings.TrimSpace(uri)
		if !strings.HasPrefix(uri, "s3://") {
			return fmt.Errorf("source must contain S3 URIs separated by commas")
		}
		allowedSources = append(allowedSources, uri)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load AWS configuration: %w", err)
	}
	service := retrieval.Service{
		Retriever:       bedrockagentruntime.NewFromConfig(cfg),
		Guardrails:      ai.Client{Runtime: bedrockruntime.NewFromConfig(cfg), GuardrailID: *guardrail, GuardrailVersion: *version},
		Graph:           neptunedata.NewFromConfig(cfg, func(o *neptunedata.Options) { o.BaseEndpoint = aws.String(*graphEndpoint) }),
		KnowledgeBaseID: *kb, Groups: []string{strings.TrimSpace(*group)}, AllowedSources: allowedSources, SearchType: types.SearchTypeSemantic,
	}
	server := &http.Server{Addr: *address, Handler: mcpserver.Handler(service, 40), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 75 * time.Second, IdleTimeout: 60 * time.Second}

	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("MCP demo listening at http://%s/mcp; fixed source list and group, 40 searches", *address)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}
