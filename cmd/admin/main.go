package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockagent"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/neptunedata"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/olaysco/org-brain-aws/internal/admin"
	"github.com/olaysco/org-brain-aws/internal/ai"
	"github.com/olaysco/org-brain-aws/internal/graph"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
func run() error {
	bucket := flag.String("bucket", "", "Source S3 bucket")
	prefix := flag.String("prefix", "sources/", "Source S3 folder prefix")
	kb := flag.String("kb", "", "Knowledge base ID")
	dataSource := flag.String("data-source", "", "Knowledge base data source ID")
	endpoint := flag.String("graph-endpoint", "", "Neptune HTTPS endpoint")
	model := flag.String("model", "eu.anthropic.claude-haiku-4-5-20251001-v1:0", "Extraction model ID")
	guardrail := flag.String("guardrail", "", "Guardrail ID")
	version := flag.String("guardrail-version", "3", "Numbered Guardrail version")
	address := flag.String("listen", "127.0.0.1:8085", "Local admin HTTP address")
	flag.Parse()
	for name, value := range map[string]string{"bucket": *bucket, "kb": *kb, "data-source": *dataSource, "graph-endpoint": *endpoint, "guardrail": *guardrail} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	host, _, err := net.SplitHostPort(*address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("admin server must listen on a loopback address")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return err
	}
	store := s3.NewFromConfig(cfg)
	graphClient := neptunedata.NewFromConfig(cfg, func(o *neptunedata.Options) { o.BaseEndpoint = aws.String(*endpoint) })
	generator := ai.Client{Runtime: bedrockruntime.NewFromConfig(cfg), ModelID: *model, GuardrailID: *guardrail, GuardrailVersion: *version}
	service := admin.Service{S3: store, Bedrock: bedrockagent.NewFromConfig(cfg), Bucket: *bucket, Prefix: strings.TrimRight(*prefix, "/") + "/", KnowledgeBaseID: *kb, DataSourceID: *dataSource}
	service.Ingest = func(ctx context.Context, key string) (int, error) {
		ctx, cancel := context.WithTimeout(ctx, 180*time.Second)
		defer cancel()
		source, err := graph.LoadS3(ctx, store, *bucket, key)
		if err != nil {
			return 0, err
		}
		extraction, err := graph.Extract(ctx, generator, source.Text)
		if err != nil {
			return 0, err
		}
		result, err := graph.Store(ctx, graphClient, source, extraction, *model)
		return result.Claims, err
	}
	server := &http.Server{Addr: *address, Handler: admin.Handler(ctx, service), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 60 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("Admin page: http://%s", *address)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
