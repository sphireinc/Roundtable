package sessions

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"roundtable/internal/db"
)

type Service struct {
	store *db.Store
}

type RegisterRequest struct {
	ID                    string
	AgentID               string
	RunID                 string
	Adapter               string
	Provider              string
	Model                 string
	ExternalSessionID     string
	ExternalResumeCommand string
	WorkingDirectory      string
	MCPSocket             string
	Status                string
	Metadata              map[string]any
}

type ResumeBriefing struct {
	Session            db.AgentSession
	Task               *db.Task
	Claims             []db.Claim
	PendingProposals   []db.Proposal
	LatestDecisions    []db.Decision
	RequiredNextAction string
	Markdown           string
}

func NewService(store *db.Store) *Service {
	return &Service{store: store}
}

func (s *Service) Register(ctx context.Context, req RegisterRequest) (db.AgentSession, error) {
	metadata := ""
	if len(req.Metadata) > 0 {
		body, err := json.Marshal(req.Metadata)
		if err != nil {
			return db.AgentSession{}, err
		}
		metadata = string(body)
	}
	session := db.AgentSession{
		ID:                    req.ID,
		AgentID:               req.AgentID,
		RunID:                 req.RunID,
		Adapter:               req.Adapter,
		Provider:              req.Provider,
		Model:                 req.Model,
		ExternalSessionID:     req.ExternalSessionID,
		ExternalResumeCommand: req.ExternalResumeCommand,
		WorkingDirectory:      req.WorkingDirectory,
		MCPSocket:             req.MCPSocket,
		Status:                defaultString(req.Status, "active"),
		LastSeenAt:            time.Now().UTC().Format(time.RFC3339),
		MetadataJSON:          metadata,
	}
	if err := s.store.UpsertAgentSession(ctx, session); err != nil {
		return db.AgentSession{}, err
	}
	saved, err := s.store.GetAgentSession(ctx, session.ID)
	if err != nil {
		return db.AgentSession{}, err
	}
	_, _ = s.store.AppendAgentSessionEvent(ctx, db.AgentSessionEvent{
		SessionID:   saved.ID,
		EventType:   "registered",
		PayloadJSON: mustJSON(map[string]any{"status": saved.Status, "adapter": saved.Adapter}),
	})
	return saved, nil
}

func (s *Service) Heartbeat(ctx context.Context, sessionID, status, externalSessionID string) (db.AgentSession, error) {
	session, err := s.store.GetAgentSession(ctx, sessionID)
	if err != nil {
		return db.AgentSession{}, err
	}
	if status != "" {
		session.Status = status
	}
	if externalSessionID != "" {
		session.ExternalSessionID = externalSessionID
	}
	session.LastSeenAt = time.Now().UTC().Format(time.RFC3339)
	if err := s.store.UpsertAgentSession(ctx, session); err != nil {
		return db.AgentSession{}, err
	}
	saved, err := s.store.GetAgentSession(ctx, sessionID)
	if err != nil {
		return db.AgentSession{}, err
	}
	_, _ = s.store.AppendAgentSessionEvent(ctx, db.AgentSessionEvent{
		SessionID:   saved.ID,
		EventType:   "heartbeat",
		PayloadJSON: mustJSON(map[string]any{"status": saved.Status, "last_seen_at": saved.LastSeenAt}),
	})
	return saved, nil
}

func (s *Service) End(ctx context.Context, sessionID, status string) (db.AgentSession, error) {
	session, err := s.store.GetAgentSession(ctx, sessionID)
	if err != nil {
		return db.AgentSession{}, err
	}
	session.Status = defaultString(status, "ended")
	session.EndedAt = time.Now().UTC().Format(time.RFC3339)
	session.LastSeenAt = session.EndedAt
	if err := s.store.UpsertAgentSession(ctx, session); err != nil {
		return db.AgentSession{}, err
	}
	saved, err := s.store.GetAgentSession(ctx, sessionID)
	if err != nil {
		return db.AgentSession{}, err
	}
	_, _ = s.store.AppendAgentSessionEvent(ctx, db.AgentSessionEvent{
		SessionID:   saved.ID,
		EventType:   "ended",
		PayloadJSON: mustJSON(map[string]any{"status": saved.Status, "ended_at": saved.EndedAt}),
	})
	return saved, nil
}

func (s *Service) GetSession(ctx context.Context, sessionID string) (db.AgentSession, error) {
	return s.store.GetAgentSession(ctx, sessionID)
}

func (s *Service) ListSessions(ctx context.Context) ([]db.AgentSession, error) {
	return s.store.ListAgentSessions(ctx)
}

func (s *Service) FindSessionByAgent(ctx context.Context, agentID string) (db.AgentSession, error) {
	sessions, err := s.store.ListAgentSessions(ctx)
	if err != nil {
		return db.AgentSession{}, err
	}
	var matches []db.AgentSession
	for _, session := range sessions {
		if session.AgentID == agentID {
			matches = append(matches, session)
		}
	}
	if len(matches) == 0 {
		return db.AgentSession{}, fmt.Errorf("no agent session found for %s", agentID)
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Status != matches[j].Status {
			return matches[i].Status == "active"
		}
		if matches[i].LastSeenAt != matches[j].LastSeenAt {
			return matches[i].LastSeenAt > matches[j].LastSeenAt
		}
		return matches[i].StartedAt > matches[j].StartedAt
	})
	return matches[0], nil
}

func (s *Service) BuildResumeBriefing(ctx context.Context, sessionID string) (ResumeBriefing, error) {
	session, err := s.store.GetAgentSession(ctx, sessionID)
	if err != nil {
		return ResumeBriefing{}, err
	}
	task, err := s.currentTask(ctx, session.AgentID)
	if err != nil {
		return ResumeBriefing{}, err
	}
	claims, err := s.activeClaims(ctx, session.AgentID)
	if err != nil {
		return ResumeBriefing{}, err
	}
	proposals, err := s.pendingProposals(ctx, session.AgentID)
	if err != nil {
		return ResumeBriefing{}, err
	}
	decisions, err := s.latestDecisions(ctx)
	if err != nil {
		return ResumeBriefing{}, err
	}
	nextAction := inferNextAction(task, claims, proposals)

	briefing := ResumeBriefing{
		Session:            session,
		Task:               task,
		Claims:             claims,
		PendingProposals:   proposals,
		LatestDecisions:    decisions,
		RequiredNextAction: nextAction,
	}
	briefing.Markdown = renderMarkdown(briefing)
	return briefing, nil
}

func (s *Service) currentTask(ctx context.Context, agentID string) (*db.Task, error) {
	tasks, err := s.store.ListTasks(ctx)
	if err != nil {
		return nil, err
	}
	var assigned []db.Task
	for _, task := range tasks {
		if task.AssignedAgentID == agentID {
			assigned = append(assigned, task)
		}
	}
	sort.Slice(assigned, func(i, j int) bool {
		if assigned[i].Status != assigned[j].Status {
			return assigned[i].Status == "in_progress"
		}
		if assigned[i].Priority != assigned[j].Priority {
			return assigned[i].Priority < assigned[j].Priority
		}
		return assigned[i].CreatedAt < assigned[j].CreatedAt
	})
	if len(assigned) > 0 {
		return &assigned[0], nil
	}

	claims, err := s.activeClaims(ctx, agentID)
	if err != nil {
		return nil, err
	}
	for _, claim := range claims {
		task, err := s.store.GetTask(ctx, claim.TaskID)
		if err == nil {
			return &task, nil
		}
	}
	return nil, nil
}

func (s *Service) activeClaims(ctx context.Context, agentID string) ([]db.Claim, error) {
	claims, err := s.store.ListClaims(ctx)
	if err != nil {
		return nil, err
	}
	var out []db.Claim
	for _, claim := range claims {
		if claim.AgentID == agentID && claim.Status == "active" {
			out = append(out, claim)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ResourceID < out[j].ResourceID })
	return out, nil
}

func (s *Service) pendingProposals(ctx context.Context, agentID string) ([]db.Proposal, error) {
	proposals, err := s.store.ListProposals(ctx)
	if err != nil {
		return nil, err
	}
	var out []db.Proposal
	for _, proposal := range proposals {
		if proposal.AgentID == agentID && proposal.Status != "accepted" && proposal.Status != "rejected" {
			out = append(out, proposal)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}

func (s *Service) latestDecisions(ctx context.Context) ([]db.Decision, error) {
	decisions, err := s.store.ListDecisions(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(decisions, func(i, j int) bool {
		if decisions[i].CreatedAt != decisions[j].CreatedAt {
			return decisions[i].CreatedAt > decisions[j].CreatedAt
		}
		return decisions[i].ID > decisions[j].ID
	})
	if len(decisions) > 3 {
		decisions = decisions[:3]
	}
	return decisions, nil
}

func inferNextAction(task *db.Task, claims []db.Claim, proposals []db.Proposal) string {
	if len(proposals) > 0 {
		return fmt.Sprintf("Review proposal %s and address any blocking feedback before resubmitting or applying.", proposals[0].ID)
	}
	if task != nil {
		return fmt.Sprintf("Resume task %s: %s", task.ID, task.Title)
	}
	if len(claims) > 0 {
		return fmt.Sprintf("Reconcile active claim %s with current table state before continuing.", claims[0].ID)
	}
	return "Query the table and memory, then wait for task assignment or claim work."
}

func renderMarkdown(briefing ResumeBriefing) string {
	var b strings.Builder
	b.WriteString("# Roundtable Resume Briefing\n\n")
	b.WriteString(fmt.Sprintf("You are resuming as %s.\n\n", briefing.Session.AgentID))
	b.WriteString(fmt.Sprintf("Run: %s\n", briefing.Session.RunID))
	if briefing.Session.ExternalSessionID != "" {
		b.WriteString(fmt.Sprintf("External session: %s\n", briefing.Session.ExternalSessionID))
	}
	b.WriteString("\n")

	b.WriteString("Current task:\n")
	if briefing.Task != nil {
		b.WriteString(fmt.Sprintf("%s %s\n\n", briefing.Task.ID, briefing.Task.Title))
	} else {
		b.WriteString("(no assigned task)\n\n")
	}

	b.WriteString("Your active claims:\n")
	if len(briefing.Claims) == 0 {
		b.WriteString("- none\n\n")
	} else {
		for _, claim := range briefing.Claims {
			b.WriteString(fmt.Sprintf("- %s\n", claim.ResourceID))
		}
		b.WriteString("\n")
	}

	b.WriteString("Pending proposals:\n")
	if len(briefing.PendingProposals) == 0 {
		b.WriteString("- none\n\n")
	} else {
		for _, proposal := range briefing.PendingProposals {
			b.WriteString(fmt.Sprintf("- %s from you: %s\n", proposal.ID, proposal.Status))
		}
		b.WriteString("\n")
	}

	b.WriteString("Latest decisions:\n")
	if len(briefing.LatestDecisions) == 0 {
		b.WriteString("- none\n\n")
	} else {
		for _, decision := range briefing.LatestDecisions {
			b.WriteString(fmt.Sprintf("- %s: %s\n", decision.ID, decision.RationaleMD))
		}
		b.WriteString("\n")
	}

	b.WriteString("Required next action:\n")
	b.WriteString(briefing.RequiredNextAction)
	b.WriteString("\n")
	return b.String()
}

func defaultString(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func mustJSON(value any) string {
	body, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(body)
}
