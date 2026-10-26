package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/olaysco/org-brain/internal/retrieval"
)

type Searcher interface {
	Search(context.Context, string) (retrieval.Evidence, error)
}
type searchInput struct {
	Query string `json:"query" jsonschema:"Question about the knowledge library"`
}

// Handler provides a read-only MCP tool with a fixed server-side demo identity.
func Handler(searcher Searcher, maxCalls int) http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "Company Knowledge", Version: "0.1.0"}, &mcp.ServerOptions{
		Instructions: "Answer from the returned evidence. Cite source references. State when the evidence is insufficient.",
	})
	var mutex sync.Mutex
	calls := 0
	slot := make(chan struct{}, 1)
	no := false
	mcp.AddTool(server, &mcp.Tool{
		Name: "search_company_knowledge", Title: "Search company knowledge",
		Description: `Search documents and related relationship evidence in the knowledge library.
		Returns accessible source text, extracted relationships, and source references. Use this tool when a question requires information from the knowledge library.
		Extracted relationships can contain errors; use the source text to check them.`,
		Annotations: &mcp.ToolAnnotations{Title: "Search company knowledge", ReadOnlyHint: true, DestructiveHint: &no, IdempotentHint: true, OpenWorldHint: &no},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input searchInput) (*mcp.CallToolResult, retrieval.Evidence, error) {
		select {
		case slot <- struct{}{}:
			defer func() { <-slot }()
		default:
			return nil, retrieval.Evidence{}, fmt.Errorf("a search is already running; retry shortly")
		}
		mutex.Lock()
		if calls >= maxCalls {
			mutex.Unlock()
			return nil, retrieval.Evidence{}, fmt.Errorf("search limit reached; restart the server")
		}
		calls++
		mutex.Unlock()
		ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		evidence, err := searcher.Search(ctx, input.Query)
		if err != nil {
			log.Printf("knowledge search failed: %v", err)
			return nil, retrieval.Evidence{}, fmt.Errorf("search unavailable or blocked by the content policy; no evidence was returned")
		}
		data, err := json.Marshal(evidence)
		if err != nil {
			return nil, retrieval.Evidence{}, err
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, evidence, nil
	})

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true, MaxRequestBodyBytes: 16 * 1024,
	})
	protection := http.NewCrossOriginProtection()
	for _, origin := range []string{"https://claude.ai", "https://claude.com"} {
		protection.AddTrustedOrigin(origin)
	}

	mux := http.NewServeMux()
	mux.Handle("/mcp", protection.Handler(handler))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"status":"ok"}`)
	})
	return mux
}
