package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/olaysco/org-brain/internal/graph"
	"github.com/olaysco/org-brain/internal/retrieval"
)

type demoSearch struct{ calls int }

func (s *demoSearch) Search(_ context.Context, query string) (retrieval.Evidence, error) {
	s.calls++
	if query == "blocked" {
		return retrieval.Evidence{}, fmt.Errorf("private diagnostic")
	}
	return retrieval.Evidence{Documents: []retrieval.Document{{Text: "Enterprise includes single sign-on.", Source: "s3://demo/sso.md"}}, RelationshipClaims: []graph.Evidence{}}, nil
}
func TestRemoteMCP(t *testing.T) {
	search := &demoSearch{}
	server := httptest.NewServer(Handler(search, 2))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != "search_company_knowledge" {
		t.Fatalf("unexpected tools: %+v", tools.Tools)
	}
	if !tools.Tools[0].Annotations.ReadOnlyHint {
		t.Fatal("missing read-only annotation")
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "search_company_knowledge", Arguments: map[string]any{"query": "Which plan includes SSO?"}})
	if err != nil || result.IsError || len(result.Content) == 0 {
		t.Fatalf("tool failed: %v %v", result, err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "Enterprise") || !strings.Contains(text, "s3://demo/sso.md") {
		t.Fatal("source evidence missing")
	}
	blocked, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "search_company_knowledge", Arguments: map[string]any{"query": "blocked"}})
	if err != nil || !blocked.IsError {
		t.Fatalf("tool error not returned: %v %v", blocked, err)
	}
	if strings.Contains(blocked.Content[0].(*mcp.TextContent).Text, "private diagnostic") {
		t.Fatal("internal error exposed")
	}
	budget, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "search_company_knowledge", Arguments: map[string]any{"query": "again"}})
	if err != nil || !budget.IsError || search.calls != 2 {
		t.Fatal("demo budget not enforced")
	}
}
func TestOriginRejected(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "http://localhost/mcp", strings.NewReader(`{}`))
	request.Header.Set("Origin", "https://untrusted.example")
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	Handler(&demoSearch{}, 2).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("got %d; want 403", recorder.Code)
	}
}
