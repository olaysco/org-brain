package retrieval

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockagentruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockagentruntime/types"
	runtimeTypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

type policyCheck struct {
	blocked bool
	calls   int
}

func (c *policyCheck) Check(context.Context, string, runtimeTypes.GuardrailContentSource) error {
	c.calls++
	if c.blocked {
		return errors.New("blocked")
	}
	return nil
}
func TestSearchAWSHTTPPath(t *testing.T) {
	requests := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/knowledgebases/KBTEST/retrieve" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		for _, term := range []string{"allowed_groups", "sales", "x-amz-bedrock-kb-source-uri", "s3://demo/sso.md"} {
			if !strings.Contains(string(body), term) {
				t.Errorf("filter missing %q", term)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"retrievalResults":[{"content":{"text":"Enterprise includes SSO.","type":"TEXT"},"location":{"type":"S3","s3Location":{"uri":"s3://demo/sso.md"}},"metadata":{"allowed_groups":["sales"],"unrestricted":false,"version":"1"},"score":0.8},{"content":{"text":"Other document.","type":"TEXT"},"location":{"type":"S3","s3Location":{"uri":"s3://demo/private.md"}},"metadata":{"allowed_groups":["sales"],"unrestricted":false,"version":"1"},"score":0.7}]}`)
	}))
	defer backend.Close()
	cfg := aws.Config{Region: "eu-west-2", Credentials: credentials.NewStaticCredentialsProvider("test", "test", "")}
	client := bedrockagentruntime.NewFromConfig(cfg, func(o *bedrockagentruntime.Options) { o.BaseEndpoint = aws.String(backend.URL) })
	check := &policyCheck{}
	service := Service{Retriever: client, Guardrails: check, KnowledgeBaseID: "KBTEST", Groups: []string{"sales"}, AllowedSources: []string{"s3://demo/sso.md"}, SearchType: types.SearchTypeSemantic}
	result, err := service.Search(context.Background(), "Which plan includes SSO?")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Documents) != 1 || result.Documents[0].Source != "s3://demo/sso.md" {
		t.Fatalf("unexpected scope: %+v", result)
	}
	if check.calls != 2 || requests != 1 {
		t.Fatal("expected input and evidence checks")
	}
	check.blocked = true
	if _, err := service.Search(context.Background(), "blocked"); err == nil || requests != 1 {
		t.Fatal("blocked question reached retrieval")
	}
}
func TestPermissionMetadata(t *testing.T) {
	if permitted(map[string]any{"unrestricted": false, "allowed_groups": []any{"sales"}}, []string{"support"}) {
		t.Fatal("wrong group allowed")
	}
	if permitted(map[string]any{}, []string{"sales"}) {
		t.Fatal("missing metadata allowed")
	}
	if !permitted(map[string]any{"unrestricted": true, "allowed_groups": []any{}}, nil) {
		t.Fatal("unrestricted document denied")
	}
}
