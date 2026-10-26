package admin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockagent"
	bedrockTypes "github.com/aws/aws-sdk-go-v2/service/bedrockagent/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/olaysco/org-brain/internal/graph"
)

const maxFileSize = 8 << 20

type Document struct {
	Key           string         `json:"key"`
	Name          string         `json:"name"`
	Size          int64          `json:"size"`
	Updated       *time.Time     `json:"updated,omitempty"`
	Metadata      graph.Metadata `json:"metadata"`
	MetadataError string         `json:"metadata_error,omitempty"`
}

type Upload struct {
	Name         string
	Content      []byte
	Owner        string
	Groups       []string
	Unrestricted bool
}

type Backend interface {
	List(context.Context) ([]Document, error)
	Read(context.Context, string) (string, error)
	Upload(context.Context, Upload) error
	Sync(context.Context, func(string)) error
}

type Service struct {
	S3                                            *s3.Client
	Bedrock                                       *bedrockagent.Client
	Bucket, Prefix, KnowledgeBaseID, DataSourceID string
	Ingest                                        func(context.Context, string) (int, error)
}

func supported(key string) bool {
	return slices.Contains([]string{".csv", ".md", ".txt"}, strings.ToLower(path.Ext(key)))
}

func (s Service) List(ctx context.Context) ([]Document, error) {
	docs := make([]Document, 0)
	pages := s3.NewListObjectsV2Paginator(s.S3, &s3.ListObjectsV2Input{Bucket: aws.String(s.Bucket), Prefix: aws.String(s.Prefix)})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list documents: %w", err)
		}
		for _, item := range page.Contents {
			key := aws.ToString(item.Key)
			if !supported(key) {
				continue
			}
			doc := Document{Key: key, Name: path.Base(key), Size: aws.ToInt64(item.Size), Updated: item.LastModified}
			metadata, err := s.S3.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.Bucket), Key: aws.String(key + ".metadata.json")})
			if err != nil {
				doc.MetadataError = "Metadata is unavailable."
			} else {
				data, readErr := io.ReadAll(io.LimitReader(metadata.Body, 64<<10))
				metadata.Body.Close()
				if readErr != nil {
					doc.MetadataError = "Metadata could not be read."
				} else {
					doc.Metadata, err = graph.DecodeMetadata(data)
					if err != nil {
						doc.MetadataError = "Metadata needs correction."
					}
				}
			}
			docs = append(docs, doc)
		}
	}
	return docs, nil
}

func (s Service) Read(ctx context.Context, key string) (string, error) {
	if !strings.HasPrefix(key, s.Prefix) || !supported(key) {
		return "", fmt.Errorf("document is outside this library")
	}
	object, err := s.S3.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.Bucket), Key: aws.String(key)})
	if err != nil {
		return "", err
	}
	defer object.Body.Close()
	data, err := io.ReadAll(io.LimitReader(object.Body, 128<<10))
	return string(data), err
}

func (s Service) Upload(ctx context.Context, input Upload) error {
	key := s.Prefix + input.Name
	versionData, _ := json.Marshal([]any{input.Content, input.Owner, input.Groups, input.Unrestricted})
	version := sha256.Sum256(versionData)
	identifier := sha256.Sum256([]byte(key))
	metadata := graph.Metadata{DocumentID: hex.EncodeToString(identifier[:16]), Version: hex.EncodeToString(version[:8]), Status: "approved", Owner: input.Owner, AllowedGroups: input.Groups, Unrestricted: &input.Unrestricted}
	body, err := json.Marshal(map[string]any{"metadataAttributes": metadata})
	if err != nil {
		return err
	}
	if _, err = s.S3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.Bucket), Key: aws.String(key), Body: bytes.NewReader(input.Content)}); err != nil {
		return fmt.Errorf("upload document: %w", err)
	}
	if _, err = s.S3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.Bucket), Key: aws.String(key + ".metadata.json"), Body: bytes.NewReader(body), ContentType: aws.String("application/json")}); err != nil {
		return fmt.Errorf("document uploaded, but metadata upload failed: %w", err)
	}
	return nil
}

func (s Service) Sync(ctx context.Context, progress func(string)) error {
	progress("Checking source documents…")
	docs, err := s.List(ctx)
	if err != nil {
		return err
	}
	if len(docs) == 0 {
		return fmt.Errorf("upload a document before synchronization")
	}
	for _, doc := range docs {
		if doc.MetadataError != "" {
			return fmt.Errorf("%s: %s", doc.Name, doc.MetadataError)
		}
	}
	progress("Updating document search…")
	started, err := s.Bedrock.StartIngestionJob(ctx, &bedrockagent.StartIngestionJobInput{KnowledgeBaseId: aws.String(s.KnowledgeBaseID), DataSourceId: aws.String(s.DataSourceID)})
	if err != nil {
		return fmt.Errorf("start document synchronization: %w", err)
	}
	if started.IngestionJob == nil {
		return fmt.Errorf("synchronization returned no job")
	}
	for {
		response, err := s.Bedrock.GetIngestionJob(ctx, &bedrockagent.GetIngestionJobInput{KnowledgeBaseId: aws.String(s.KnowledgeBaseID), DataSourceId: aws.String(s.DataSourceID), IngestionJobId: started.IngestionJob.IngestionJobId})
		if err != nil {
			return err
		}
		job := response.IngestionJob
		if job == nil {
			return fmt.Errorf("synchronization returned no status")
		}
		if job.Status == bedrockTypes.IngestionJobStatusComplete {
			if job.Statistics != nil && job.Statistics.NumberOfDocumentsFailed > 0 {
				return fmt.Errorf("document synchronization contains failed documents")
			}
			break
		}
		if job.Status == bedrockTypes.IngestionJobStatusFailed || job.Status == bedrockTypes.IngestionJobStatusStopped {
			return fmt.Errorf("document synchronization %s: %s", job.Status, strings.Join(job.FailureReasons, "; "))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	claims := 0
	for i, doc := range docs {
		progress(fmt.Sprintf("Updating relationships: %d of %d documents…", i+1, len(docs)))
		count, err := s.Ingest(ctx, doc.Key)
		if err != nil {
			return fmt.Errorf("search sync completed; relationships failed for %s: %w", doc.Name, err)
		}
		claims += count
	}
	progress(fmt.Sprintf("Synchronized %d documents and %d relationship claims.", len(docs), claims))
	return nil
}

func validUpload(input Upload) error {
	if input.Name == "" || path.Base(input.Name) != input.Name || strings.ContainsAny(input.Name, "\\\x00") || !supported(input.Name) {
		return fmt.Errorf("select a CSV, Markdown, or text file")
	}
	if strings.TrimSpace(string(input.Content)) == "" || len(input.Content) > maxFileSize || !utf8.Valid(input.Content) {
		return fmt.Errorf("file must contain UTF-8 text and be no larger than 8 MB")
	}
	if strings.TrimSpace(input.Owner) == "" {
		return fmt.Errorf("enter a document owner")
	}
	if !input.Unrestricted && len(input.Groups) == 0 {
		return fmt.Errorf("select at least one access group")
	}
	return nil
}
