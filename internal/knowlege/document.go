package knowledge

type Document struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Content       string   `json:"content"`
	SourceURI     string   `json:"source_uri"`
	Version       string   `json:"version"`
	Status        string   `json:"status"`
	Owner         string   `json:"owner"`
	AllowedGroups []string `json:"allowed_groups"`
}

type Metadata struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	SourceURI     string   `json:"source_uri"`
	Version       string   `json:"version"`
	Status        string   `json:"status"`
	Owner         string   `json:"owner"`
	AllowedGroups []string `json:"allowed_groups"`
	Unrestricted  bool     `json:"unrestricted"`
}
