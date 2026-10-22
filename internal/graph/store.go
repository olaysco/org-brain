package graph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/neptunedata"
)

type QueryClient interface {
	ExecuteOpenCypherQuery(context.Context, *neptunedata.ExecuteOpenCypherQueryInput, ...func(*neptunedata.Options)) (*neptunedata.ExecuteOpenCypherQueryOutput, error)
}

func digest(value any) string {
	data, _ := json.Marshal(value)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
func entityID(entity Entity) string {
	return digest([]string{entity.Type, strings.ToLower(strings.Join(strings.Fields(entity.Name), " "))})
}

// Old snapshots remain stored. Readers must select the document's active snapshot.
const storeQuery = `MERGE (d:Document {id: $document_key})
MERGE (v:Snapshot {id: $snapshot_id})
SET v.document_id = $document_id, v.source_uri = $source_uri,
    v.source_version = $source_version, v.content_hash = $content_hash,
    v.s3_version = $s3_version, v.etag = $etag, v.metadata_etag = $metadata_etag,
    v.unrestricted = $unrestricted, v.allowed_groups_json = $allowed_groups_json,
    v.owner = $owner, v.status = $status, v.model_id = $model_id,
    v.prompt_hash = $prompt_hash
MERGE (d)-[:HAS_SNAPSHOT]->(v)
SET d.active_snapshot = $snapshot_id
WITH v
UNWIND $claims AS row
MERGE (s:Entity {id: row.subject_id})
SET s.name = row.subject_name, s.kind = row.subject_type
MERGE (o:Entity {id: row.object_id})
SET o.name = row.object_name, o.kind = row.object_type
MERGE (c:Claim {id: row.claim_id})
SET c.predicate = row.predicate, c.quote = row.quote, c.review_status = 'unreviewed'
MERGE (v)-[:ASSERTS]->(c)
MERGE (s)-[:SUBJECT_OF]->(c)
MERGE (c)-[:OBJECT]->(o)
RETURN count(c) AS stored_claims`

type IngestResult struct {
	Source     string `json:"source"`
	SnapshotID string `json:"snapshot_id"`
	Claims     int    `json:"claims"`
}

func Store(ctx context.Context, client QueryClient, source Source, extraction Extraction, modelID string) (IngestResult, error) {
	// Validate again at the write boundary.
	data, err := json.Marshal(extraction)
	if err != nil {
		return IngestResult{}, err
	}
	extraction, err = DecodeExtraction(string(data), source.Text)
	if err != nil {
		return IngestResult{}, err
	}
	if source.Metadata.Unrestricted == nil || source.Metadata.AllowedGroups == nil {
		return IngestResult{}, fmt.Errorf("source permissions are required")
	}
	snapshotID := digest(struct {
		Source      Source
		Claims      Extraction
		Model       string
		Prompt      string
		ContentHash string
	}{source, extraction, modelID, ExtractionInstruction, digest(source.Text)})
	rows := make([]map[string]any, 0, len(extraction.Claims))
	for _, claim := range extraction.Claims {
		rows = append(rows, map[string]any{
			"claim_id":   digest([]any{snapshotID, claim}),
			"subject_id": entityID(claim.Subject), "subject_name": claim.Subject.Name, "subject_type": claim.Subject.Type,
			"object_id": entityID(claim.Object), "object_name": claim.Object.Name, "object_type": claim.Object.Type,
			"predicate": claim.Predicate, "quote": claim.Quote,
		})
	}
	groups, _ := json.Marshal(source.Metadata.AllowedGroups)
	values := map[string]any{
		"document_key": source.URI, "snapshot_id": snapshotID, "document_id": source.Metadata.DocumentID,
		"source_uri": source.URI, "source_version": source.Metadata.Version, "content_hash": digest(source.Text),
		"s3_version": source.S3Version, "etag": source.ETag, "metadata_etag": source.MetadataETag,
		"unrestricted": *source.Metadata.Unrestricted, "allowed_groups_json": string(groups),
		"owner": source.Metadata.Owner, "status": source.Metadata.Status, "model_id": modelID, "prompt_hash": digest(ExtractionInstruction), "claims": rows,
	}
	parameters, err := json.Marshal(values)
	if err != nil {
		return IngestResult{}, fmt.Errorf("encode graph parameters: %w", err)
	}
	_, err = client.ExecuteOpenCypherQuery(ctx, &neptunedata.ExecuteOpenCypherQueryInput{OpenCypherQuery: aws.String(storeQuery), Parameters: aws.String(string(parameters))})
	if err != nil {
		return IngestResult{}, fmt.Errorf("store source snapshot: %w", err)
	}
	return IngestResult{Source: source.URI, SnapshotID: snapshotID, Claims: len(rows)}, nil
}
