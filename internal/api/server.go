package api

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/agent-sync/agent-sync/internal/config"
	"github.com/agent-sync/agent-sync/internal/context"
	"github.com/agent-sync/agent-sync/internal/db"
	"github.com/agent-sync/agent-sync/internal/groups"
	"github.com/agent-sync/agent-sync/internal/sync"
	"github.com/agent-sync/agent-sync/pkg/types"
)

//go:embed web/*
var webFS embed.FS

type Server struct {
	router   *chi.Mux
	db       *db.DB
	registry *sync.Registry
	store    *context.Store
	merge    *context.MergeEngine
	groupMgr *groups.Manager
	cfg      *config.Config
}

func NewServer(database *db.DB, registry *sync.Registry, store *context.Store, merge *context.MergeEngine, groupMgr *groups.Manager, cfg *config.Config) *Server {
	s := &Server{
		router:   chi.NewRouter(),
		db:       database,
		registry: registry,
		store:    store,
		merge:    merge,
		groupMgr: groupMgr,
		cfg:      cfg,
	}
	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	s.router.Use(middleware.Logger)
	s.router.Use(middleware.Recoverer)
	s.router.Use(corsMiddleware)

	s.router.Get("/api/health", s.handleHealth)
	s.router.Get("/api/stats", s.handleStats)

	s.router.Get("/api/providers", s.handleListProviders)
	s.router.Post("/api/providers/sync", s.handleSyncAll)

	s.router.Get("/api/sessions", s.handleListSessions)
	s.router.Get("/api/sessions/{id}", s.handleGetSession)
	s.router.Get("/api/sessions/{id}/messages", s.handleGetSessionMessages)

	s.router.Get("/api/context", s.handleListContext)
	s.router.Post("/api/context", s.handleSaveContext)
	s.router.Delete("/api/context/{id}", s.handleDeleteContext)
	s.router.Get("/api/context/search", s.handleSearchContext)

	s.router.Post("/api/context/merge", s.handleMergeContext)
	s.router.Get("/api/merges", s.handleListMerges)

	s.router.Get("/api/groups", s.handleListGroups)
	s.router.Post("/api/groups", s.handleCreateGroup)
	s.router.Delete("/api/groups/{id}", s.handleDeleteGroup)

	s.router.Get("/api/search", s.handleSearch)

	webSub, _ := fs.Sub(webFS, "web")
	fileServer := http.FileServer(http.FS(webSub))
	s.router.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}
		r2 := *r
		r2.URL.Path = path
		fileServer.ServeHTTP(w, &r2)
	})
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) ListenAndServe() error {
	addr := fmt.Sprintf(":%d", s.cfg.APIPort)
	fmt.Printf("API server listening on http://localhost%s\n", addr)
	return http.ListenAndServe(addr, s)
}

func respond(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, message string) {
	respond(w, status, map[string]string{"error": message})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(200)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	respond(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := s.db.GetStats()
	respond(w, 200, stats)
}

func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := s.db.ListProviders()
	if err != nil {
		respondError(w, 500, err.Error())
		return
	}
	respond(w, 200, providers)
}

func (s *Server) handleSyncAll(w http.ResponseWriter, r *http.Request) {
	var results []*types.SyncStats
	for _, p := range s.registry.List() {
		stats, err := p.Sync()
		if err != nil {
			respondError(w, 500, err.Error())
			return
		}
		results = append(results, stats)
	}
	respond(w, 200, results)
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	provider := r.URL.Query().Get("provider")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 {
		limit = 50
	}

	sessions, err := s.db.ListSessions(provider, limit, offset)
	if err != nil {
		respondError(w, 500, err.Error())
		return
	}
	respond(w, 200, sessions)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	session, err := s.db.GetSession(id)
	if err != nil {
		respondError(w, 404, "session not found")
		return
	}
	respond(w, 200, session)
}

func (s *Server) handleGetSessionMessages(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	messages, err := s.db.GetSessionMessages(id)
	if err != nil {
		respondError(w, 404, "session not found")
		return
	}
	respond(w, 200, messages)
}

func (s *Server) handleListContext(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 {
		limit = 50
	}

	entries, err := s.store.List(limit, offset)
	if err != nil {
		respondError(w, 500, err.Error())
		return
	}
	respond(w, 200, entries)
}

func (s *Server) handleSaveContext(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content  string   `json:"content"`
		Summary  string   `json:"summary"`
		Source   string   `json:"source"`
		SourceID string   `json:"source_id"`
		Tags     []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "invalid request body")
		return
	}

	entry, err := s.store.Save(req.Content, req.Summary, req.Source, req.SourceID, req.Tags)
	if err != nil {
		respondError(w, 500, err.Error())
		return
	}
	respond(w, 201, entry)
}

func (s *Server) handleDeleteContext(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.store.Delete(id); err != nil {
		respondError(w, 500, err.Error())
		return
	}
	respond(w, 200, map[string]string{"status": "deleted"})
}

func (s *Server) handleSearchContext(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 20
	}

	entries, err := s.store.Search(query, limit)
	if err != nil {
		respondError(w, 500, err.Error())
		return
	}
	respond(w, 200, entries)
}

func (s *Server) handleMergeContext(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EntryIDs []string `json:"entry_ids"`
		Name     string   `json:"name"`
		Strategy string   `json:"strategy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "invalid request body")
		return
	}
	if req.Strategy == "" {
		req.Strategy = "append"
	}

	merged, err := s.merge.Merge(req.EntryIDs, req.Name, req.Strategy)
	if err != nil {
		respondError(w, 400, err.Error())
		return
	}
	respond(w, 201, merged)
}

func (s *Server) handleListMerges(w http.ResponseWriter, r *http.Request) {
	merges, err := s.merge.ListMerges()
	if err != nil {
		respondError(w, 500, err.Error())
		return
	}
	respond(w, 200, merges)
}

func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := s.groupMgr.List()
	if err != nil {
		respondError(w, 500, err.Error())
		return
	}
	respond(w, 200, groups)
}

func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		ProviderIDs []string `json:"provider_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "invalid request body")
		return
	}

	group, err := s.groupMgr.Create(req.Name, req.Description, req.ProviderIDs)
	if err != nil {
		respondError(w, 400, err.Error())
		return
	}
	respond(w, 201, group)
}

func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.groupMgr.Delete(id); err != nil {
		respondError(w, 500, err.Error())
		return
	}
	respond(w, 200, map[string]string{"status": "deleted"})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 20
	}

	messages, _ := s.db.SearchMessages(query, limit)
	contextEntries, _ := s.store.Search(query, limit)

	respond(w, 200, map[string]interface{}{
		"messages": messages,
		"context":  contextEntries,
	})
}
