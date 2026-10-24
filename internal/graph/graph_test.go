package graph

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/neptunedata"
)

const sourceText = "The Enterprise plan includes single sign-on."
const validExtraction = `{"claims":[{"subject":{"name":"Enterprise","type":"Plan"},"predicate":"INCLUDES","object":{"name":"single sign-on","type":"Feature"},"quote":"The Enterprise plan includes single sign-on."}]}`

func TestDecodeExtraction(t *testing.T) {
	cases := []struct {
		name, output string
		valid        bool
	}{
		{"valid", validExtraction, true}, {"empty", `{"claims":[]}`, true},
		{"missing", `{}`, false}, {"null", `{"claims":null}`, false},
		{"unknown field", strings.Replace(validExtraction, `"quote":`, `"extra":true,"quote":`, 1), false},
		{"fabricated quote", strings.Replace(validExtraction, "The Enterprise plan includes single sign-on.", "An unsupported claim.", 1), false},
		{"source-specific predicate", strings.Replace(validExtraction, "INCLUDES", "OFFERS", 1), true},
		{"empty predicate", strings.Replace(validExtraction, "INCLUDES", "", 1), false},
		{"trailing object", validExtraction + `{}`, false}, {"markdown", "```json\n" + validExtraction + "\n```", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := DecodeExtraction(c.output, sourceText)
			if (err == nil) != c.valid {
				t.Fatalf("error = %v; valid = %v", err, c.valid)
			}
		})
	}
}

func TestDecodeMetadataPermissions(t *testing.T) {
	cases := []struct {
		name, data string
		valid      bool
	}{
		{"restricted", `{"metadataAttributes":{"document_id":"d","version":"1","unrestricted":false,"allowed_groups":["sales"]}}`, true},
		{"unrestricted", `{"metadataAttributes":{"document_id":"d","version":"1","unrestricted":true,"allowed_groups":[]}}`, true},
		{"missing groups", `{"metadataAttributes":{"document_id":"d","version":"1","unrestricted":true}}`, false},
		{"conflicting permissions", `{"metadataAttributes":{"document_id":"d","version":"1","unrestricted":false,"allowed_groups":[]}}`, false},
		{"blank group", `{"metadataAttributes":{"document_id":"d","version":"1","unrestricted":false,"allowed_groups":[""]}}`, false},
		{"reject typed attributes", `{"metadataAttributes":{"document_id":{"value":{"type":"STRING","stringValue":"d"}},"version":"1","unrestricted":{"value":{"type":"BOOLEAN","booleanValue":false}},"allowed_groups":{"value":{"type":"STRING_LIST","stringListValue":["sales"]}}}}`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := DecodeMetadata([]byte(c.data))
			if (err == nil) != c.valid {
				t.Fatalf("error = %v; valid = %v", err, c.valid)
			}
		})
	}
}

type queryRecorder struct {
	calls      int
	parameters map[string]any
	err        error
}

func (q *queryRecorder) ExecuteOpenCypherQuery(_ context.Context, input *neptunedata.ExecuteOpenCypherQueryInput, _ ...func(*neptunedata.Options)) (*neptunedata.ExecuteOpenCypherQueryOutput, error) {
	q.calls++
	if err := json.Unmarshal([]byte(*input.Parameters), &q.parameters); err != nil {
		return nil, err
	}
	return &neptunedata.ExecuteOpenCypherQueryOutput{}, q.err
}
func TestStoreSnapshot(t *testing.T) {
	metadata, err := DecodeMetadata([]byte(`{"metadataAttributes":{"document_id":"d","version":"1","unrestricted":false,"allowed_groups":["sales"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	source := Source{URI: "s3://test/document.md", Text: sourceText, ETag: "v1", Metadata: metadata}
	extraction, err := DecodeExtraction(validExtraction, sourceText)
	if err != nil {
		t.Fatal(err)
	}
	recorder := &queryRecorder{}
	first, err := Store(context.Background(), recorder, source, extraction, "model")
	if err != nil {
		t.Fatal(err)
	}
	again, err := Store(context.Background(), recorder, source, extraction, "model")
	if err != nil {
		t.Fatal(err)
	}
	if first.SnapshotID != again.SnapshotID {
		t.Fatal("repeat ingestion changed snapshot ID")
	}
	if recorder.parameters["allowed_groups_json"] != `["sales"]` {
		t.Fatal("permissions missing from snapshot")
	}
	source.Metadata.AllowedGroups = []string{"support"}
	changed, err := Store(context.Background(), recorder, source, extraction, "model")
	if err != nil {
		t.Fatal(err)
	}
	if changed.SnapshotID == first.SnapshotID {
		t.Fatal("permission change reused snapshot")
	}
	before := recorder.calls
	extraction.Claims[0].Quote = "fabricated"
	if _, err := Store(context.Background(), recorder, source, extraction, "model"); err == nil {
		t.Fatal("invalid claim accepted")
	}
	if recorder.calls != before {
		t.Fatal("invalid claim reached database")
	}
	recorder.err = errors.New("database unavailable")
	if _, err := Store(context.Background(), recorder, source, Extraction{Claims: []Claim{}}, "model"); err == nil {
		t.Fatal("database failure ignored")
	}
}
