package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/neptunedata"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/olaysco/org-brain-aws/internal/ai"
	"github.com/olaysco/org-brain-aws/internal/graph"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	bucket := flag.String("bucket", "", "Source S3 bucket")
	key := flag.String("key", "", "Source Markdown or text object key")
	endpoint := flag.String("endpoint", "", "Neptune HTTPS endpoint")
	model := flag.String("model", "", "Bedrock model or inference profile ID")
	guardrail := flag.String("guardrail", "", "Guardrail ID")
	version := flag.String("guardrail-version", "1", "Numbered Guardrail version")
	flag.Parse()
	for name, value := range map[string]string{"bucket": *bucket, "key": *key, "model": *model, "guardrail": *guardrail} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}

	extension := strings.ToLower(path.Ext(*key))
	if extension != ".md" && extension != ".txt" {
		return fmt.Errorf("key must identify a Markdown or text document")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load AWS configuration: %w", err)
	}

	source, err := graph.LoadS3(ctx, s3.NewFromConfig(cfg), *bucket, *key)
	if err != nil {
		return err
	}

	generator := ai.Client{Runtime: bedrockruntime.NewFromConfig(cfg), ModelID: *model, GuardrailID: *guardrail, GuardrailVersion: *version}
	extraction, err := graph.Extract(ctx, generator, source.Text)
	if err != nil {
		return fmt.Errorf("extract source relationships: %w", err)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")

	client := neptunedata.NewFromConfig(cfg, func(options *neptunedata.Options) { options.BaseEndpoint = aws.String(*endpoint) })
	result, err := graph.Store(ctx, client, source, extraction, *model)
	if err != nil {
		return err
	}
	return encoder.Encode(result)
}
