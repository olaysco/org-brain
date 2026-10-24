package graph

import (
	"context"
	"encoding/json"
	"testing"
)

func TestEntityIDUsesSourceIdentifier(t *testing.T) {
	for _, pair := range [][2]Entity{
		{{ID: "H001", Name: "London Hub", Type: "Hub"}, {ID: "H001", Name: "H001", Type: "Hub"}},
		{{ID: "GLOBAL_EXPRESS", Name: "Global Express service", Type: "Service"}, {ID: "GLOBAL_EXPRESS", Name: "GLOBAL_EXPRESS", Type: "Service"}},
	} {
		if entityID(pair[0]) != entityID(pair[1]) {
			t.Fatal("labels changed source identity")
		}
	}
	if entityID(Entity{ID: "H001", Name: "London Hub", Type: "Hub"}) == entityID(Entity{ID: "H002", Name: "London Hub", Type: "Hub"}) {
		t.Fatal("different IDs merged")
	}
}

func TestEntityIDMustAppearInQuote(t *testing.T) {
	source := "hub_id,hub_name,relationship_evidence\nH001,London Hub,Hub H001 (London Hub) is owned by Team A."
	claim := Claim{Subject: Entity{Name: "London Hub", Type: "Hub"}, Predicate: "OWNED_BY", Object: Entity{Name: "Team A", Type: "Team"}, Quote: "Hub H001 (London Hub) is owned by Team A."}
	claim.Subject.ID = "H001"
	data, _ := json.Marshal(Extraction{Claims: []Claim{claim}})
	if _, err := DecodeExtraction(string(data), source); err != nil {
		t.Fatal(err)
	}
	claim.Subject.ID = "H999"
	data, _ = json.Marshal(Extraction{Claims: []Claim{claim}})
	if _, err := DecodeExtraction(string(data), source); err == nil {
		t.Fatal("invented source ID accepted")
	}
}

func TestExtractStructuredDataWithoutEvidenceColumn(t *testing.T) {
	source := "engineer_id,engineer_name,assigned_project_id\nE001,Ada,P001\n"
	output := `{"claims":[{"subject":{"id":"E001","name":"Ada","type":"Engineer"},"predicate":"ASSIGNED_TO","object":{"id":"P001","name":"P001","type":"Project"},"quote":"E001,Ada,P001"}]}`
	generator := &extractionGenerator{outputs: []string{output}}
	extraction, err := Extract(context.Background(), generator, source)
	if err != nil || len(extraction.Claims) != 1 || generator.calls != 1 {
		t.Fatalf("extraction=%+v calls=%d error=%v", extraction, generator.calls, err)
	}
	if generator.sources[0] != source {
		t.Fatal("extraction changed the source")
	}
	recorder := &queryRecorder{}
	unrestricted := true
	_, err = Store(context.Background(), recorder, Source{URI: "s3://test/projects.csv", Text: source, Metadata: Metadata{DocumentID: "projects", Version: "1", Unrestricted: &unrestricted, AllowedGroups: []string{}}}, extraction, "model")
	if err != nil {
		t.Fatal(err)
	}
	rows := recorder.parameters["claims"].([]any)
	if rows[0].(map[string]any)["predicate"] != "ASSIGNED_TO" {
		t.Fatal("storage changed the source-specific predicate")
	}
}

func TestExtractUsesFullDocument(t *testing.T) {
	generator := &extractionGenerator{outputs: []string{validExtraction}}
	source := "# Reference\n" + sourceText + "\n## Relationships\nMore source text.\n## Notes\nAdditional context."
	extraction, err := Extract(context.Background(), generator, source)
	if err != nil || len(extraction.Claims) != 1 || generator.calls != 1 {
		t.Fatalf("extraction=%+v calls=%d error=%v", extraction, generator.calls, err)
	}
	if generator.sources[0] != source {
		t.Fatal("extraction omitted source sections")
	}
}
