package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/neptunedata"
)

const expandQuery = `MATCH (s:Entity)-[:SUBJECT_OF]->(c:Claim)-[:OBJECT]->(o:Entity)
MATCH (d:Document)-[:HAS_SNAPSHOT]->(v:Snapshot)-[:ASSERTS]->(c)
WHERE v.id = d.active_snapshot
  AND (s.id IN $entities OR o.id IN $entities)
  AND ($all_sources OR v.source_uri IN $sources)
RETURN s.id AS subject_id, o.id AS object_id,
       s.name AS subject, c.predicate AS predicate, o.name AS object,
       c.quote AS quote, v.source_uri AS source, v.source_version AS version,
       c.review_status AS review_status, v.unrestricted AS unrestricted,
       v.allowed_groups_json AS allowed_groups_json
ORDER BY c.id
LIMIT 201`

// Expand follows permitted claims for three steps from the retrieved seed claims.
func Expand(ctx context.Context, client QueryClient, seeds []Evidence, groups, allowedSources []string) ([]Evidence, error) {
	result := make([]Evidence, 0, len(seeds))
	seen := make(map[Evidence]bool)
	visited := make(map[string]bool)
	frontier := make([]string, 0)
	add := func(claim Evidence) {
		if seen[claim] {
			return
		}
		seen[claim] = true
		result = append(result, claim)
		for _, id := range []string{claim.SubjectID, claim.ObjectID} {
			if id != "" && !visited[id] {
				visited[id] = true
				frontier = append(frontier, id)
			}
		}
	}
	for _, seed := range seeds {
		if len(allowedSources) == 0 || slices.Contains(allowedSources, seed.Source) {
			add(seed)
		}
	}
	for hop := 0; hop < 3 && len(frontier) > 0; hop++ {
		entities := frontier
		frontier = nil
		sources := allowedSources
		if sources == nil {
			sources = []string{}
		}
		parameters, err := json.Marshal(map[string]any{"entities": entities, "sources": sources, "all_sources": len(sources) == 0})
		if err != nil {
			return nil, fmt.Errorf("encode traversal parameters: %w", err)
		}
		response, err := client.ExecuteOpenCypherQuery(ctx, &neptunedata.ExecuteOpenCypherQueryInput{
			OpenCypherQuery: aws.String(expandQuery), Parameters: aws.String(string(parameters)),
		})
		if err != nil {
			return nil, fmt.Errorf("expand graph evidence: %w", err)
		}
		if response == nil || response.Results == nil {
			return nil, fmt.Errorf("graph returned no traversal payload")
		}
		data, err := response.Results.MarshalSmithyDocument()
		if err != nil {
			return nil, fmt.Errorf("decode traversal result: %w", err)
		}
		var rows []evidenceRow
		if err := json.Unmarshal(data, &rows); err != nil {
			return nil, fmt.Errorf("decode traversal rows: %w", err)
		}
		if len(rows) > 200 {
			return nil, fmt.Errorf("graph traversal exceeds the demo row limit")
		}
		for _, row := range rows {
			if row.Source == "" || row.Version == "" || row.SubjectKey == "" || row.ObjectKey == "" || !row.permitted(groups) {
				continue
			}
			if len(allowedSources) > 0 && !slices.Contains(allowedSources, row.Source) {
				continue
			}
			if !slices.Contains(entities, row.SubjectKey) && !slices.Contains(entities, row.ObjectKey) {
				continue
			}
			row.Evidence.SubjectID, row.Evidence.ObjectID = row.SubjectKey, row.ObjectKey
			add(row.Evidence)
			if len(result) > 150 {
				return nil, fmt.Errorf("graph evidence exceeds the demo claim limit")
			}
		}
	}
	return result, nil
}
