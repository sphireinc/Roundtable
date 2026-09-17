package httpapi

import "net/http"

type phaseResponse struct {
	ID            string         `json:"id"`
	TransactionID string         `json:"transaction_id"`
	Phase         string         `json:"phase"`
	Status        string         `json:"status"`
	Actor         string         `json:"actor"`
	Reason        string         `json:"reason,omitempty"`
	RequestID     string         `json:"request_id,omitempty"`
	Inputs        map[string]any `json:"inputs"`
	Outputs       map[string]any `json:"outputs"`
	LogRef        string         `json:"log_ref,omitempty"`
	FailureCode   string         `json:"failure_code,omitempty"`
	RecoveryState string         `json:"recovery_state"`
	StartedAt     string         `json:"started_at,omitempty"`
	EndedAt       string         `json:"ended_at,omitempty"`
	CreatedAt     string         `json:"created_at"`
}
type recoveryResponse struct {
	TransactionID   string   `json:"transaction_id"`
	Status          string   `json:"status"`
	RepositoryState string   `json:"repository_state"`
	Actions         []string `json:"actions"`
	BlockedReason   string   `json:"blocked_reason,omitempty"`
}

func (s *Server) listTransactionPhasesAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	if _, err = s.readTransaction(r, workspace.ID, r.PathValue("transaction_id")); err != nil {
		WriteProblem(w, r, 404, "transaction_not_found", "Transaction not found", err.Error())
		return
	}
	rows, err := s.config.Store.DB().QueryContext(r.Context(), `SELECT id,transaction_id,phase,status,actor,COALESCE(reason,''),COALESCE(request_id,''),COALESCE(inputs_json,'{}'),COALESCE(outputs_json,'{}'),COALESCE(log_ref,''),COALESCE(failure_code,''),COALESCE(recovery_state,'recoverable'),COALESCE(started_at,''),COALESCE(ended_at,''),created_at FROM transaction_phases WHERE transaction_id=? ORDER BY created_at,id`, r.PathValue("transaction_id"))
	if err != nil {
		WriteProblem(w, r, 500, "phase_list_failed", "Unable to list transaction phases", err.Error())
		return
	}
	defer rows.Close()
	items := make([]phaseResponse, 0)
	for rows.Next() {
		var v phaseResponse
		var reason, request, inputs, outputs string
		if err := rows.Scan(&v.ID, &v.TransactionID, &v.Phase, &v.Status, &v.Actor, &reason, &request, &inputs, &outputs, &v.LogRef, &v.FailureCode, &v.RecoveryState, &v.StartedAt, &v.EndedAt, &v.CreatedAt); err != nil {
			WriteProblem(w, r, 500, "phase_list_failed", "Unable to list transaction phases", err.Error())
			return
		}
		v.Reason = redactText(reason)
		v.RequestID = request
		v.Inputs = decodeMap(inputs)
		v.Outputs = decodeMap(outputs)
		items = append(items, v)
	}
	writePage(w, r, items)
}

func (s *Server) transactionRecoveryAPI(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.workspace(r)
	if err != nil {
		writeWorkspaceError(w, r, err)
		return
	}
	tx, err := s.readTransaction(r, workspace.ID, r.PathValue("transaction_id"))
	if err != nil {
		WriteProblem(w, r, 404, "transaction_not_found", "Transaction not found", err.Error())
		return
	}
	value := recoveryResponse{TransactionID: tx.ID, Status: tx.Status, RepositoryState: "unknown", Actions: []string{}}
	switch tx.Status {
	case "pending":
		value.Actions = []string{"stage", "validate", "cancel"}
	case "staged":
		value.Actions = []string{"validate", "apply", "cancel"}
	case "failed":
		value.Actions = []string{"retry", "cancel", "compensate"}
	case "cancelled":
		value.Actions = []string{"compensate"}
	case "applied":
		value.RepositoryState = "verified"
	default:
		value.BlockedReason = "transaction state has no safe recovery action"
	}
	writeJSON(w, 200, value)
}
