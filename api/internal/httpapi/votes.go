package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"roundtable/internal/db"
)

type voteResponse struct {
	ID            string  `json:"id"`
	ProposalID    string  `json:"proposal_id"`
	AgentID       string  `json:"agent_id"`
	SessionID     string  `json:"session_id,omitempty"`
	Decision      string  `json:"decision"`
	PolicyWeight  int     `json:"policy_weight"`
	PolicyVersion string  `json:"policy_version"`
	Rationale     string  `json:"rationale,omitempty"`
	Confidence    float64 `json:"confidence,omitempty"`
	CreatedAt     string  `json:"created_at"`
}

type consensusResponse struct {
	ProposalID    string `json:"proposal_id"`
	Outcome       string `json:"outcome"`
	ApproveWeight int    `json:"approve_weight"`
	RejectWeight  int    `json:"reject_weight"`
	AbstainWeight int    `json:"abstain_weight"`
	VetoWeight    int    `json:"veto_weight"`
	Quorum        int    `json:"quorum"`
	Threshold     int    `json:"threshold"`
	VotesCast     int    `json:"votes_cast"`
	PolicyVersion string `json:"policy_version"`
	Explanation   string `json:"explanation"`
	CalculatedAt  string `json:"calculated_at"`
}

func (s *Server) listVotesAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	proposalID := r.PathValue("proposal_id")
	var exists int
	if err := s.config.Store.DB().QueryRowContext(r.Context(), "SELECT COUNT(*) FROM proposals WHERE id = ? AND workspace_id = ?", proposalID, workspace.ID).Scan(&exists); err != nil || exists == 0 {
		WriteProblem(w, r, 404, "proposal_not_found", "Proposal not found", proposalID)
		return
	}
	rows, err := s.config.Store.DB().QueryContext(r.Context(), `SELECT id, proposal_id, agent_id, COALESCE(session_id,''), vote, COALESCE(confidence,0), COALESCE(reason_md,''), COALESCE(policy_weight,1), COALESCE(policy_version,''), created_at FROM votes WHERE proposal_id = ? ORDER BY created_at, id`, proposalID)
	if err != nil {
		WriteProblem(w, r, 500, "vote_list_failed", "Unable to list votes", err.Error())
		return
	}
	defer rows.Close()
	items := make([]voteResponse, 0)
	for rows.Next() {
		var v voteResponse
		if err := rows.Scan(&v.ID, &v.ProposalID, &v.AgentID, &v.SessionID, &v.Decision, &v.Confidence, &v.Rationale, &v.PolicyWeight, &v.PolicyVersion, &v.CreatedAt); err != nil {
			WriteProblem(w, r, 500, "vote_list_failed", "Unable to list votes", err.Error())
			return
		}
		v.Rationale = redactText(v.Rationale)
		items = append(items, v)
	}
	writePage(w, r, items)
}

func (s *Server) getConsensusAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	if err := s.assertProposalWorkspace(r, workspace.ID); err != nil {
		WriteProblem(w, r, 404, "proposal_not_found", "Proposal not found", err.Error())
		return
	}
	value, err := s.calculateConsensus(r, r.PathValue("proposal_id"))
	if err != nil {
		WriteProblem(w, r, 500, "consensus_failed", "Unable to calculate consensus", err.Error())
		return
	}
	writeJSON(w, 200, value)
}

func (s *Server) castVoteAPI(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Roundtable-Orchestrator") != "true" && !humanAuthorized(r) {
		WriteProblem(w, r, 403, "forbidden", "Vote authorization required", "Use the orchestrator boundary or a human role")
		return
	}
	if strings.TrimSpace(r.Header.Get("Idempotency-Key")) == "" {
		WriteProblem(w, r, 400, "idempotency_key_required", "Idempotency key required", "Vote changes require Idempotency-Key")
		return
	}
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	if err := s.assertProposalWorkspace(r, workspace.ID); err != nil {
		WriteProblem(w, r, 404, "proposal_not_found", "Proposal not found", err.Error())
		return
	}
	var input struct {
		ID         string  `json:"id"`
		AgentID    string  `json:"agent_id"`
		SessionID  string  `json:"session_id"`
		Decision   string  `json:"decision"`
		Rationale  string  `json:"rationale"`
		Confidence float64 `json:"confidence"`
	}
	if err := decodeJSON(r, &input); err != nil || strings.TrimSpace(input.AgentID) == "" || strings.TrimSpace(input.Decision) == "" || strings.TrimSpace(input.Rationale) == "" {
		WriteProblem(w, r, 400, "invalid_vote", "Invalid vote", "agent_id, decision, and rationale are required")
		return
	}
	input.Decision = strings.ToLower(strings.TrimSpace(input.Decision))
	if !map[string]bool{"approve": true, "reject": true, "abstain": true, "veto": true}[input.Decision] {
		WriteProblem(w, r, 400, "invalid_vote", "Invalid vote", "decision must be approve, reject, abstain, or veto")
		return
	}
	weight := policyWeight(r.Context(), s.config.Store, input.AgentID)
	version := "default-v1"
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = fmt.Sprintf("vote-%d", time.Now().UnixNano())
	}
	tx, err := s.config.Store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		WriteProblem(w, r, 500, "vote_failed", "Unable to record vote", err.Error())
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(r.Context(), `INSERT INTO votes (id, proposal_id, agent_id, session_id, vote, confidence, reason_md, policy_weight, policy_version) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET agent_id=excluded.agent_id, session_id=excluded.session_id, vote=excluded.vote, confidence=excluded.confidence, reason_md=excluded.reason_md, policy_weight=excluded.policy_weight, policy_version=excluded.policy_version`, id, r.PathValue("proposal_id"), input.AgentID, strings.TrimSpace(input.SessionID), input.Decision, input.Confidence, redactText(input.Rationale), weight, version)
	if err != nil {
		WriteProblem(w, r, 409, "vote_conflict", "Unable to record vote", err.Error())
		return
	}
	consensus, err := calculateConsensusTx(r.Context(), tx, r.PathValue("proposal_id"))
	if err != nil {
		WriteProblem(w, r, 409, "consensus_failed", "Unable to calculate consensus", err.Error())
		return
	}
	snapshot, _ := json.Marshal(consensus)
	_, err = tx.ExecContext(r.Context(), `INSERT INTO consensus_snapshots (id, proposal_id, status, approval_count, rejection_count, abstain_count, policy_version, snapshot_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, fmt.Sprintf("snapshot-%d", time.Now().UnixNano()), r.PathValue("proposal_id"), consensus.Outcome, consensus.ApproveWeight, consensus.RejectWeight, consensus.AbstainWeight, consensus.PolicyVersion, string(snapshot))
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO audit_events (workspace_id, actor_id, action, entity_type, entity_id, request_id, reason, payload_json) VALUES (?, ?, 'vote.cast', 'vote', ?, ?, ?, ?)`, workspace.ID, r.Header.Get("X-Actor-ID"), id, requestID(r.Context()), redactText(input.Rationale), string(snapshot))
	}
	if err != nil || tx.Commit() != nil {
		WriteProblem(w, r, 409, "vote_conflict", "Unable to commit vote", fmt.Sprint(err))
		return
	}
	_, _ = s.config.Store.AppendEvent(r.Context(), db.Event{RunID: r.PathValue("proposal_id"), Type: "vote.cast", ActorID: r.Header.Get("X-Actor-ID"), PayloadJSON: string(snapshot)})
	writeJSON(w, 201, map[string]any{"vote": voteResponse{ID: id, ProposalID: r.PathValue("proposal_id"), AgentID: input.AgentID, SessionID: input.SessionID, Decision: input.Decision, PolicyWeight: weight, PolicyVersion: version, Rationale: redactText(input.Rationale), Confidence: input.Confidence}, "consensus": consensus})
}

func (s *Server) assertProposalWorkspace(r *http.Request, workspaceID string) error {
	var one int
	return s.config.Store.DB().QueryRowContext(r.Context(), "SELECT 1 FROM proposals WHERE id = ? AND workspace_id = ?", r.PathValue("proposal_id"), workspaceID).Scan(&one)
}

func policyWeight(ctx context.Context, store *db.Store, agentID string) int {
	var role string
	if store.DB().QueryRowContext(ctx, "SELECT role FROM agents WHERE id = ?", agentID).Scan(&role) != nil {
		return 1
	}
	switch strings.ToLower(role) {
	case "security":
		return 3
	case "architect", "reviewer":
		return 2
	default:
		return 1
	}
}

func (s *Server) calculateConsensus(r *http.Request, proposalID string) (consensusResponse, error) {
	return calculateConsensusDB(r.Context(), s.config.Store.DB(), proposalID)
}

func calculateConsensusTx(ctx context.Context, tx *sql.Tx, proposalID string) (consensusResponse, error) {
	return calculateConsensusRows(ctx, tx.QueryContext, proposalID)
}

func calculateConsensusDB(ctx context.Context, database *sql.DB, proposalID string) (consensusResponse, error) {
	return calculateConsensusRows(ctx, database.QueryContext, proposalID)
}

type queryFunc func(context.Context, string, ...any) (*sql.Rows, error)

func calculateConsensusRows(ctx context.Context, query queryFunc, proposalID string) (consensusResponse, error) {
	rows, err := query(ctx, `SELECT vote, COALESCE(policy_weight,1), COALESCE(policy_version,'default-v1') FROM votes WHERE proposal_id = ?`, proposalID)
	if err != nil {
		return consensusResponse{}, err
	}
	defer rows.Close()
	value := consensusResponse{ProposalID: proposalID, Quorum: 2, Threshold: 2, CalculatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	for rows.Next() {
		var decision, version string
		var weight int
		if err := rows.Scan(&decision, &weight, &version); err != nil {
			return value, err
		}
		if weight < 1 {
			weight = 1
		}
		value.VotesCast++
		value.PolicyVersion = version
		switch decision {
		case "approve":
			value.ApproveWeight += weight
		case "reject":
			value.RejectWeight += weight
		case "abstain":
			value.AbstainWeight += weight
		case "veto":
			value.VetoWeight += weight
		}
	}
	if value.ApproveWeight >= value.Threshold && value.RejectWeight == 0 && value.VetoWeight == 0 && value.VotesCast >= value.Quorum {
		value.Outcome = "approved"
	} else if value.VetoWeight > 0 || value.RejectWeight > 0 {
		value.Outcome = "blocked"
	} else {
		value.Outcome = "pending"
	}
	value.Explanation = fmt.Sprintf("approve weight %d of threshold %d; reject weight %d; veto weight %d; quorum %d votes", value.ApproveWeight, value.Threshold, value.RejectWeight, value.VetoWeight, value.Quorum)
	return value, rows.Err()
}
