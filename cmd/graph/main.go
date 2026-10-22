package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/neptunedata"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	endpoint := flag.String("endpoint", "", "Neptune HTTPS endpoint")
	source := flag.String("source", "", "Inspect active claims for an S3 source URI")
	flag.Parse()

	if *endpoint == "" {
		return fmt.Errorf("endpoint is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load AWS configuration: %w", err)
	}

	client := neptunedata.NewFromConfig(
		cfg,
		func(options *neptunedata.Options) {
			options.BaseEndpoint = aws.String(*endpoint)
		},
	)

	query := "RETURN 1 AS connected"
	parameters := "{}"
	if *source != "" {
		query = `MATCH (d:Document {id: $source})-[:HAS_SNAPSHOT]->(v:Snapshot)-[:ASSERTS]->(c:Claim)
WHERE v.id = d.active_snapshot
MATCH (s:Entity)-[:SUBJECT_OF]->(c)-[:OBJECT]->(o:Entity)
RETURN s.name AS subject, c.predicate AS predicate, o.name AS object,
       c.quote AS quote, v.source_uri AS source, v.source_version AS version
LIMIT 50`
		data, err := json.Marshal(map[string]string{"source": *source})
		if err != nil {
			return fmt.Errorf("encode query parameters: %w", err)
		}
		parameters = string(data)
	}
	response, err := client.ExecuteOpenCypherQuery(
		ctx,
		&neptunedata.ExecuteOpenCypherQueryInput{
			OpenCypherQuery: aws.String(query),
			Parameters:      aws.String(parameters),
		},
	)
	if err != nil {
		return fmt.Errorf("query Neptune: %w", err)
	}

	var results any
	if err := response.Results.UnmarshalSmithyDocument(&results); err != nil {
		return fmt.Errorf("decode graph results: %w", err)
	}

	return json.NewEncoder(os.Stdout).Encode(results)
}
