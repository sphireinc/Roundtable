package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"roundtable/internal/config"
	"roundtable/internal/db"
	"roundtable/internal/policy"
	"roundtable/internal/proposals"
)

type Service struct {
	root      string
	cfg       config.Config
	store     *db.Store
	proposals *proposals.Service
	tick      func(context.Context, string) error
}

type LoopOptions struct {
	Interval             time.Duration
	RetryBackoff         time.Duration
	MaxConsecutiveErrors int
	MaxIterations        int
}

type RunOutcome struct {
	Iterations int
	Status     string
	Reason     string
}

var ErrIterationLimit = errors.New("orchestrator iteration limit reached")

func NewService(root string, cfg config.Config, store *db.Store) (*Service, error) {
	evaluator, err := policy.Load(root)
	if err != nil {
		return nil, err
	}
	service := &Service{
		root:      root,
		cfg:       cfg,
		store:     store,
		proposals: proposals.NewService(root, store, evaluator),
	}
	service.tick = service.Tick
	return service, nil
}

func (s *Service) SyncAgents(ctx context.Context) error {
	for _, spec := range s.agentSpecs() {
		if err := s.store.UpsertAgent(ctx, db.Agent{
			ID:        spec.ID,
			Role:      spec.Role,
			Name:      spec.Name,
			Adapter:   spec.Adapter,
			Command:   spec.Command,
			Status:    "idle",
			IsEnabled: true,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) Tick(ctx context.Context, runID string) error {
	if err := s.assignOpenTasks(ctx, runID); err != nil {
		return err
	}
	if err := s.requestPendingProposalReviews(ctx, runID); err != nil {
		return err
	}
	return s.scheduleNextTurnRequest(ctx, runID)
}

// Run keeps applying idempotent orchestration ticks until the run converges,
// becomes fully blocked, is cancelled, or reaches an explicitly configured limit.
func (s *Service) Run(ctx context.Context, runID string, options LoopOptions) (RunOutcome, error) {
	options = normalizeLoopOptions(options)
	outcome := RunOutcome{Status: "active"}
	consecutiveErrors := 0
	ticker := time.NewTicker(options.Interval)
	defer ticker.Stop()

	for {
		if err := ctx.Err(); err != nil {
			outcome.Reason = "context_cancelled"
			return outcome, err
		}
		if options.MaxIterations > 0 && outcome.Iterations >= options.MaxIterations {
			outcome.Reason = "iteration_limit"
			return outcome, ErrIterationLimit
		}
		outcome.Iterations++

		status, reason, cycleErr := s.iterate(ctx, runID)
		if cycleErr != nil {
			if errors.Is(cycleErr, context.Canceled) || errors.Is(cycleErr, context.DeadlineExceeded) {
				outcome.Reason = "context_cancelled"
				return outcome, cycleErr
			}
			consecutiveErrors++
			if consecutiveErrors >= options.MaxConsecutiveErrors {
				outcome.Status = "failed"
				outcome.Reason = "retry_limit_exhausted"
				statusErr := s.setRunStatus(ctx, runID, outcome.Status, "orchestrator.failed", map[string]any{
					"iterations": outcome.Iterations,
					"error":      cycleErr.Error(),
				})
				if statusErr != nil {
					return outcome, fmt.Errorf("orchestrator failed after %d consecutive errors: %w (persist failure status: %v)", consecutiveErrors, cycleErr, statusErr)
				}
				return outcome, fmt.Errorf("orchestrator stopped after %d consecutive errors: %w", consecutiveErrors, cycleErr)
			}
			if eventErr := s.appendRunEvent(ctx, runID, "orchestrator.retry", map[string]any{
				"iteration":        outcome.Iterations,
				"attempt":          consecutiveErrors,
				"max_attempts":     options.MaxConsecutiveErrors,
				"error":            cycleErr.Error(),
				"next_retry_delay": options.RetryBackoff.String(),
			}); eventErr != nil {
				return outcome, fmt.Errorf("record orchestration retry: %w (original error: %v)", eventErr, cycleErr)
			}
			if err := waitContext(ctx, options.RetryBackoff); err != nil {
				outcome.Reason = "context_cancelled"
				return outcome, err
			}
			continue
		}
		consecutiveErrors = 0

		if status == "completed" || status == "blocked" {
			outcome.Status = status
			outcome.Reason = reason
			eventType := "orchestrator." + status
			if err := s.setRunStatus(ctx, runID, status, eventType, map[string]any{
				"iterations": outcome.Iterations,
				"reason":     reason,
			}); err != nil {
				return outcome, err
			}
			return outcome, nil
		}

		if options.MaxIterations > 0 && outcome.Iterations >= options.MaxIterations {
			outcome.Reason = "iteration_limit"
			return outcome, ErrIterationLimit
		}
		select {
		case <-ctx.Done():
			outcome.Reason = "context_cancelled"
			return outcome, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Service) iterate(ctx context.Context, runID string) (string, string, error) {
	if err := s.tick(ctx, runID); err != nil {
		return "", "", err
	}
	return s.convergence(ctx, runID)
}

func normalizeLoopOptions(options LoopOptions) LoopOptions {
	if options.Interval <= 0 {
		options.Interval = time.Second
	}
	if options.RetryBackoff <= 0 {
		options.RetryBackoff = 250 * time.Millisecond
	}
	if options.MaxConsecutiveErrors <= 0 {
		options.MaxConsecutiveErrors = 5
	}
	return options
}

func (s *Service) convergence(ctx context.Context, runID string) (string, string, error) {
	tasks, err := s.store.ListTasks(ctx)
	if err != nil {
		return "", "", err
	}
	proposalsFound, err := s.store.ListProposals(ctx)
	if err != nil {
		return "", "", err
	}
	events, err := s.store.ListEvents(ctx, runID)
	if err != nil {
		return "", "", err
	}
	requests, err := pendingTurnRequests(events)
	if err != nil {
		return "", "", err
	}
	if len(requests) > 0 {
		return "active", "pending_agent_turn", nil
	}
	if len(tasks) == 0 && len(proposalsFound) == 0 {
		return "active", "awaiting_initial_work", nil
	}

	activeTasks, blockedTasks := 0, 0
	for _, task := range tasks {
		switch task.Status {
		case "open", "in_progress", "pending", "review":
			activeTasks++
		case "blocked":
			blockedTasks++
		case "completed", "done", "closed", "cancelled", "canceled", "failed":
		default:
			activeTasks++
		}
	}
	activeProposals := 0
	for _, proposal := range proposalsFound {
		if proposal.Status == "pending" || proposal.Status == "in_review" || proposal.Status == "revision_requested" {
			activeProposals++
		}
	}
	if activeTasks == 0 && activeProposals == 0 && blockedTasks == 0 {
		return "completed", "all_tasks_and_proposals_terminal", nil
	}
	if activeTasks == 0 && activeProposals == 0 && blockedTasks > 0 {
		return "blocked", "all_remaining_tasks_blocked", nil
	}
	return "active", "work_remains", nil
}

func (s *Service) scheduleNextTurnRequest(ctx context.Context, runID string) error {
	events, err := s.store.ListEvents(ctx, runID)
	if err != nil {
		return err
	}
	requests, scheduled, started, completed, err := turnRequestState(events)
	if err != nil {
		return err
	}
	for requestID := range scheduled {
		if _, done := completed[requestID]; done {
			continue
		}
		if _, inProgress := started[requestID]; inProgress {
			return nil
		}
		return nil
	}
	if len(requests) == 0 {
		return nil
	}
	sort.Slice(requests, func(i, j int) bool { return requests[i].ID < requests[j].ID })
	request := requests[0]
	var payload map[string]any
	if err := json.Unmarshal([]byte(request.PayloadJSON), &payload); err != nil {
		return fmt.Errorf("decode turn request event %d: %w", request.ID, err)
	}
	return s.appendRunEvent(ctx, runID, "agent.turn_scheduled", map[string]any{
		"agent_id":         request.ActorID,
		"request_event_id": request.ID,
		"reason":           payload["reason_md"],
		"position":         1,
	})
}

func pendingTurnRequests(events []db.Event) ([]db.Event, error) {
	requests, _, _, _, err := turnRequestState(events)
	return requests, err
}

func turnRequestState(events []db.Event) ([]db.Event, map[int64]struct{}, map[int64]struct{}, map[int64]struct{}, error) {
	scheduled := make(map[int64]struct{})
	started := make(map[int64]struct{})
	completed := make(map[int64]struct{})
	for _, event := range events {
		var payload struct {
			RequestEventID int64 `json:"request_event_id"`
		}
		switch event.Type {
		case "agent.turn_scheduled", "agent.turn_started", "agent.turn_completed":
			if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
				return nil, nil, nil, nil, fmt.Errorf("decode turn lifecycle event %d: %w", event.ID, err)
			}
			switch event.Type {
			case "agent.turn_scheduled":
				scheduled[payload.RequestEventID] = struct{}{}
			case "agent.turn_started":
				started[payload.RequestEventID] = struct{}{}
			case "agent.turn_completed":
				completed[payload.RequestEventID] = struct{}{}
			}
		}
	}
	requests := make([]db.Event, 0)
	for _, event := range events {
		if event.Type != "agent.turn_requested" {
			continue
		}
		if _, ok := completed[event.ID]; ok {
			continue
		}
		requests = append(requests, event)
	}
	return requests, scheduled, started, completed, nil
}

func (s *Service) appendRunEvent(ctx context.Context, runID, eventType string, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.store.AppendEvent(ctx, db.Event{
		RunID:       runID,
		Type:        eventType,
		ActorID:     "chair-1",
		PayloadJSON: string(body),
	})
	return err
}

func (s *Service) setRunStatus(ctx context.Context, runID, status, eventType string, payload map[string]any) error {
	run, err := s.store.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	run.Status = status
	if status == "completed" || status == "blocked" || status == "failed" {
		run.EndedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if err := s.store.UpsertRun(ctx, run); err != nil {
		return err
	}
	return s.appendRunEvent(ctx, runID, eventType, payload)
}

func waitContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type agentSpec struct {
	ID      string
	Name    string
	Role    string
	Adapter string
	Command string
}

func (s *Service) agentSpecs() []agentSpec {
	specs := []agentSpec{
		{ID: "chair-1", Name: "Chair 1", Role: s.cfg.Agents.Chair.Role, Adapter: s.cfg.Agents.Chair.Adapter, Command: s.adapterCommand(s.cfg.Agents.Chair.Adapter)},
		{ID: "architect-1", Name: "Architect 1", Role: s.cfg.Agents.Architect.Role, Adapter: s.cfg.Agents.Architect.Adapter, Command: s.adapterCommand(s.cfg.Agents.Architect.Adapter)},
		{ID: "tester-1", Name: "Tester 1", Role: s.cfg.Agents.Tester.Role, Adapter: s.cfg.Agents.Tester.Adapter, Command: s.adapterCommand(s.cfg.Agents.Tester.Adapter)},
		{ID: "security-1", Name: "Security 1", Role: s.cfg.Agents.Security.Role, Adapter: s.cfg.Agents.Security.Adapter, Command: s.adapterCommand(s.cfg.Agents.Security.Adapter)},
		{ID: "memory-oracle-1", Name: "Memory Oracle 1", Role: s.cfg.Agents.MemoryOracle.Role, Adapter: s.cfg.Agents.MemoryOracle.Adapter, Command: s.adapterCommand(s.cfg.Agents.MemoryOracle.Adapter)},
	}
	for i := 0; i < s.cfg.Agents.Implementers.Count; i++ {
		specs = append(specs, agentSpec{
			ID:      fmt.Sprintf("implementer-%d", i+1),
			Name:    fmt.Sprintf("Implementer %d", i+1),
			Role:    "Implementer",
			Adapter: s.cfg.Agents.Implementers.Adapter,
			Command: s.adapterCommand(s.cfg.Agents.Implementers.Adapter),
		})
	}
	for i := 0; i < s.cfg.Agents.Reviewers.Count; i++ {
		specs = append(specs, agentSpec{
			ID:      fmt.Sprintf("reviewer-%d", i+1),
			Name:    fmt.Sprintf("Reviewer %d", i+1),
			Role:    "Reviewer",
			Adapter: s.cfg.Agents.Reviewers.Adapter,
			Command: s.adapterCommand(s.cfg.Agents.Reviewers.Adapter),
		})
	}
	return specs
}

func (s *Service) assignOpenTasks(ctx context.Context, runID string) error {
	tasks, err := s.store.ListTasks(ctx)
	if err != nil {
		return err
	}
	implementers := s.implementerIDs()
	if len(implementers) == 0 {
		return nil
	}
	next := 0
	for _, task := range tasks {
		if task.AssignedAgentID != "" || task.Status != "open" {
			continue
		}
		task.AssignedAgentID = implementers[next%len(implementers)]
		task.Status = "in_progress"
		next++
		if err := s.store.UpsertTask(ctx, task); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"task_id": task.ID, "assigned_agent_id": task.AssignedAgentID})
		if _, err := s.store.AppendEvent(ctx, db.Event{
			RunID:       runID,
			Type:        "task.assigned",
			ActorID:     "chair-1",
			TaskID:      task.ID,
			PayloadJSON: string(payload),
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) requestPendingProposalReviews(ctx context.Context, runID string) error {
	found, err := s.store.ListProposals(ctx)
	if err != nil {
		return err
	}
	for _, proposal := range found {
		if proposal.Status != "pending" {
			continue
		}
		result, err := s.proposals.ProposalRequestReview(ctx, map[string]any{"proposal_id": proposal.ID})
		if err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{
			"proposal_id": proposal.ID,
			"reviewers":   result["reviewers"],
			"roles":       result["review_roles"],
		})
		if _, err := s.store.AppendEvent(ctx, db.Event{
			RunID:       runID,
			Type:        "proposal.review_requested",
			ActorID:     "chair-1",
			TaskID:      proposal.TaskID,
			PayloadJSON: string(payload),
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) implementerIDs() []string {
	ids := make([]string, 0, s.cfg.Agents.Implementers.Count)
	for i := 0; i < s.cfg.Agents.Implementers.Count; i++ {
		ids = append(ids, fmt.Sprintf("implementer-%d", i+1))
	}
	return ids
}

func (s *Service) adapterCommand(name string) string {
	adapter := s.cfg.Adapters[name]
	return strings.TrimSpace(adapter.Command)
}

func PatchPath(root, proposalID string) string {
	return filepath.Join(root, ".roundtable/patches", proposalID+".diff")
}
