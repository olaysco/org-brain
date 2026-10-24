package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockagent"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/olaysco/org-brain-aws/internal/graph"
)

func TestAWSUploadAndSync(t *testing.T) {
	events := []string{}
	var metadata graph.Metadata
	failed := false
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/demo/"):
			data, _ := io.ReadAll(r.Body)
			if strings.HasSuffix(r.URL.Path, ".metadata.json") {
				var value struct {
					Metadata graph.Metadata `json:"metadataAttributes"`
				}
				if err := json.Unmarshal(data, &value); err != nil {
					t.Error(err)
				}
				metadata = value.Metadata
			}
			w.Header().Set("ETag", `"test"`)
			w.WriteHeader(http.StatusOK)
		case r.URL.Query().Get("list-type") == "2":
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, `<ListBucketResult><IsTruncated>false</IsTruncated><Contents><Key>sources/logistics/new.csv</Key><Size>10</Size></Contents><Contents><Key>sources/logistics/new.csv.metadata.json</Key></Contents></ListBucketResult>`)
		case strings.HasSuffix(r.URL.Path, ".metadata.json"):
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"metadataAttributes": metadata})
		case (r.Method == http.MethodPost || r.Method == http.MethodPut) && strings.HasSuffix(strings.TrimRight(r.URL.Path, "/"), "/ingestionjobs"):
			events = append(events, "start")
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"ingestionJob":{"ingestionJobId":"JOB","status":"STARTING"}}`)
		case strings.HasSuffix(strings.TrimRight(r.URL.Path, "/"), "/ingestionjobs/JOB"):
			events = append(events, "complete")
			count := 0
			if failed {
				count = 1
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"ingestionJob":{"ingestionJobId":"JOB","status":"COMPLETE","statistics":{"numberOfDocumentsFailed":%d}}}`, count)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer backend.Close()
	cfg := aws.Config{Region: "eu-west-2", Credentials: credentials.NewStaticCredentialsProvider("test", "test", "")}
	service := Service{S3: s3.NewFromConfig(cfg, func(o *s3.Options) { o.BaseEndpoint = aws.String(backend.URL); o.UsePathStyle = true }), Bedrock: bedrockagent.NewFromConfig(cfg, func(o *bedrockagent.Options) { o.BaseEndpoint = aws.String(backend.URL) }), Bucket: "demo", Prefix: "sources/logistics/", KnowledgeBaseID: "KB", DataSourceID: "DS", Ingest: func(_ context.Context, key string) (int, error) {
		events = append(events, "graph")
		if key != "sources/logistics/new.csv" {
			t.Error(key)
		}
		return 2, nil
	}}
	input := Upload{Name: "new.csv", Content: []byte("id\nS106\n"), Owner: "finance", Groups: []string{"finance"}}
	if err := service.Upload(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if metadata.Unrestricted == nil || *metadata.Unrestricted || !reflect.DeepEqual(metadata.AllowedGroups, []string{"finance"}) || metadata.DocumentID == "" || metadata.Version == "" {
		t.Fatalf("incorrect metadata: %+v", metadata)
	}
	if err := service.Sync(context.Background(), func(string) {}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(events, []string{"start", "complete", "graph"}) {
		t.Fatalf("incorrect sync order: %v", events)
	}
	events = nil
	failed = true
	if err := service.Sync(context.Background(), func(string) {}); err == nil {
		t.Fatal("failed document accepted")
	}
	if !reflect.DeepEqual(events, []string{"start", "complete"}) {
		t.Fatalf("failed sync reached graph: %v", events)
	}
}
