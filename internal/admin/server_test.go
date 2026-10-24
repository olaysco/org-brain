package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type testBackend struct {
	input   Upload
	started chan struct{}
	release chan struct{}
}

func (b *testBackend) List(context.Context) ([]Document, error)     { return []Document{}, nil }
func (b *testBackend) Read(context.Context, string) (string, error) { return "source text", nil }
func (b *testBackend) Upload(_ context.Context, input Upload) error { b.input = input; return nil }
func (b *testBackend) Sync(ctx context.Context, progress func(string)) error {
	close(b.started)
	progress("Updating document search…")
	select {
	case <-b.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func uploadRequest(groups string, public bool) *http.Request {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, _ := writer.CreateFormFile("file", "new.csv")
	file.Write([]byte("id\nS106\n"))
	writer.WriteField("owner", "operations")
	writer.WriteField("groups", groups)
	if public {
		writer.WriteField("unrestricted", "true")
	}
	writer.Close()
	request := httptest.NewRequest(http.MethodPost, "http://localhost/api/upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}
func TestUploadAccessAndOrigin(t *testing.T) {
	backend := &testBackend{}
	handler := Handler(context.Background(), backend)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, uploadRequest("", false))
	if response.Code != http.StatusBadRequest {
		t.Fatal("missing access groups accepted")
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, uploadRequest("sales, operations, sales", false))
	if response.Code != http.StatusCreated || strings.Join(backend.input.Groups, ",") != "operations,sales" || backend.input.Unrestricted {
		t.Fatalf("code=%d input=%+v", response.Code, backend.input)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, uploadRequest("finance", true))
	if response.Code != http.StatusCreated || !backend.input.Unrestricted || len(backend.input.Groups) != 0 {
		t.Fatal("explicit unrestricted access not applied")
	}
	request := uploadRequest("sales", false)
	request.Header.Set("Origin", "https://other.example")
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatal("cross-origin upload accepted")
	}
}
func TestSyncIsAsynchronousAndSingle(t *testing.T) {
	backend := &testBackend{started: make(chan struct{}), release: make(chan struct{})}
	defer close(backend.release)
	handler := Handler(context.Background(), backend)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "http://localhost/api/sync", nil))
	if response.Code != http.StatusAccepted {
		t.Fatal(response.Code)
	}
	<-backend.started
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "http://localhost/api/sync", nil))
	if response.Code != http.StatusConflict {
		t.Fatal("duplicate synchronization accepted")
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, uploadRequest("sales", false))
	if response.Code != http.StatusConflict {
		t.Fatal("upload accepted during synchronization")
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://localhost/api/sync", nil))
	var state syncState
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state.Status != "running" {
		t.Fatalf("unexpected state: %+v", state)
	}
}

func TestAdminRejectsUntrustedHost(t *testing.T) {
	handler := Handler(context.Background(), &testBackend{})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://other.example/api/documents", nil))
	if response.Code != http.StatusForbidden {
		t.Fatal("untrusted host accepted")
	}
}
