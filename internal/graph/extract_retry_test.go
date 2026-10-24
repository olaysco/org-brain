package graph

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type extractionGenerator struct {
	outputs      []string
	calls        int
	instructions []string
	sources      []string
	err          error
}

func (g *extractionGenerator) Generate(_ context.Context, instruction, source string, _ int32) (string, error) {
	g.instructions = append(g.instructions, instruction)
	g.calls++
	g.sources = append(g.sources, source)
	if g.err != nil {
		return "", g.err
	}
	return g.outputs[g.calls-1], nil
}
func TestExtractValidationRetry(t *testing.T) {
	invalid := strings.Replace(validExtraction, sourceText, "A statement absent from the source.", 1)
	g := &extractionGenerator{outputs: []string{invalid, validExtraction}}
	extraction, err := Extract(context.Background(), g, sourceText)
	if err != nil || len(extraction.Claims) != 1 || g.calls != 2 {
		t.Fatalf("calls=%d extraction=%+v error=%v", g.calls, extraction, err)
	}
	if !strings.Contains(g.instructions[1], "an exact source quote is required") {
		t.Fatal("retry omitted validation feedback")
	}
}
func TestExtractRetryLimit(t *testing.T) {
	g := &extractionGenerator{outputs: []string{`{}`, `{}`}}
	if _, err := Extract(context.Background(), g, sourceText); err == nil || g.calls != 2 {
		t.Fatalf("calls=%d error=%v", g.calls, err)
	}
}
func TestExtractDoesNotRetryGenerationErrors(t *testing.T) {
	g := &extractionGenerator{err: errors.New("guardrail blocked")}
	if _, err := Extract(context.Background(), g, sourceText); err == nil || g.calls != 1 {
		t.Fatalf("calls=%d error=%v", g.calls, err)
	}
}
