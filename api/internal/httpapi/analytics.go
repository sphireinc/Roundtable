package httpapi

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

type consensusAnalytics struct {
	Window             analyticsWindow      `json:"window"`
	GeneratedAt        string               `json:"generated_at"`
	ProposalCount      int                  `json:"proposal_count"`
	ConsensusCount     int                  `json:"consensus_count"`
	SuccessRate        float64              `json:"success_rate"`
	RejectionRate      float64              `json:"rejection_rate"`
	AbstentionRate     float64              `json:"abstention_rate"`
	MeanSeconds        float64              `json:"mean_time_to_consensus_seconds"`
	P50Seconds         float64              `json:"p50_time_to_consensus_seconds"`
	P95Seconds         float64              `json:"p95_time_to_consensus_seconds"`
	PolicyOverrides    int                  `json:"policy_overrides"`
	HumanInterventions int                  `json:"human_interventions"`
	QuorumFailures     int                  `json:"quorum_failures"`
	AgentParticipation []agentParticipation `json:"agent_participation"`
}

type analyticsWindow struct {
	From string `json:"from"`
	To   string `json:"to"`
}
type agentParticipation struct {
	AgentID     string `json:"agent_id"`
	Votes       int    `json:"votes"`
	Approvals   int    `json:"approvals"`
	Rejections  int    `json:"rejections"`
	Abstentions int    `json:"abstentions"`
	Vetoes      int    `json:"vetoes"`
}

func (s *Server) consensusAnalyticsAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	from, to, err := analyticsRange(r)
	if err != nil {
		WriteProblem(w, r, 400, "invalid_analytics_window", "Invalid analytics window", err.Error())
		return
	}
	fromText, toText := from.Format(time.RFC3339Nano), to.Format(time.RFC3339Nano)
	fromDB, toDB := from.Format("2006-01-02 15:04:05.999999999"), to.Format("2006-01-02 15:04:05.999999999")
	value := consensusAnalytics{Window: analyticsWindow{From: fromText, To: toText}, GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano), AgentParticipation: make([]agentParticipation, 0)}
	if err := s.config.Store.DB().QueryRowContext(r.Context(), `SELECT COUNT(*) FROM proposals WHERE workspace_id = ? AND created_at >= ? AND created_at < ?`, workspace.ID, fromDB, toDB).Scan(&value.ProposalCount); err != nil {
		WriteProblem(w, r, 500, "analytics_failed", "Unable to calculate analytics", err.Error())
		return
	}
	rows, err := s.config.Store.DB().QueryContext(r.Context(), `SELECT cs.status, (julianday(cs.created_at)-julianday(p.created_at))*86400 FROM consensus_snapshots cs JOIN proposals p ON p.id = cs.proposal_id WHERE p.workspace_id = ? AND cs.created_at >= ? AND cs.created_at < ? ORDER BY cs.created_at`, workspace.ID, fromDB, toDB)
	if err != nil {
		WriteProblem(w, r, 500, "analytics_failed", "Unable to calculate analytics", err.Error())
		return
	}
	var durations []float64
	for rows.Next() {
		var status string
		var seconds float64
		if err := rows.Scan(&status, &seconds); err != nil {
			rows.Close()
			WriteProblem(w, r, 500, "analytics_failed", "Unable to calculate analytics", err.Error())
			return
		}
		switch status {
		case "approved":
			value.SuccessRate++
			value.ConsensusCount++
			durations = append(durations, seconds)
		case "blocked":
			value.RejectionRate++
			value.ConsensusCount++
			durations = append(durations, seconds)
		case "pending":
			value.QuorumFailures++
		}
	}
	rows.Close()
	if value.ConsensusCount > 0 {
		value.SuccessRate /= float64(value.ConsensusCount)
		value.RejectionRate /= float64(value.ConsensusCount)
	}
	voteRows, err := s.config.Store.DB().QueryContext(r.Context(), `SELECT v.agent_id, v.vote FROM votes v JOIN proposals p ON p.id = v.proposal_id WHERE p.workspace_id = ? AND v.created_at >= ? AND v.created_at < ?`, workspace.ID, fromDB, toDB)
	if err != nil {
		WriteProblem(w, r, 500, "analytics_failed", "Unable to calculate analytics", err.Error())
		return
	}
	agents := map[string]*agentParticipation{}
	totalVotes := 0
	for voteRows.Next() {
		var agent, decision string
		if err := voteRows.Scan(&agent, &decision); err != nil {
			voteRows.Close()
			WriteProblem(w, r, 500, "analytics_failed", "Unable to calculate analytics", err.Error())
			return
		}
		item := agents[agent]
		if item == nil {
			item = &agentParticipation{AgentID: agent}
			agents[agent] = item
		}
		item.Votes++
		totalVotes++
		switch decision {
		case "approve":
			item.Approvals++
		case "reject":
			item.Rejections++
		case "abstain":
			item.Abstentions++
		case "veto":
			item.Vetoes++
		}
	}
	voteRows.Close()
	if totalVotes > 0 {
		abstain := 0
		for _, item := range agents {
			abstain += item.Abstentions
		}
		value.AbstentionRate = float64(abstain) / float64(totalVotes)
	}
	for _, item := range agents {
		value.AgentParticipation = append(value.AgentParticipation, *item)
	}
	sort.Slice(value.AgentParticipation, func(i, j int) bool { return value.AgentParticipation[i].AgentID < value.AgentParticipation[j].AgentID })
	var interventions int
	_ = s.config.Store.DB().QueryRowContext(r.Context(), `SELECT COUNT(*) FROM audit_events WHERE workspace_id = ? AND created_at >= ? AND created_at < ? AND (action LIKE 'human.%' OR action LIKE '%approval%')`, workspace.ID, fromDB, toDB).Scan(&interventions)
	value.HumanInterventions = interventions
	var overrides int
	_ = s.config.Store.DB().QueryRowContext(r.Context(), `SELECT COUNT(*) FROM human_approvals WHERE status = 'approved' AND override_policy = 1 AND updated_at >= ? AND updated_at < ?`, fromDB, toDB).Scan(&overrides)
	value.PolicyOverrides = overrides
	if len(durations) > 0 {
		total := 0.0
		for _, seconds := range durations {
			total += seconds
		}
		value.MeanSeconds = total / float64(len(durations))
		value.P50Seconds = percentile(durations, .50)
		value.P95Seconds = percentile(durations, .95)
	}
	writeJSON(w, 200, value)
}

func analyticsRange(r *http.Request) (time.Time, time.Time, error) {
	to := time.Now().UTC()
	from := to.Add(-24 * time.Hour)
	var err error
	if raw := strings.TrimSpace(r.URL.Query().Get("from")); raw != "" {
		from, err = time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		from = from.UTC()
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("to")); raw != "" {
		to, err = time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		to = to.UTC()
	}
	if !from.Before(to) || to.Sub(from) > 31*24*time.Hour {
		return time.Time{}, time.Time{}, strconv.ErrRange
	}
	return from, to, nil
}

func percentile(values []float64, fraction float64) float64 {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	index := int(float64(len(sorted)-1)*fraction + .5)
	return sorted[index]
}
