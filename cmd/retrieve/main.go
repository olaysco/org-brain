package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockagentruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockagentruntime/document"
	"github.com/aws/aws-sdk-go-v2/service/bedrockagentruntime/types"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	kbID := flag.String("kb", "", "Knowledge base ID")
	query := flag.String("query", "", "Search question")
	group := flag.String("group", "", "Simulated user group")
	searchType := flag.String("search", "SEMANTIC", "SEMANTIC or HYBRID")
	flag.Parse()

	if *kbID == "" || strings.TrimSpace(*query) == "" {
		return fmt.Errorf("kb and query are required")
	}

	mode := strings.ToUpper(strings.TrimSpace(*searchType))
	if mode != "SEMANTIC" && mode != "HYBRID" {
		return fmt.Errorf("search must be SEMANTIC or HYBRID")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load AWS configuration: %w", err)
	}

	var accessFilter types.RetrievalFilter = &types.RetrievalFilterMemberEquals{
		Value: types.FilterAttribute{
			Key:   aws.String("unrestricted"),
			Value: document.NewLazyDocument(true),
		},
	}

	if value := strings.TrimSpace(*group); value != "" {
		accessFilter = &types.RetrievalFilterMemberOrAll{
			Value: []types.RetrievalFilter{
				accessFilter,
				&types.RetrievalFilterMemberListContains{
					Value: types.FilterAttribute{
						Key:   aws.String("allowed_groups"),
						Value: document.NewLazyDocument(value),
					},
				},
			},
		}
	}

	client := bedrockagentruntime.NewFromConfig(cfg)

	response, err := client.Retrieve(ctx, &bedrockagentruntime.RetrieveInput{
		KnowledgeBaseId: aws.String(*kbID),
		RetrievalQuery: &types.KnowledgeBaseQuery{
			Text: aws.String(strings.TrimSpace(*query)),
		},
		RetrievalConfiguration: &types.KnowledgeBaseRetrievalConfiguration{
			VectorSearchConfiguration: &types.KnowledgeBaseVectorSearchConfiguration{
				Filter:             accessFilter,
				NumberOfResults:    aws.Int32(5),
				OverrideSearchType: types.SearchType(mode),
			},
		},
	})
	if err != nil {
		return fmt.Errorf("retrieve evidence: %w", err)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(response.RetrievalResults); err != nil {
		return fmt.Errorf("encode evidence: %w", err)
	}

	return nil
}
