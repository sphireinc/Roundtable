package httpapi

import (
	"net/http"
	"time"
)

type dashboardSummary struct {
	WorkspaceID          string               `json:"workspace_id"`
	GeneratedAt          string               `json:"generated_at"`
	MeasurementWindow    analyticsWindow      `json:"measurement_window"`
	AgentsOnline         int                  `json:"agents_online"`
	ProposalQueue        proposalQueueSummary `json:"proposal_queue"`
	ConsensusSuccessRate float64              `json:"consensus_success_rate"`
	PolicyPassRate       float64              `json:"policy_pass_rate"`
	TransactionManager   string               `json:"transaction_manager"`
	MCPEnforcement       string               `json:"mcp_enforcement"`
	MemoryOracle         string               `json:"memory_oracle"`
	RunState             string               `json:"run_state"`
	Repository           any                  `json:"repository"`
}
type proposalQueueSummary struct {
	Pending  int `json:"pending"`
	InReview int `json:"in_review"`
	Approved int `json:"approved"`
	Rejected int `json:"rejected"`
}

func (s *Server) dashboardSummaryAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	to := time.Now().UTC()
	from := to.Add(-24 * time.Hour)
	fromDB, toDB := from.Format("2006-01-02 15:04:05.999999999"), to.Format("2006-01-02 15:04:05.999999999")
	value := dashboardSummary{WorkspaceID: workspace.ID, GeneratedAt: to.Format(time.RFC3339Nano), MeasurementWindow: analyticsWindow{From: from.Format(time.RFC3339Nano), To: to.Format(time.RFC3339Nano)}, TransactionManager: "ready", MCPEnforcement: "orchestrator-only", MemoryOracle: "ready"}
	_ = s.config.Store.DB().QueryRowContext(r.Context(), `SELECT COUNT(*) FROM agents WHERE status IN ('ready','idle') AND is_enabled=1`).Scan(&value.AgentsOnline)
	_ = s.config.Store.DB().QueryRowContext(r.Context(), `SELECT COUNT(*) FROM proposals WHERE workspace_id=? AND status='pending'`, workspace.ID).Scan(&value.ProposalQueue.Pending)
	_ = s.config.Store.DB().QueryRowContext(r.Context(), `SELECT COUNT(*) FROM proposals WHERE workspace_id=? AND status='in_review'`, workspace.ID).Scan(&value.ProposalQueue.InReview)
	_ = s.config.Store.DB().QueryRowContext(r.Context(), `SELECT COUNT(*) FROM proposals WHERE workspace_id=? AND status IN ('accepted','approved')`, workspace.ID).Scan(&value.ProposalQueue.Approved)
	_ = s.config.Store.DB().QueryRowContext(r.Context(), `SELECT COUNT(*) FROM proposals WHERE workspace_id=? AND status='rejected'`, workspace.ID).Scan(&value.ProposalQueue.Rejected)
	var finalized, approved int
	_ = s.config.Store.DB().QueryRowContext(r.Context(), `SELECT COUNT(*),COALESCE(SUM(CASE WHEN status='approved' THEN 1 ELSE 0 END),0) FROM consensus_snapshots cs JOIN proposals p ON p.id=cs.proposal_id WHERE p.workspace_id=? AND cs.created_at>=? AND cs.created_at<? AND cs.status IN ('approved','blocked')`, workspace.ID, fromDB, toDB).Scan(&finalized, &approved)
	if finalized > 0 {
		value.ConsensusSuccessRate = float64(approved) / float64(finalized)
	}
	var evaluations, passes int
	_ = s.config.Store.DB().QueryRowContext(r.Context(), `SELECT COUNT(*),COALESCE(SUM(CASE WHEN pe.result='pass' THEN 1 ELSE 0 END),0) FROM policy_evaluations pe JOIN policies p ON p.id=pe.policy_id WHERE p.workspace_id=? AND pe.created_at>=? AND pe.created_at<?`, workspace.ID, fromDB, toDB).Scan(&evaluations, &passes)
	if evaluations > 0 {
		value.PolicyPassRate = float64(passes) / float64(evaluations)
	}
	_ = s.config.Store.DB().QueryRowContext(r.Context(), `SELECT status FROM runs WHERE workspace_id=? ORDER BY started_at DESC LIMIT 1`, workspace.ID).Scan(&value.RunState)
	if value.RunState == "" {
		value.RunState = "stopped"
	}
	repository, repoErr := inspectRepository(r.Context(), workspace)
	if repoErr != nil {
		value.Repository = map[string]any{"status": "unavailable"}
	} else {
		value.Repository = repository
	}
	writeJSON(w, 200, value)
}
