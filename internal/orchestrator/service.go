package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

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
}

func NewService(root string, cfg config.Config, store *db.Store) (*Service, error) {
	evaluator, err := policy.Load(root)
	if err != nil {
		return nil, err
	}
	return &Service{
		root:      root,
		cfg:       cfg,
		store:     store,
		proposals: proposals.NewService(root, store, evaluator),
	}, nil
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
	return nil
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
