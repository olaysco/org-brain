package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Metadata struct {
	DocumentID    string   `json:"document_id"`
	Version       string   `json:"version"`
	Status        string   `json:"status"`
	Owner         string   `json:"owner"`
	Unrestricted  *bool    `json:"unrestricted"`
	AllowedGroups []string `json:"allowed_groups"`
}

type Source struct {
	URI          string   `json:"uri"`
	Text         string   `json:"-"`
	ETag         string   `json:"etag"`
	S3Version    string   `json:"s3_version"`
	MetadataETag string   `json:"metadata_etag"`
	Metadata     Metadata `json:"metadata"`
}

func DecodeMetadata(data []byte) (Metadata, error) {
	var envelope struct {
		Metadata Metadata `json:"metadataAttributes"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return Metadata{}, fmt.Errorf("decode source metadata: %w", err)
	}
	metadata := envelope.Metadata
	if strings.TrimSpace(metadata.DocumentID) == "" || strings.TrimSpace(metadata.Version) == "" {
		return Metadata{}, fmt.Errorf("document_id and version are required")
	}
	if metadata.Unrestricted == nil || metadata.AllowedGroups == nil {
		return Metadata{}, fmt.Errorf("unrestricted and allowed_groups are required")
	}

	return metadata, nil
}

func Read(reader io.Reader) ([]byte, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func LoadS3(ctx context.Context, client *s3.Client, bucket, key string) (Source, error) {
	object, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if err != nil {
		return Source{}, fmt.Errorf("read S3 document: %w", err)
	}
	defer object.Body.Close()
	text, err := Read(object.Body)
	if err != nil {
		return Source{}, fmt.Errorf("read document body: %w", err)
	}
	if !utf8.Valid(text) || strings.TrimSpace(string(text)) == "" {
		return Source{}, fmt.Errorf("document must contain UTF-8 text")
	}

	metadataObject, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key + ".metadata.json")})
	if err != nil {
		return Source{}, fmt.Errorf("read S3 metadata: %w", err)
	}
	defer metadataObject.Body.Close()
	data, err := Read(metadataObject.Body)
	if err != nil {
		return Source{}, fmt.Errorf("read metadata body: %w", err)
	}
	metadata, err := DecodeMetadata(data)
	if err != nil {
		return Source{}, err
	}
	return Source{URI: "s3://" + bucket + "/" + key, Text: string(text), ETag: aws.ToString(object.ETag), S3Version: aws.ToString(object.VersionId), MetadataETag: aws.ToString(metadataObject.ETag), Metadata: metadata}, nil
}
