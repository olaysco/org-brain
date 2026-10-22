package graph

type Entity struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type Claim struct {
	Subject   Entity `json:"subject"`
	Predicate string `json:"predicate"`
	Object    Entity `json:"object"`
	Quote     string `json:"quote"`
}

type Extraction struct {
	Claims []Claim `json:"claims"`
}
