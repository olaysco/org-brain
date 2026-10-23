package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/neptunedata"
)

type SourceRef struct {
	URI     string
	Version string
}

type Evidence struct {
	SubjectID    string `json:"-"`
	ObjectID     string `json:"-"`
	Subject      string `json:"subject"`
	Predicate    string `json:"predicate"`
	Object       string `json:"object"`
	Quote        string `json:"quote"`
	Source       string `json:"source"`
	Version      string `json:"version"`
	ReviewStatus string `json:"review_status"`
}

type evidenceRow struct {
	Evidence
	SubjectKey        string `json:"subject_id"`
	ObjectKey         string `json:"object_id"`
	Unrestricted      *bool  `json:"unrestricted"`
	AllowedGroupsJSON string `json:"allowed_groups_json"`
}

const lookupQuery = `MATCH (d:Document {id: $source})-[:HAS_SNAPSHOT]->(v:Snapshot)-[:ASSERTS]->(c:Claim)
WHERE v.id = d.active_snapshot AND v.source_version = $version
MATCH (s:Entity)-[:SUBJECT_OF]->(c)-[:OBJECT]->(o:Entity)
RETURN s.id AS subject_id, o.id AS object_id,
       s.name AS subject, c.predicate AS predicate, o.name AS object,
       c.quote AS quote, v.source_uri AS source, v.source_version AS version,
       c.review_status AS review_status, v.unrestricted AS unrestricted,
       v.allowed_groups_json AS allowed_groups_json
ORDER BY c.id
LIMIT 50`

// Lookup returns claims only for supplied source versions and permitted groups.
func Lookup(ctx context.Context, client QueryClient, sources []SourceRef, groups []string) ([]Evidence, error) {
	evidence := make([]Evidence, 0)
	seen := make(map[SourceRef]bool)
	for _, source := range sources {
		if source.URI == "" || source.Version == "" || seen[source] {
			continue
		}
		seen[source] = true
		parameters, err := json.Marshal(map[string]string{"source": source.URI, "version": source.Version})
		if err != nil {
			return nil, fmt.Errorf("encode graph lookup: %w", err)
		}
		response, err := client.ExecuteOpenCypherQuery(ctx, &neptunedata.ExecuteOpenCypherQueryInput{
			OpenCypherQuery: aws.String(lookupQuery), Parameters: aws.String(string(parameters)),
		})
		if err != nil {
			return nil, fmt.Errorf("retrieve graph evidence: %w", err)
		}
		if response == nil || response.Results == nil {
			return nil, fmt.Errorf("graph returned no result payload")
		}
		data, err := response.Results.MarshalSmithyDocument()
		if err != nil {
			return nil, fmt.Errorf("decode graph result: %w", err)
		}
		var rows []evidenceRow
		if err := json.Unmarshal(data, &rows); err != nil {
			return nil, fmt.Errorf("decode graph rows: %w", err)
		}
		for _, row := range rows {
			if row.Source != source.URI || row.Version != source.Version {
				continue
			}
			if !row.permitted(groups) {
				continue
			}
			row.Evidence.SubjectID = row.SubjectKey
			row.Evidence.ObjectID = row.ObjectKey
			evidence = append(evidence, row.Evidence)
		}
	}
	return evidence, nil
}

func (row evidenceRow) permitted(groups []string) bool {
	if row.Unrestricted == nil {
		return false
	}
	var allowed []string
	if err := json.Unmarshal([]byte(row.AllowedGroupsJSON), &allowed); err != nil || allowed == nil {
		return false
	}
	if *row.Unrestricted {
		return true
	}
	for _, group := range groups {
		if strings.TrimSpace(group) != "" && slices.Contains(allowed, group) {
			return true
		}
	}
	return false
}
