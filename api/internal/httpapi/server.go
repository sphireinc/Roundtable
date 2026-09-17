package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"roundtable/internal/db"
)

type Config struct {
	Version               string
	Logger                *slog.Logger
	Store                 *db.Store
	AllowedWorkspaceRoots []string
}

type Server struct{ config Config }

type problem struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail,omitempty"`
	Instance  string `json:"instance,omitempty"`
	RequestID string `json:"request_id"`
	Code      string `json:"code,omitempty"`
}

type workspaceInput struct {
	DisplayName   string `json:"display_name"`
	RootAlias     string `json:"root_alias"`
	RootPath      string `json:"root_path"`
	DefaultBranch string `json:"default_branch"`
}
type workspaceResponse struct {
	ID                          string `json:"id"`
	DisplayName                 string `json:"display_name"`
	RootAlias                   string `json:"root_alias,omitempty"`
	CanonicalRepositoryIdentity string `json:"canonical_repository_identity"`
	Status                      string `json:"status"`
	DefaultBranch               string `json:"default_branch,omitempty"`
	CreatedAt                   string `json:"created_at"`
	LastOpenedAt                string `json:"last_opened_at,omitempty"`
	Revision                    int    `json:"revision"`
}
type workspaceDetachResponse struct {
	Workspace workspaceResponse `json:"workspace"`
	Impact    workspaceImpact   `json:"impact"`
}
type workspaceImpact struct {
	ActiveSessions     int `json:"active_sessions"`
	ActiveClaims       int `json:"active_claims"`
	OpenProposals      int `json:"open_proposals"`
	ActiveTransactions int `json:"active_transactions"`
}

type componentHealth struct {
	Status  string         `json:"status"`
	Details map[string]any `json:"details,omitempty"`
}
type nodeHealthResponse struct {
	Status          string                     `json:"status"`
	Version         string                     `json:"version"`
	APIVersion      string                     `json:"api_version"`
	Time            string                     `json:"time"`
	RequestID       string                     `json:"request_id"`
	Components      map[string]componentHealth `json:"components"`
	DegradedReasons []string                   `json:"degraded_reasons,omitempty"`
}
type workspaceHealthResponse struct {
	Workspace       workspaceResponse          `json:"workspace"`
	Status          string                     `json:"status"`
	Components      map[string]componentHealth `json:"components"`
	Impact          workspaceImpact            `json:"impact"`
	DegradedReasons []string                   `json:"degraded_reasons,omitempty"`
	RequestID       string                     `json:"request_id"`
}

func NewServer(cfg Config) *Server {
	if cfg.Version == "" {
		cfg.Version = "dev"
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Server{config: cfg}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", s.health)
	mux.HandleFunc("GET /api/v1/status", s.status)
	mux.HandleFunc("GET /api/v1/workspaces/{id}/health", s.workspaceHealth)
	mux.HandleFunc("GET /api/v1/workspaces", s.listWorkspaces)
	mux.HandleFunc("POST /api/v1/workspaces", s.createWorkspace)
	mux.HandleFunc("GET /api/v1/workspaces/{id}", s.getWorkspace)
	mux.HandleFunc("PATCH /api/v1/workspaces/{id}", s.patchWorkspace)
	mux.HandleFunc("DELETE /api/v1/workspaces/{id}", s.detachWorkspace)
	mux.HandleFunc("GET /api/v1/workspaces/{id}/repository", s.repositoryStatus)
	mux.HandleFunc("POST /api/v1/workspaces/{id}/repository/branch-switch/preflight", s.branchSwitchPreflight)
	mux.HandleFunc("POST /api/v1/workspaces/{id}/repository/branch-switch", s.branchSwitch)
	mux.HandleFunc("GET /api/v1/workspaces/{id}/repository/entities", s.repositoryEntities)
	mux.HandleFunc("GET /api/v1/workspaces/{id}/repository/entities/{entity_id}", s.repositoryEntity)
	mux.HandleFunc("GET /api/v1/workspaces/{id}/agents", s.listAgents)
	mux.HandleFunc("GET /api/v1/workspaces/{id}/agents/{agent_id}", s.getAgent)
	mux.HandleFunc("POST /api/v1/workspaces/{id}/agents/{agent_id}/enable", func(w http.ResponseWriter, r *http.Request) { s.setAgentEnabled(w, r, true) })
	mux.HandleFunc("POST /api/v1/workspaces/{id}/agents/{agent_id}/disable", func(w http.ResponseWriter, r *http.Request) { s.setAgentEnabled(w, r, false) })
	mux.HandleFunc("POST /api/v1/workspaces/{id}/agents/{agent_id}/diagnostics", s.agentDiagnostics)
	mux.HandleFunc("GET /api/v1/workspaces/{id}/sessions", s.listSessions)
	mux.HandleFunc("POST /api/v1/workspaces/{id}/sessions", s.createSession)
	mux.HandleFunc("GET /api/v1/workspaces/{id}/sessions/{session_id}", s.getSession)
	mux.HandleFunc("POST /api/v1/workspaces/{id}/sessions/{session_id}/heartbeat", s.heartbeatSession)
	mux.HandleFunc("POST /api/v1/workspaces/{id}/sessions/{session_id}/pause", s.pauseSession)
	mux.HandleFunc("POST /api/v1/workspaces/{id}/sessions/{session_id}/resume", s.resumeSession)
	mux.HandleFunc("POST /api/v1/workspaces/{id}/sessions/{session_id}/stop", s.stopSession)
	mux.HandleFunc("POST /api/v1/workspaces/{id}/sessions/{session_id}/terminate", s.terminateSession)
	mux.HandleFunc("GET /api/v1/workspaces/{id}/sessions/{session_id}/logs", s.sessionLogs)
	mux.HandleFunc("GET /api/v1/workspaces/{id}/sessions/{session_id}/tool-calls", s.sessionToolCalls)
	mux.HandleFunc("GET /api/v1/workspaces/{id}/sessions/{session_id}/claims", s.sessionClaims)
	mux.HandleFunc("GET /api/v1/workspaces/{id}/sessions/{session_id}/proposals", s.sessionProposals)
	mux.HandleFunc("GET /api/v1/workspaces/{id}/sessions/{session_id}/metrics", s.sessionMetrics)
	mux.HandleFunc("GET /api/v1/workspaces/{id}/sessions/{session_id}/environment", s.sessionEnvironment)
	mux.HandleFunc("GET /api/v1/workspaces/{id}/deliberations", s.listDeliberations)
	mux.HandleFunc("POST /api/v1/workspaces/{id}/deliberations", s.createDeliberation)
	mux.HandleFunc("GET /api/v1/workspaces/{id}/deliberations/{deliberation_id}", s.getDeliberation)
	mux.HandleFunc("POST /api/v1/workspaces/{id}/deliberations/{deliberation_id}/start", s.transitionDeliberation)
	mux.HandleFunc("POST /api/v1/workspaces/{id}/deliberations/{deliberation_id}/pause", s.transitionDeliberation)
	mux.HandleFunc("POST /api/v1/workspaces/{id}/deliberations/{deliberation_id}/resume", s.transitionDeliberation)
	mux.HandleFunc("POST /api/v1/workspaces/{id}/deliberations/{deliberation_id}/terminate", s.transitionDeliberation)
	mux.HandleFunc("POST /api/v1/workspaces/{id}/deliberations/{deliberation_id}/messages", s.addDeliberationMessage)
	mux.HandleFunc("GET /api/v1/workspaces/{id}/deliberations/{deliberation_id}/transcript", s.listTranscript)
	mux.HandleFunc("GET /api/v1/workspaces/{id}/deliberations/{deliberation_id}/transcript/{entry_id}", s.getTranscriptEntry)
	mux.HandleFunc("GET /api/v1/workspaces/{id}/proposals", s.listProposalsAPI)
	mux.HandleFunc("POST /api/v1/workspaces/{id}/proposals/internal", s.createInternalProposal)
	mux.HandleFunc("GET /api/v1/workspaces/{id}/proposals/{proposal_id}", s.getProposalAPI)
	mux.HandleFunc("POST /api/v1/workspaces/{id}/proposals/{proposal_id}/{action}", s.transitionProposal)
	mux.HandleFunc("GET /api/v1/workspaces/{id}/proposals/{proposal_id}/patch", s.patchMetadataAPI)
	mux.HandleFunc("GET /api/v1/workspaces/{id}/proposals/{proposal_id}/patch/files", s.patchFilesAPI)
	mux.HandleFunc("GET /api/v1/workspaces/{id}/proposals/{proposal_id}/patch/diff", s.patchDiffAPI)
	mux.HandleFunc("GET /api/v1/workspaces/{id}/proposals/{proposal_id}/patch/symbol-impact", s.patchSymbolsAPI)
	mux.HandleFunc("POST /api/v1/workspaces/{id}/proposals/{proposal_id}/validation", s.validateProposalAPI)
	mux.HandleFunc("GET /api/v1/workspaces/{id}/claims", s.listClaimsAPI)
	mux.HandleFunc("POST /api/v1/workspaces/{id}/claims/internal", s.createClaimAPI)
	mux.HandleFunc("GET /api/v1/workspaces/{id}/claims/{claim_id}", s.getClaimAPI)
	mux.HandleFunc("POST /api/v1/workspaces/{id}/claims/{claim_id}/{action}", s.transitionClaimAPI)
	mux.HandleFunc("GET /api/v1/workspaces/{id}/claims/contentions", s.listClaimContentions)
	return requestIDs(securityHeaders(jsonDefaults(mux)))
}

func (s *Server) Serve(ctx context.Context, addr string) error {
	if strings.TrimSpace(addr) == "" {
		addr = "127.0.0.1:8080"
	}
	srv := &http.Server{Addr: addr, Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	err := srv.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.config.Version, "request_id": requestID(r.Context())})
}
func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.nodeHealth(r))
}

func (s *Server) nodeHealth(r *http.Request) nodeHealthResponse {
	components := map[string]componentHealth{
		"event_stream":        {Status: "ready"},
		"transaction_manager": {Status: "ready"},
		"repository_index":    {Status: "ready", Details: map[string]any{"configured_roots": len(s.config.AllowedWorkspaceRoots)}},
	}
	degraded := []string{}
	if s.config.Store == nil {
		components["database"] = componentHealth{Status: "unavailable"}
		components["agent_adapters"] = componentHealth{Status: "unknown"}
		degraded = append(degraded, "authoritative database store is unavailable")
	} else {
		var one int
		if err := s.config.Store.DB().QueryRowContext(r.Context(), "SELECT 1").Scan(&one); err != nil {
			components["database"] = componentHealth{Status: "unavailable"}
			degraded = append(degraded, "authoritative database connectivity failed")
		} else {
			var migration int
			if err := s.config.Store.DB().QueryRowContext(r.Context(), "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&migration); err != nil {
				components["database"] = componentHealth{Status: "degraded", Details: map[string]any{"connectivity": "ready"}}
				degraded = append(degraded, "database migration status is unavailable")
			} else {
				components["database"] = componentHealth{Status: "ready", Details: map[string]any{"migration_version": migration}}
			}
		}
		agents, err := s.config.Store.ListAgents(r.Context())
		if err != nil {
			components["agent_adapters"] = componentHealth{Status: "degraded"}
			degraded = append(degraded, "agent adapter aggregate is unavailable")
		} else {
			healthy := 0
			for _, agent := range agents {
				if agent.Status == "ready" || agent.Status == "idle" {
					healthy++
				}
			}
			adapterStatus := "ready"
			if len(agents) > 0 && healthy != len(agents) {
				adapterStatus = "degraded"
				degraded = append(degraded, "one or more agent adapters are not ready")
			}
			components["agent_adapters"] = componentHealth{Status: adapterStatus, Details: map[string]any{"configured": len(agents), "ready": healthy}}
		}
	}
	status := "ok"
	if len(degraded) > 0 {
		status = "degraded"
	}
	return nodeHealthResponse{Status: status, Version: s.config.Version, APIVersion: "v1", Time: nowRFC3339Nano(), RequestID: requestID(r.Context()), Components: components, DegradedReasons: degraded}
}

func (s *Server) workspaceHealth(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	impact, err := s.config.Store.WorkspaceImpact(r.Context(), workspace.ID)
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "workspace_impact_failed", "Unable to determine workspace health", err.Error())
		return
	}
	node := s.nodeHealth(r)
	components := node.Components
	degraded := append([]string(nil), node.DegradedReasons...)
	if workspace.Status != "active" {
		components["workspace"] = componentHealth{Status: workspace.Status}
		degraded = append(degraded, "workspace is detached")
	} else {
		components["workspace"] = componentHealth{Status: "ready"}
	}
	status := "ok"
	if len(degraded) > 0 {
		status = "degraded"
	}
	writeJSON(w, http.StatusOK, workspaceHealthResponse{Workspace: toWorkspaceResponse(workspace), Status: status, Components: components, Impact: toWorkspaceImpact(impact), DegradedReasons: degraded, RequestID: requestID(r.Context())})
}

func (s *Server) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	if s.config.Store == nil {
		WriteProblem(w, r, 503, "store_unavailable", "Workspace store unavailable", "The API is not connected to its authoritative store")
		return
	}
	workspaces, err := s.config.Store.ListWorkspaces(r.Context())
	if err != nil {
		WriteProblem(w, r, 500, "workspace_list_failed", "Unable to list workspaces", err.Error())
		return
	}
	items := make([]workspaceResponse, 0, len(workspaces))
	for _, workspace := range workspaces {
		items = append(items, toWorkspaceResponse(workspace))
	}
	writeJSON(w, 200, map[string]any{"items": items, "next_cursor": nil})
}

func (s *Server) getWorkspace(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	writeJSON(w, 200, toWorkspaceResponse(workspace))
}

func (s *Server) createWorkspace(w http.ResponseWriter, r *http.Request) {
	if !authorized(r) {
		WriteProblem(w, r, 403, "forbidden", "Mutation requires an actor", "Set X-Actor-ID to identify the authorized human actor")
		return
	}
	if s.config.Store == nil {
		WriteProblem(w, r, 503, "store_unavailable", "Workspace store unavailable", "The API is not connected to its authoritative store")
		return
	}
	var input workspaceInput
	if err := decodeJSON(r, &input); err != nil {
		WriteProblem(w, r, 400, "invalid_json", "Invalid workspace request", err.Error())
		return
	}
	root, err := s.validateRoot(input.RootPath)
	if err != nil {
		WriteProblem(w, r, 400, "invalid_workspace_root", "Invalid workspace root", err.Error())
		return
	}
	if strings.TrimSpace(input.DisplayName) == "" {
		input.DisplayName = filepath.Base(root)
	}
	workspace := db.Workspace{ID: newWorkspaceID(), DisplayName: strings.TrimSpace(input.DisplayName), RootAlias: strings.TrimSpace(input.RootAlias), CanonicalRepositoryIdentity: repositoryIdentity(root), Status: "active", DefaultBranch: strings.TrimSpace(input.DefaultBranch), RootPath: root, LastOpenedAt: nowRFC3339Nano(), Revision: 1}
	if err := s.config.Store.CreateWorkspace(r.Context(), workspace, r.Header.Get("X-Actor-ID"), requestID(r.Context())); err != nil {
		WriteProblem(w, r, 409, "workspace_create_conflict", "Workspace could not be created", err.Error())
		return
	}
	w.WriteHeader(201)
	_ = json.NewEncoder(w).Encode(toWorkspaceResponse(workspace))
}

func (s *Server) patchWorkspace(w http.ResponseWriter, r *http.Request) {
	if !authorized(r) {
		WriteProblem(w, r, 403, "forbidden", "Mutation requires an actor", "Set X-Actor-ID to identify the authorized human actor")
		return
	}
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	var input workspaceInput
	if err := decodeJSON(r, &input); err != nil {
		WriteProblem(w, r, 400, "invalid_json", "Invalid workspace request", err.Error())
		return
	}
	if strings.TrimSpace(input.DisplayName) != "" {
		workspace.DisplayName = strings.TrimSpace(input.DisplayName)
	}
	if strings.TrimSpace(input.RootAlias) != "" {
		workspace.RootAlias = strings.TrimSpace(input.RootAlias)
	}
	if strings.TrimSpace(input.DefaultBranch) != "" {
		workspace.DefaultBranch = strings.TrimSpace(input.DefaultBranch)
	}
	workspace.LastOpenedAt = nowRFC3339Nano()
	updated, err := s.config.Store.UpdateWorkspace(r.Context(), workspace, expectedRevision(r, workspace.Revision), r.Header.Get("X-Actor-ID"), requestID(r.Context()))
	if err != nil {
		if errors.Is(err, db.ErrRevisionConflict) {
			WriteProblem(w, r, 409, "workspace_revision_conflict", "Workspace changed", err.Error())
			return
		}
		WriteProblem(w, r, 409, "workspace_update_conflict", "Workspace could not be updated", err.Error())
		return
	}
	writeJSON(w, 200, toWorkspaceResponse(updated))
}

func (s *Server) detachWorkspace(w http.ResponseWriter, r *http.Request) {
	if !authorized(r) {
		WriteProblem(w, r, 403, "forbidden", "Mutation requires an actor", "Set X-Actor-ID to identify the authorized human actor")
		return
	}
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	impact, err := s.config.Store.WorkspaceImpact(r.Context(), workspace.ID)
	if err != nil {
		WriteProblem(w, r, 500, "workspace_impact_failed", "Unable to determine workspace impact", err.Error())
		return
	}
	detached, err := s.config.Store.DetachWorkspaceVersioned(r.Context(), workspace.ID, expectedRevision(r, workspace.Revision), r.Header.Get("X-Actor-ID"), requestID(r.Context()))
	if err != nil {
		if errors.Is(err, db.ErrRevisionConflict) {
			WriteProblem(w, r, 409, "workspace_revision_conflict", "Workspace changed", err.Error())
			return
		}
		WriteProblem(w, r, 409, "workspace_detach_conflict", "Workspace could not be detached", err.Error())
		return
	}
	writeJSON(w, 200, workspaceDetachResponse{Workspace: toWorkspaceResponse(detached), Impact: toWorkspaceImpact(impact)})
}

func (s *Server) workspace(r *http.Request) (db.Workspace, error) {
	if s.config.Store == nil {
		return db.Workspace{}, errors.New("store unavailable")
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		return db.Workspace{}, errors.New("workspace id is required")
	}
	return s.config.Store.GetWorkspace(r.Context(), id)
}

func (s *Server) validateRoot(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", errors.New("root_path is required")
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve root path: %w", err)
	}
	root, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve root symlinks: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", errors.New("root_path must name an existing directory")
	}
	if len(s.config.AllowedWorkspaceRoots) == 0 {
		return "", errors.New("no allowed workspace roots are configured")
	}
	for _, allowed := range s.config.AllowedWorkspaceRoots {
		canonicalAllowed, err := filepath.EvalSymlinks(allowed)
		if err != nil {
			continue
		}
		if root == canonicalAllowed || isWithin(root, canonicalAllowed) {
			return root, nil
		}
	}
	return "", errors.New("root_path is outside configured workspace roots")
}
func isWithin(candidate, parent string) bool {
	rel, err := filepath.Rel(parent, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
func authorized(r *http.Request) bool { return strings.TrimSpace(r.Header.Get("X-Actor-ID")) != "" }
func writeWorkspaceError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, os.ErrNotExist) {
		WriteProblem(w, r, 404, "workspace_not_found", "Workspace not found", err.Error())
		return
	}
	if strings.Contains(err.Error(), "store unavailable") {
		WriteProblem(w, r, 503, "store_unavailable", "Workspace store unavailable", err.Error())
		return
	}
	WriteProblem(w, r, 404, "workspace_not_found", "Workspace not found", err.Error())
}
func toWorkspaceResponse(w db.Workspace) workspaceResponse {
	return workspaceResponse{ID: w.ID, DisplayName: w.DisplayName, RootAlias: w.RootAlias, CanonicalRepositoryIdentity: w.CanonicalRepositoryIdentity, Status: w.Status, DefaultBranch: w.DefaultBranch, CreatedAt: w.CreatedAt, LastOpenedAt: w.LastOpenedAt, Revision: w.Revision}
}
func toWorkspaceImpact(i db.WorkspaceImpact) workspaceImpact {
	return workspaceImpact{ActiveSessions: i.ActiveSessions, ActiveClaims: i.ActiveClaims, OpenProposals: i.OpenProposals, ActiveTransactions: i.ActiveTransactions}
}
func expectedRevision(r *http.Request, fallback int) int {
	value := strings.TrimSpace(r.Header.Get("If-Match"))
	if value == "" {
		return fallback
	}
	revision, err := strconv.Atoi(value)
	if err != nil {
		return -1
	}
	return revision
}
func decodeJSON(r *http.Request, value any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}
func repositoryIdentity(root string) string {
	sum := sha256.Sum256([]byte(root))
	return "path-sha256:" + hex.EncodeToString(sum[:])
}
func newWorkspaceID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("ws-%d", time.Now().UnixNano())
	}
	return "ws-" + hex.EncodeToString(b)
}
func nowRFC3339Nano() string { return time.Now().UTC().Format(time.RFC3339Nano) }

type requestIDKey struct{}

func requestID(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey{}).(string); ok {
		return v
	}
	return "unknown"
}
func newRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return "req-" + hex.EncodeToString(b)
}
func requestIDs(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if id == "" || len(id) > 128 || strings.ContainsAny(id, "\r\n") {
			id = newRequestID()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
func jsonDefaults(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		next.ServeHTTP(w, r)
	})
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func WriteProblem(w http.ResponseWriter, r *http.Request, status int, code, title, detail string) {
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	writeJSON(w, status, problem{Type: "https://roundtable.dev/problems/" + code, Title: title, Status: status, Detail: detail, Instance: r.URL.Path, RequestID: requestID(r.Context()), Code: code})
}
