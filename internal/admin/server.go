package admin

import (
	"context"
	"embed"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

//go:embed index.html style.css app.js
var assets embed.FS

type syncState struct {
	Status   string     `json:"status"`
	Message  string     `json:"message"`
	Started  *time.Time `json:"started,omitempty"`
	Finished *time.Time `json:"finished,omitempty"`
}

type server struct {
	backend Backend
	root    context.Context
	mu      sync.Mutex
	state   syncState
}

func Handler(ctx context.Context, backend Backend) http.Handler {
	s := &server{backend: backend, root: ctx, state: syncState{Status: "idle", Message: "No sync has run in this session."}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/documents", s.documents)
	mux.HandleFunc("GET /api/document", s.document)
	mux.HandleFunc("POST /api/upload", s.upload)
	mux.HandleFunc("GET /api/sync", s.syncStatus)
	mux.HandleFunc("POST /api/sync", s.startSync)
	mux.Handle("GET /", http.FileServer(http.FS(assets)))
	protected := http.NewCrossOriginProtection().Handler(mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if value, _, err := net.SplitHostPort(host); err == nil {
			host = value
		}
		ip := net.ParseIP(strings.Trim(host, "[]"))
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			http.Error(w, "Local admin access only.", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		protected.ServeHTTP(w, r)
	})
}
func send(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func failure(w http.ResponseWriter, status int, err error) {
	send(w, status, map[string]string{"error": err.Error()})
}
func (s *server) documents(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	docs, err := s.backend.List(ctx)
	if err != nil {
		failure(w, http.StatusBadGateway, err)
		return
	}
	send(w, http.StatusOK, docs)
}
func (s *server) document(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	text, err := s.backend.Read(ctx, r.URL.Query().Get("key"))
	if err != nil {
		failure(w, http.StatusBadGateway, err)
		return
	}
	send(w, http.StatusOK, map[string]string{"text": text})
}
func (s *server) upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFileSize+(1<<20))
	if err := r.ParseMultipartForm(maxFileSize + (1 << 20)); err != nil {
		http.Error(w, "File is too large or the form is invalid.", http.StatusBadRequest)
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Select a file.", http.StatusBadRequest)
		return
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxFileSize+1))
	if err != nil {
		failure(w, http.StatusBadRequest, err)
		return
	}
	groups := make([]string, 0)
	for _, group := range strings.Split(r.FormValue("groups"), ",") {
		group = strings.TrimSpace(group)
		if group != "" && !slices.Contains(groups, group) {
			groups = append(groups, group)
		}
	}
	slices.Sort(groups)
	input := Upload{Name: header.Filename, Content: content, Owner: strings.TrimSpace(r.FormValue("owner")), Groups: groups, Unrestricted: r.FormValue("unrestricted") == "true"}
	if input.Unrestricted {
		input.Groups = []string{}
	}
	if err := validUpload(input); err != nil {
		failure(w, http.StatusBadRequest, err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Status == "running" {
		send(w, http.StatusConflict, map[string]string{"error": "Wait for synchronization before uploading."})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	if err := s.backend.Upload(ctx, input); err != nil {
		failure(w, http.StatusBadGateway, err)
		return
	}
	send(w, http.StatusCreated, map[string]string{"message": "Document and access metadata uploaded. Sync knowledge to make them available."})
}
func (s *server) syncStatus(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	send(w, http.StatusOK, s.state)
}
func (s *server) startSync(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	if s.state.Status == "running" {
		state := s.state
		s.mu.Unlock()
		send(w, http.StatusConflict, state)
		return
	}
	now := time.Now().UTC()
	s.state = syncState{Status: "running", Message: "Starting synchronization…", Started: &now}
	state := s.state
	s.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(s.root, 30*time.Minute)
		defer cancel()
		err := s.backend.Sync(ctx, func(message string) { s.mu.Lock(); s.state.Message = message; s.mu.Unlock() })
		s.mu.Lock()
		defer s.mu.Unlock()
		finished := time.Now().UTC()
		s.state.Finished = &finished
		if err != nil {
			s.state.Status = "failed"
			s.state.Message = err.Error()
		} else {
			s.state.Status = "complete"
		}
	}()
	send(w, http.StatusAccepted, state)
}
