package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path"
	"slices"
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
	key := flag.String("key", "", "Source Markdown, text, or CSV object key")
	prefix := flag.String("prefix", "", "Source S3 folder prefix")
	endpoint := flag.String("endpoint", "", "Neptune HTTPS endpoint")
	model := flag.String("model", "", "Bedrock model or inference profile ID")
	guardrail := flag.String("guardrail", "", "Guardrail ID")
	version := flag.String("guardrail-version", "1", "Numbered Guardrail version")
	flag.Parse()
	for name, value := range map[string]string{"bucket": *bucket, "endpoint": *endpoint, "model": *model, "guardrail": *guardrail} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}

	if (strings.TrimSpace(*key) == "") == (strings.TrimSpace(*prefix) == "") {
		return fmt.Errorf("specify either key or prefix")
	}

	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load AWS configuration: %w", err)
	}

	s3Client := s3.NewFromConfig(cfg)
	keys, err := sourceKeys(ctx, s3Client, *bucket, *key, *prefix)
	if err != nil {
		return err
	}
	generator := ai.Client{Runtime: bedrockruntime.NewFromConfig(cfg), ModelID: *model, GuardrailID: *guardrail, GuardrailVersion: *version}
	client := neptunedata.NewFromConfig(cfg, func(options *neptunedata.Options) { options.BaseEndpoint = aws.String(*endpoint) })
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")

	ingest := func(key string) error {
		ctx, cancel := context.WithTimeout(ctx, 180*time.Second)
		defer cancel()
		source, err := graph.LoadS3(ctx, s3Client, *bucket, key)
		if err != nil {
			return err
		}
		extraction, err := graph.Extract(ctx, generator, source.Text)
		if err != nil {
			return fmt.Errorf("extract source relationships: %w", err)
		}
		result, err := graph.Store(ctx, client, source, extraction, *model)
		if err != nil {
			return err
		}
		return encoder.Encode(result)
	}
	for _, sourceKey := range keys {
		if err := ingest(sourceKey); err != nil {
			return fmt.Errorf("ingest %q: %w", sourceKey, err)
		}
	}
	return nil
}

func sourceKeys(ctx context.Context, client *s3.Client, bucket, key, prefix string) ([]string, error) {
	supported := func(key string) bool {
		return slices.Contains([]string{".txt", ".md", ".csv"}, strings.ToLower(path.Ext(key)))
	}
	if key != "" {
		if !supported(key) {
			return nil, fmt.Errorf("key must identify a Markdown, text, or CSV document")
		}
		return []string{key}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	prefix = strings.TrimRight(prefix, "/") + "/"
	pages := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{Bucket: aws.String(bucket), Prefix: aws.String(prefix)})
	keys := make([]string, 0)
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list S3 sources: %w", err)
		}
		for _, object := range page.Contents {
			key := aws.ToString(object.Key)
			if strings.HasPrefix(key, prefix) && supported(key) {
				keys = append(keys, key)
			}
		}
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("no supported source documents under %q", prefix)
	}
	return keys, nil
}
