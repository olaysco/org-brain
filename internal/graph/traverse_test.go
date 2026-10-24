package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/neptunedata"
	"github.com/aws/aws-sdk-go-v2/service/neptunedata/document"
)

type traversalFixture struct {
	rows      []map[string]any
	frontiers [][]string
}

func (f *traversalFixture) ExecuteOpenCypherQuery(_ context.Context, input *neptunedata.ExecuteOpenCypherQueryInput, _ ...func(*neptunedata.Options)) (*neptunedata.ExecuteOpenCypherQueryOutput, error) {
	if !strings.Contains(*input.OpenCypherQuery, "v.id = d.active_snapshot") {
		return nil, fmt.Errorf("missing active snapshot filter")
	}
	var parameters struct {
		Entities []string `json:"entities"`
		Sources  []string `json:"sources"`
	}
	if err := json.Unmarshal([]byte(*input.Parameters), &parameters); err != nil {
		return nil, err
	}
	f.frontiers = append(f.frontiers, parameters.Entities)
	rows := make([]map[string]any, 0)
	for _, row := range f.rows {
		if slices.Contains(parameters.Entities, row["subject_id"].(string)) || slices.Contains(parameters.Entities, row["object_id"].(string)) {
			rows = append(rows, row)
		}
	}
	return &neptunedata.ExecuteOpenCypherQueryOutput{Results: document.NewLazyDocument(rows)}, nil
}
func traversalRow(subject, object, source, group string) map[string]any {
	return map[string]any{"subject_id": subject, "object_id": object, "subject": subject, "object": object, "predicate": "DEPENDS_ON", "quote": subject + " depends on " + object + ".", "source": source, "version": "1", "review_status": "unreviewed", "unrestricted": false, "allowed_groups_json": `["` + group + `"]`}
}
func TestExpandShipmentDependenciesAndPermissions(t *testing.T) {
	public := "s3://demo/operations.csv"
	fixture := &traversalFixture{rows: []map[string]any{
		traversalRow("route-london-nairobi", "london", public, "sales"),
		traversalRow("shipment-104", "route-london-nairobi", public, "sales"),
		traversalRow("shipment-104", "customer-4", public, "sales"),
		traversalRow("secret-bridge", "london", public, "finance"),
		traversalRow("hidden-customer", "secret-bridge", public, "sales"),
		traversalRow("out-of-scope", "london", "s3://other/private.csv", "sales"),
	}}
	seed := Evidence{Subject: "London Hub", Object: "Europe Operations team", SubjectID: "london", ObjectID: "europe-team", Source: public, Version: "1", Predicate: "OWNED_BY", Quote: "London Hub is owned by the Europe Operations team."}
	result, err := Expand(context.Background(), fixture, []Evidence{seed}, []string{"sales"}, []string{public})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 4 {
		t.Fatalf("expected seed and three connected claims; got %+v", result)
	}
	foundCustomer := false
	for _, claim := range result {
		if claim.ObjectID == "customer-4" {
			foundCustomer = true
		}
		if strings.Contains(claim.SubjectID, "secret") || claim.SubjectID == "out-of-scope" || claim.SubjectID == "hidden-customer" {
			t.Fatal("restricted claim entered evidence")
		}
	}
	if !foundCustomer {
		t.Fatal("three-step traversal did not reach the shipment customer")
	}
	for _, ids := range fixture.frontiers {
		if slices.Contains(ids, "secret-bridge") || slices.Contains(ids, "out-of-scope") {
			t.Fatal("restricted claim became a traversal bridge")
		}
	}
	if len(fixture.frontiers) != 3 {
		t.Fatalf("unexpected traversal count %d", len(fixture.frontiers))
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "subject_id") || strings.Contains(string(encoded), "object_id") {
		t.Fatal("internal keys entered public evidence")
	}
}
func TestExpandRejectsExcessRows(t *testing.T) {
	fixture := &traversalFixture{}
	for i := 0; i < 201; i++ {
		fixture.rows = append(fixture.rows, traversalRow("hub", fmt.Sprint(i), "s3://demo/a.csv", "sales"))
	}
	_, err := Expand(context.Background(), fixture, []Evidence{{SubjectID: "hub", Source: "s3://demo/a.csv"}}, []string{"sales"}, []string{"s3://demo/a.csv"})
	if err == nil {
		t.Fatal("oversized traversal returned incomplete evidence without an error")
	}
}
