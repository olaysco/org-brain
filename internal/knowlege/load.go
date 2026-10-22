package knowledge

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

var ErrAccessDenied = errors.New("document access denied")

// LoadContent reads the file path and returns an error
func LoadContent(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	return string(content), nil
}

func LoadMetadata(path string) (Metadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Metadata{}, fmt.Errorf("metadata: %q: %w", path, err)
	}

	var metadata Metadata
	err = json.Unmarshal(data, &metadata)
	if err != nil {
		return Metadata{}, fmt.Errorf("metadata: %q: %w", path, err)
	}

	if metadata.ID == "" {
		return Metadata{}, fmt.Errorf("metadata: metadata ID cannot be empty %q", path)
	}

	if metadata.SourceURI == "" {
		return Metadata{}, fmt.Errorf("metadata: metadata source URI cannot be empty %q", path)
	}

	if metadata.AllowedGroups == nil {
		return Metadata{}, fmt.Errorf(
			"metadata %q: allowed_groups is required; use [] for unrestricted access",
			path,
		)
	}

	return metadata, nil
}

func LoadDocument(metadataPath string, userGroups []string) (Document, error) {
	metadata, err := LoadMetadata(metadataPath)
	if err != nil {
		return Document{}, err
	}

	if !CheckPermission(userGroups, metadata.AllowedGroups) {
		return Document{}, ErrAccessDenied
	}

	content, err := LoadContent(metadata.SourceURI)
	if err != nil {
		return Document{}, err
	}

	return Document{
		ID:            metadata.ID,
		Title:         metadata.Title,
		Content:       content,
		SourceURI:     metadata.SourceURI,
		Version:       metadata.Version,
		Status:        metadata.Status,
		Owner:         metadata.Owner,
		AllowedGroups: metadata.AllowedGroups,
	}, nil
}

func LoadDocuments(metadataPaths []string, userGroups []string) ([]Document, error) {
	documents := make([]Document, 0)
	for _, path := range metadataPaths {
		doc, err := LoadDocument(path, userGroups)
		if err != nil {
			if err == ErrAccessDenied {
				continue
			}
			return []Document{}, fmt.Errorf("load document metadata %q: %w", path, err)
		}

		documents = append(documents, doc)
	}

	return documents, nil
}
