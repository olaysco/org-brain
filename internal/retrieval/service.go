package retrieval

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockagentruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockagentruntime/document"
	"github.com/aws/aws-sdk-go-v2/service/bedrockagentruntime/types"
	runtimeTypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/olaysco/org-brain-aws/internal/graph"
)

type Retriever interface {
	Retrieve(context.Context, *bedrockagentruntime.RetrieveInput, ...func(*bedrockagentruntime.Options)) (*bedrockagentruntime.RetrieveOutput, error)
}

type Checker interface {
	Check(context.Context, string, runtimeTypes.GuardrailContentSource) error
}

type Document struct {
	Text     string         `json:"text"`
	Source   string         `json:"source"`
	Score    *float64       `json:"score,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type Evidence struct {
	Documents          []Document       `json:"documents"`
	RelationshipClaims []graph.Evidence `json:"relationship_claims"`
	GraphScope         string           `json:"graph_scope,omitempty"`
}

type Service struct {
	Retriever       Retriever
	Guardrails      Checker
	Graph           graph.QueryClient
	KnowledgeBaseID string
	Groups          []string
	AllowedSources  []string
	SearchType      types.SearchType
}

func (s Service) Search(ctx context.Context, query string) (Evidence, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 1000 {
		return Evidence{}, fmt.Errorf("query must contain 1 to 1000 bytes")
	}
	if err := s.Guardrails.Check(ctx, query, runtimeTypes.GuardrailContentSourceInput); err != nil {
		return Evidence{}, fmt.Errorf("question check: %w", err)
	}
	filter := s.accessFilter()
	response, err := s.Retriever.Retrieve(ctx, &bedrockagentruntime.RetrieveInput{
		KnowledgeBaseId: aws.String(s.KnowledgeBaseID), RetrievalQuery: &types.KnowledgeBaseQuery{Text: aws.String(query)},
		RetrievalConfiguration: &types.KnowledgeBaseRetrievalConfiguration{VectorSearchConfiguration: &types.KnowledgeBaseVectorSearchConfiguration{
			Filter: filter, NumberOfResults: aws.Int32(5), OverrideSearchType: s.SearchType,
		}},
	})
	if err != nil {
		return Evidence{}, fmt.Errorf("retrieve documents: %w", err)
	}
	if response == nil {
		return Evidence{}, fmt.Errorf("retrieval returned no response")
	}
	evidence := Evidence{Documents: make([]Document, 0), RelationshipClaims: make([]graph.Evidence, 0)}
	sources := make([]graph.SourceRef, 0)
	for _, result := range response.RetrievalResults {
		if result.Location == nil || result.Location.S3Location == nil || result.Content == nil {
			continue
		}
		uri := aws.ToString(result.Location.S3Location.Uri)
		if len(s.AllowedSources) > 0 && !slices.Contains(s.AllowedSources, uri) {
			continue
		}
		metadata := make(map[string]any)
		// Only source fields used by the application enter the returned evidence.
		for _, key := range []string{"document_id", "version", "status", "owner", "allowed_groups", "unrestricted"} {
			if value, ok := result.Metadata[key]; ok {
				data, err := value.MarshalSmithyDocument()
				if err != nil {
					return Evidence{}, fmt.Errorf("decode metadata: %w", err)
				}
				var decoded any
				if err := json.Unmarshal(data, &decoded); err != nil {
					return Evidence{}, fmt.Errorf("decode metadata: %w", err)
				}
				metadata[key] = decoded
			}
		}
		if !permitted(metadata, s.Groups) {
			continue
		}
		evidence.Documents = append(evidence.Documents, Document{Text: aws.ToString(result.Content.Text), Source: uri, Score: result.Score, Metadata: metadata})
		version, _ := metadata["version"].(string)
		sources = append(sources, graph.SourceRef{URI: uri, Version: version})
	}
	if s.Graph != nil {
		evidence.RelationshipClaims, err = graph.Lookup(ctx, s.Graph, sources, s.Groups)
		if err != nil {
			return Evidence{}, err
		}
		evidence.RelationshipClaims, err = graph.Expand(ctx, s.Graph, evidence.RelationshipClaims, s.Groups, s.AllowedSources)
		if err != nil {
			return Evidence{}, err
		}
		evidence.GraphScope = "Related evidence from active permitted sources, within three relationship steps of retrieved claims. Results do not establish complete coverage or current source status."
	}
	data, err := json.Marshal(evidence)
	if err != nil {
		return Evidence{}, err
	}
	if len(data) > 48000 {
		return Evidence{}, fmt.Errorf("evidence exceeds the demo response limit")
	}
	if len(evidence.Documents) > 0 {
		if err := s.Guardrails.Check(ctx, string(data), runtimeTypes.GuardrailContentSourceOutput); err != nil {
			return Evidence{}, fmt.Errorf("evidence check: %w", err)
		}
	}
	return evidence, nil
}

func (s Service) accessFilter() types.RetrievalFilter {
	choices := []types.RetrievalFilter{&types.RetrievalFilterMemberEquals{Value: types.FilterAttribute{Key: aws.String("unrestricted"), Value: document.NewLazyDocument(true)}}}
	for _, group := range s.Groups {
		if group != "" {
			choices = append(choices, &types.RetrievalFilterMemberListContains{Value: types.FilterAttribute{Key: aws.String("allowed_groups"), Value: document.NewLazyDocument(group)}})
		}
	}
	var access types.RetrievalFilter = choices[0]
	if len(choices) > 1 {
		access = &types.RetrievalFilterMemberOrAll{Value: choices}
	}
	if len(s.AllowedSources) > 0 {
		var scope types.RetrievalFilter = &types.RetrievalFilterMemberIn{Value: types.FilterAttribute{
			Key: aws.String("x-amz-bedrock-kb-source-uri"), Value: document.NewLazyDocument(s.AllowedSources),
		}}
		return &types.RetrievalFilterMemberAndAll{Value: []types.RetrievalFilter{access, scope}}
	}
	return access
}

func permitted(metadata map[string]any, groups []string) bool {
	unrestricted, ok := metadata["unrestricted"].(bool)
	if !ok {
		return false
	}
	allowed, ok := metadata["allowed_groups"].([]any)
	if !ok {
		return false
	}
	if unrestricted {
		return true
	}
	for _, value := range allowed {
		group, ok := value.(string)
		if ok && group != "" && slices.Contains(groups, group) {
			return true
		}
	}
	return false
}
