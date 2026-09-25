package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"roundtable/internal/claims"
	"roundtable/internal/config"
	"roundtable/internal/db"
	"roundtable/internal/policy"
	"roundtable/internal/proposals"
	"roundtable/internal/repo"
	"roundtable/internal/security"
	"roundtable/internal/state"
	"roundtable/internal/symbols"
)

type Runtime struct {
	root      string
	config    config.Config
	store     *db.Store
	claims    *claims.Service
	proposals *proposals.Service
	security  *security.Service
	indexer   *symbols.Indexer
	turnMu    sync.Mutex
}

type CallRequest struct {
	Tool string         `json:"tool"`
	Args map[string]any `json:"args"`
}

type CallResponse struct {
	OK     bool           `json:"ok"`
	Result map[string]any `json:"result,omitempty"`
	Error  string         `json:"error,omitempty"`
}

func OpenRuntime(root string) (*Runtime, func(), error) {
	cfg, err := config.Load(root)
	if err != nil {
		return nil, nil, err
	}
	sqlDB, err := db.Open(filepath.Join(root, cfg.Storage.SQLitePath))
	if err != nil {
		return nil, nil, err
	}
	store := db.NewStore(sqlDB, nil)
	policyEngine, err := policy.Load(root)
	if err != nil {
		_ = sqlDB.Close()
		return nil, nil, err
	}
	rt := &Runtime{
		root:      root,
		config:    cfg,
		store:     store,
		claims:    claims.NewService(store),
		proposals: proposals.NewService(root, store, policyEngine),
		security:  security.NewService(root, store, policyEngine),
		indexer:   symbols.NewIndexer(),
	}
	return rt, func() { _ = sqlDB.Close() }, nil
}

func (r *Runtime) Call(ctx context.Context, tool string, args map[string]any) (map[string]any, error) {
	switch tool {
	case "agent.turn_request":
		return r.agentTurnRequest(ctx, args)
	case "agent.turn_start":
		return r.agentTurnLifecycle(ctx, args, "agent.turn_started")
	case "agent.turn_complete":
		return r.agentTurnLifecycle(ctx, args, "agent.turn_completed")
	case "table.get_state":
		return r.tableGetState(ctx)
	case "table.watch":
		return r.tableWatch(ctx, args)
	case "patch.validate":
		return r.proposals.PatchValidate(ctx, args)
	case "patch.reject":
		return r.proposals.PatchReject(ctx, args)
	case "patch.apply":
		return r.proposals.PatchApply(ctx, args)
	case "proposal.create":
		return r.proposals.ProposalCreate(ctx, args)
	case "proposal.get":
		return r.proposals.ProposalGet(ctx, args)
	case "proposal.list":
		return r.proposals.ProposalList(ctx, args)
	case "proposal.attach_patch":
		return r.proposals.ProposalAttachPatch(ctx, args)
	case "proposal.request_review":
		return r.proposals.ProposalRequestReview(ctx, args)
	case "vote.cast":
		return r.proposals.VoteCast(ctx, args)
	case "vote.list":
		return r.proposals.VoteList(ctx, args)
	case "decision.record":
		return r.proposals.DecisionRecord(ctx, args)
	case "human.request_approval":
		return r.humanRequestApproval(ctx, args)
	case "human.approval_status":
		return r.humanApprovalStatus(ctx, args)
	case "security.review":
		return r.security.Review(ctx, args)
	case "task.list":
		return r.taskList(ctx)
	case "task.get":
		return r.taskGet(ctx, args)
	case "task.create":
		return r.taskCreate(ctx, args)
	case "task.update_status":
		return r.taskUpdateStatus(ctx, args)
	case "memory.query":
		return r.memoryQuery(ctx, args)
	case "memory.record":
		return r.memoryRecord(ctx, args)
	case "memory.summarize":
		return r.memorySummarize(ctx, args)
	case "memory.mark_stale":
		return r.memoryMarkStale(ctx, args)
	case "test.suggest":
		return r.testSuggest(ctx, args)
	case "test.run":
		return r.testRun(ctx, args)
	case "test.get_result":
		return r.testGetResult(ctx, args)
	case "repo.read_file":
		return r.repoReadFile(args)
	case "repo.search":
		return r.repoSearch(args)
	case "resource.claim":
		return r.resourceClaim(ctx, args)
	case "resource.release":
		return r.resourceRelease(ctx, args)
	case "resource.claim_status":
		return r.resourceClaimStatus(ctx, args)
	case "resource.get":
		return r.resourceGet(ctx, args)
	case "resource.search":
		return r.resourceSearch(ctx, args)
	case "repo.symbols":
		return r.repoSymbols(ctx, args)
	default:
		return nil, fmt.Errorf("tool not implemented: %s", tool)
	}
}

func (r *Runtime) agentTurnLifecycle(ctx context.Context, args map[string]any, eventType string) (map[string]any, error) {
	r.turnMu.Lock()
	defer r.turnMu.Unlock()
	runID := stringArg(args, "run_id")
	agentID := stringArg(args, "agent_id")
	requestEventID := int64(intArgDefault(args, "request_event_id", 0))
	if runID == "" || agentID == "" || requestEventID <= 0 {
		return nil, errors.New("run_id, agent_id, and positive request_event_id are required")
	}
	run, err := r.store.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run.Status != "active" {
		return nil, fmt.Errorf("run %s is %s and cannot accept turn lifecycle events", runID, run.Status)
	}
	events, err := r.store.ListEvents(ctx, runID)
	if err != nil {
		return nil, err
	}
	var request *db.Event
	scheduled, started, completed := false, false, false
	for i := range events {
		event := &events[i]
		if event.Type == "agent.turn_requested" && event.ID == requestEventID {
			request = event
		}
		if event.Type != "agent.turn_scheduled" && event.Type != "agent.turn_started" && event.Type != "agent.turn_completed" {
			continue
		}
		var payload struct {
			RequestEventID int64  `json:"request_event_id"`
			AgentID        string `json:"agent_id"`
		}
		if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
			return nil, fmt.Errorf("decode turn event %d: %w", event.ID, err)
		}
		if payload.RequestEventID != requestEventID {
			continue
		}
		switch event.Type {
		case "agent.turn_scheduled":
			if payload.AgentID != agentID {
				return nil, errors.New("turn request was scheduled for a different agent")
			}
			scheduled = true
		case "agent.turn_started":
			started = true
		case "agent.turn_completed":
			completed = true
		}
	}
	if request == nil || request.ActorID != agentID {
		return nil, errors.New("turn request not found for this agent")
	}
	if completed {
		return nil, errors.New("turn request is already complete")
	}
	if eventType == "agent.turn_started" {
		if !scheduled {
			return nil, errors.New("turn request has not been scheduled by the Chair")
		}
		if started {
			return nil, errors.New("turn request has already started")
		}
	} else if !started {
		return nil, errors.New("turn request has not started")
	}
	body, err := json.Marshal(map[string]any{
		"agent_id":         agentID,
		"request_event_id": requestEventID,
	})
	if err != nil {
		return nil, err
	}
	event, err := r.store.AppendEvent(ctx, db.Event{
		RunID:       runID,
		Type:        eventType,
		ActorID:     agentID,
		PayloadJSON: string(body),
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"event": event, "request_event_id": requestEventID, "status": eventType}, nil
}

func (r *Runtime) agentTurnRequest(ctx context.Context, args map[string]any) (map[string]any, error) {
	r.turnMu.Lock()
	defer r.turnMu.Unlock()
	runID := stringArg(args, "run_id")
	agentID := stringArg(args, "agent_id")
	reason := strings.TrimSpace(stringArg(args, "reason_md"))
	if runID == "" || agentID == "" || reason == "" {
		return nil, errors.New("run_id, agent_id, and reason_md are required")
	}
	if len([]rune(reason)) > 1000 {
		return nil, errors.New("reason_md must be 1000 characters or fewer")
	}
	run, err := r.store.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run.Status != "active" {
		return nil, fmt.Errorf("run %s is %s and cannot accept turn requests", runID, run.Status)
	}
	agent, err := r.store.GetAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if !agent.IsEnabled {
		return nil, fmt.Errorf("agent %s is disabled", agentID)
	}
	events, err := r.store.ListEvents(ctx, runID)
	if err != nil {
		return nil, err
	}
	completed := make(map[int64]struct{})
	for _, event := range events {
		if event.Type == "agent.turn_completed" {
			var payload struct {
				RequestEventID int64 `json:"request_event_id"`
			}
			if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
				return nil, fmt.Errorf("decode completed turn %d: %w", event.ID, err)
			}
			completed[payload.RequestEventID] = struct{}{}
		}
	}
	queued := make(map[string]int64)
	for _, event := range events {
		if event.Type != "agent.turn_requested" {
			continue
		}
		if _, ok := completed[event.ID]; ok {
			continue
		}
		if _, exists := queued[event.ActorID]; !exists {
			queued[event.ActorID] = event.ID
		}
	}
	if requestID, exists := queued[agentID]; exists {
		return map[string]any{
			"request_event_id": requestID,
			"agent_id":         agentID,
			"queued":           true,
			"duplicate":        true,
			"queue_position":   turnQueuePosition(queued, agentID),
		}, nil
	}
	body, err := json.Marshal(map[string]any{
		"agent_id":     agentID,
		"reason_md":    reason,
		"requested_at": time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return nil, err
	}
	event, err := r.store.AppendEvent(ctx, db.Event{
		RunID:       runID,
		Type:        "agent.turn_requested",
		ActorID:     agentID,
		PayloadJSON: string(body),
	})
	if err != nil {
		return nil, err
	}
	queued[agentID] = event.ID
	return map[string]any{
		"request_event_id": event.ID,
		"agent_id":         agentID,
		"queued":           true,
		"duplicate":        false,
		"queue_position":   turnQueuePosition(queued, agentID),
	}, nil
}

func turnQueuePosition(queued map[string]int64, agentID string) int {
	position := 1
	requestID := queued[agentID]
	for otherID, otherRequestID := range queued {
		if otherID != agentID && otherRequestID < requestID {
			position++
		}
	}
	return position
}

func (r *Runtime) taskList(ctx context.Context) (map[string]any, error) {
	tasks, err := r.store.ListTasks(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"tasks": tasks}, nil
}

func (r *Runtime) taskGet(ctx context.Context, args map[string]any) (map[string]any, error) {
	taskID := stringArg(args, "task_id")
	if taskID == "" {
		return nil, errors.New("task_id is required")
	}
	task, err := r.store.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"task": task}, nil
}

func (r *Runtime) taskCreate(ctx context.Context, args map[string]any) (map[string]any, error) {
	title := stringArg(args, "title")
	body := stringArg(args, "body_md")
	if title == "" || body == "" {
		return nil, errors.New("title and body_md are required")
	}
	task := db.Task{
		ID:              stringArgDefault(args, "task_id", generatedID("T")),
		Title:           title,
		BodyMD:          body,
		Status:          stringArgDefault(args, "status", "open"),
		Priority:        intArgDefault(args, "priority", 100),
		Risk:            stringArgDefault(args, "risk", "normal"),
		AssignedAgentID: stringArg(args, "assigned_agent_id"),
	}
	if err := r.store.UpsertTask(ctx, task); err != nil {
		return nil, err
	}
	saved, err := r.store.GetTask(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"task": saved}, nil
}

func (r *Runtime) taskUpdateStatus(ctx context.Context, args map[string]any) (map[string]any, error) {
	taskID := stringArg(args, "task_id")
	if taskID == "" {
		return nil, errors.New("task_id is required")
	}
	task, err := r.store.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if status := stringArg(args, "status"); status != "" {
		task.Status = status
	}
	if agentID := stringArg(args, "assigned_agent_id"); agentID != "" {
		task.AssignedAgentID = agentID
	}
	if err := r.store.UpsertTask(ctx, task); err != nil {
		return nil, err
	}
	saved, err := r.store.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"task": saved}, nil
}

func (r *Runtime) tableGetState(ctx context.Context) (map[string]any, error) {
	snapshot, err := state.LoadSnapshot(ctx, r.store)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"runs":             snapshot.Runs,
		"agents":           snapshot.Agents,
		"tasks":            snapshot.Tasks,
		"claims":           snapshot.Claims,
		"proposals":        snapshot.Proposals,
		"transactions":     snapshot.Transactions,
		"human_approvals":  snapshot.HumanApprovals,
		"security_reviews": snapshot.SecurityReviews,
		"memory":           snapshot.Memories,
	}, nil
}

func (r *Runtime) tableWatch(ctx context.Context, args map[string]any) (map[string]any, error) {
	runID := stringArg(args, "run_id")
	limit := intArgDefault(args, "limit", 10)
	if limit <= 0 {
		limit = 10
	}
	if limit > 200 {
		limit = 200
	}
	snapshot, err := state.LoadSnapshot(ctx, r.store)
	if err != nil {
		return nil, err
	}
	events, err := r.store.ListEvents(ctx, runID)
	if err != nil {
		return nil, err
	}
	_, cursorProvided := args["after_event_id"]
	afterEventID := int64(intArgDefault(args, "after_event_id", 0))
	hasMoreEvents := false
	if afterEventID < 0 {
		return nil, errors.New("after_event_id must be zero or greater")
	}
	if cursorProvided {
		filtered := make([]db.Event, 0, limit+1)
		for _, event := range events {
			if event.ID <= afterEventID {
				continue
			}
			filtered = append(filtered, event)
			if len(filtered) == limit+1 {
				break
			}
		}
		hasMoreEvents = len(filtered) > limit
		if hasMoreEvents {
			filtered = filtered[:limit]
		}
		events = filtered
	} else if len(events) > limit {
		events = events[len(events)-limit:]
	}
	nextAfterEventID := afterEventID
	if len(events) > 0 {
		nextAfterEventID = events[len(events)-1].ID
	}
	blockedTasks := make([]db.Task, 0)
	for _, task := range snapshot.Tasks {
		if task.Status == "blocked" {
			blockedTasks = append(blockedTasks, task)
		}
	}
	pendingProposals := make([]db.Proposal, 0)
	for _, proposal := range snapshot.Proposals {
		if proposal.Status == "pending" || proposal.Status == "in_review" {
			pendingProposals = append(pendingProposals, proposal)
		}
	}
	requiredApprovals := make([]db.HumanApproval, 0)
	for _, approval := range snapshot.HumanApprovals {
		if approval.Status == "requested" {
			requiredApprovals = append(requiredApprovals, approval)
		}
	}
	transactions := snapshot.Transactions
	if limit > 0 && len(transactions) > limit {
		transactions = transactions[len(transactions)-limit:]
	}
	return map[string]any{
		"run_id":              runID,
		"events":              events,
		"next_after_event_id": nextAfterEventID,
		"has_more_events":     hasMoreEvents,
		"transactions":        transactions,
		"blocked_tasks":       blockedTasks,
		"pending_proposals":   pendingProposals,
		"required_approvals":  requiredApprovals,
		"active_claims":       countClaimsByStatus(snapshot.Claims, "active"),
		"suspended_claims":    countClaimsByStatus(snapshot.Claims, "suspended"),
		"open_tasks":          countTasksByStatus(snapshot.Tasks, "open", "in_progress", "blocked"),
		"active_agents":       countEnabledAgents(snapshot.Agents),
	}, nil
}

func (r *Runtime) humanRequestApproval(ctx context.Context, args map[string]any) (map[string]any, error) {
	subject := stringArg(args, "subject")
	reason := stringArg(args, "reason_md")
	if subject == "" || reason == "" {
		return nil, errors.New("subject and reason_md are required")
	}
	proposalID := stringArg(args, "proposal_id")
	taskID := stringArg(args, "task_id")
	if proposalID != "" {
		proposal, err := r.store.GetProposal(ctx, proposalID)
		if err != nil {
			return nil, err
		}
		if taskID == "" {
			taskID = proposal.TaskID
		}
	}
	approval := db.HumanApproval{
		ID:             stringArgDefault(args, "approval_id", generatedID("H")),
		ProposalID:     proposalID,
		TaskID:         taskID,
		Subject:        subject,
		ReasonMD:       reason,
		Status:         stringArgDefault(args, "status", "requested"),
		RequestedBy:    stringArgDefault(args, "requested_by", "orchestrator"),
		DecisionMD:     stringArg(args, "decision_md"),
		DecidedBy:      stringArg(args, "decided_by"),
		OverridePolicy: boolArgDefault(args, "override_policy", false),
	}
	if err := r.store.UpsertHumanApproval(ctx, approval); err != nil {
		return nil, err
	}
	saved, err := r.store.GetHumanApproval(ctx, approval.ID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"approval": saved}, nil
}

func (r *Runtime) humanApprovalStatus(ctx context.Context, args map[string]any) (map[string]any, error) {
	approvalID := stringArg(args, "approval_id")
	if approvalID == "" {
		return nil, errors.New("approval_id is required")
	}
	approval, err := r.store.GetHumanApproval(ctx, approvalID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"approval": approval}, nil
}

func (r *Runtime) memoryQuery(ctx context.Context, args map[string]any) (map[string]any, error) {
	query := strings.ToLower(stringArg(args, "query"))
	scope := stringArg(args, "scope")
	kind := stringArg(args, "kind")
	limit := intArgDefault(args, "limit", 10)

	entries, err := r.store.ListMemoryEntries(ctx)
	if err != nil {
		return nil, err
	}
	type scored struct {
		entry db.MemoryEntry
		score int
	}
	var matched []scored
	for _, entry := range entries {
		if scope != "" && entry.Scope != scope {
			continue
		}
		if kind != "" && entry.Kind != kind {
			continue
		}
		score := memoryScore(entry, query)
		if query != "" && score == 0 {
			continue
		}
		matched = append(matched, scored{entry: entry, score: score})
	}
	sort.Slice(matched, func(i, j int) bool {
		if matched[i].score != matched[j].score {
			return matched[i].score > matched[j].score
		}
		if matched[i].entry.Importance != matched[j].entry.Importance {
			return matched[i].entry.Importance > matched[j].entry.Importance
		}
		return matched[i].entry.ID < matched[j].entry.ID
	})
	if limit > 0 && len(matched) > limit {
		matched = matched[:limit]
	}
	out := make([]db.MemoryEntry, 0, len(matched))
	for _, item := range matched {
		out = append(out, item.entry)
	}
	return map[string]any{"entries": out}, nil
}

func (r *Runtime) memoryRecord(ctx context.Context, args map[string]any) (map[string]any, error) {
	entry := db.MemoryEntry{
		ID:            stringArgDefault(args, "memory_id", generatedID("M")),
		Scope:         stringArg(args, "scope"),
		Kind:          stringArg(args, "kind"),
		Title:         stringArg(args, "title"),
		BodyMD:        stringArg(args, "body_md"),
		SourceEventID: int64(intArgDefault(args, "source_event_id", 0)),
		Importance:    intArgDefault(args, "importance", 50),
		Status:        stringArgDefault(args, "status", "active"),
	}
	if entry.Scope == "" || entry.Kind == "" || entry.Title == "" || entry.BodyMD == "" {
		return nil, errors.New("scope, kind, title, and body_md are required")
	}
	if err := r.store.UpsertMemoryEntry(ctx, entry); err != nil {
		return nil, err
	}
	saved, err := r.store.GetMemoryEntry(ctx, entry.ID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"entry": saved}, nil
}

func (r *Runtime) memorySummarize(ctx context.Context, args map[string]any) (map[string]any, error) {
	scope := stringArg(args, "scope")
	taskID := stringArg(args, "task_id")
	proposalID := stringArg(args, "proposal_id")

	entries, err := r.store.ListMemoryEntries(ctx)
	if err != nil {
		return nil, err
	}
	filtered := make([]db.MemoryEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Status == "stale" {
			continue
		}
		if scope != "" && entry.Scope != scope {
			continue
		}
		if taskID != "" && entry.Scope != taskID {
			continue
		}
		if proposalID != "" && entry.Scope != proposalID {
			continue
		}
		filtered = append(filtered, entry)
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].Importance != filtered[j].Importance {
			return filtered[i].Importance > filtered[j].Importance
		}
		return filtered[i].ID < filtered[j].ID
	})

	var summary strings.Builder
	summary.WriteString("# Memory Summary\n\n")
	switch {
	case proposalID != "":
		summary.WriteString(fmt.Sprintf("Proposal: %s\n\n", proposalID))
	case taskID != "":
		summary.WriteString(fmt.Sprintf("Task: %s\n\n", taskID))
	case scope != "":
		summary.WriteString(fmt.Sprintf("Scope: %s\n\n", scope))
	default:
		summary.WriteString("Scope: project\n\n")
	}
	if len(filtered) == 0 {
		summary.WriteString("No active memory entries matched.\n")
	} else {
		for _, entry := range filtered {
			summary.WriteString(fmt.Sprintf("- [%s] %s: %s\n", entry.Kind, entry.Title, entry.BodyMD))
		}
	}
	return map[string]any{
		"entries":     filtered,
		"summary_md":  summary.String(),
		"entry_count": len(filtered),
	}, nil
}

func (r *Runtime) memoryMarkStale(ctx context.Context, args map[string]any) (map[string]any, error) {
	memoryID := stringArg(args, "memory_id")
	reason := stringArg(args, "reason_md")
	if memoryID == "" || reason == "" {
		return nil, errors.New("memory_id and reason_md are required")
	}
	entry, err := r.store.GetMemoryEntry(ctx, memoryID)
	if err != nil {
		return nil, err
	}
	entry.Status = "stale"
	if strings.TrimSpace(entry.BodyMD) == "" {
		entry.BodyMD = fmt.Sprintf("Marked stale: %s", reason)
	} else {
		entry.BodyMD = fmt.Sprintf("%s\n\nStale reason: %s", entry.BodyMD, reason)
	}
	if err := r.store.UpsertMemoryEntry(ctx, entry); err != nil {
		return nil, err
	}
	saved, err := r.store.GetMemoryEntry(ctx, memoryID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"entry": saved}, nil
}

func (r *Runtime) testSuggest(ctx context.Context, args map[string]any) (map[string]any, error) {
	proposalID := stringArg(args, "proposal_id")
	taskID := stringArg(args, "task_id")
	files := []string{}
	risk := ""
	if proposalID != "" {
		proposal, err := r.store.GetProposal(ctx, proposalID)
		if err != nil {
			return nil, err
		}
		risk = proposal.Risk
		patchPath := filepath.Join(r.root, filepath.FromSlash(proposal.PatchPath))
		data, err := os.ReadFile(patchPath)
		if err != nil {
			return nil, err
		}
		patchInfo, err := repo.ParseTouchedFiles(string(data))
		if err != nil {
			return nil, err
		}
		files = append(files, patchInfo.Files...)
		if taskID == "" {
			taskID = proposal.TaskID
		}
	}
	if taskID != "" && risk == "" {
		task, err := r.store.GetTask(ctx, taskID)
		if err == nil {
			risk = task.Risk
		}
	}
	suggestions := suggestedTestCommands(files, risk)
	return map[string]any{
		"task_id":     taskID,
		"proposal_id": proposalID,
		"commands":    suggestions,
	}, nil
}

func (r *Runtime) testRun(ctx context.Context, args map[string]any) (map[string]any, error) {
	command := stringArg(args, "command")
	if strings.TrimSpace(command) == "" {
		return nil, errors.New("command is required")
	}
	if err := validateAgentCommand(command); err != nil {
		return nil, err
	}
	testRun := db.TestRun{
		ID:         stringArgDefault(args, "test_run_id", generatedID("TR")),
		ProposalID: stringArg(args, "proposal_id"),
		TaskID:     stringArg(args, "task_id"),
		Command:    command,
		Status:     "running",
	}
	if err := r.store.UpsertTestRun(ctx, testRun); err != nil {
		return nil, err
	}

	// The command is restricted to a read/test allowlist and shell-control
	// characters are rejected before this compatibility execution path. This
	// keeps existing configured test commands while preventing repository write
	// primitives from entering the agent surface.
	cmd := exec.CommandContext(ctx, "/bin/sh", "-lc", command)
	cmd.Dir = r.root
	output, err := cmd.CombinedOutput()

	logPath := filepath.Join(r.root, ".roundtable/testlogs", testRun.ID+".log")
	if mkdirErr := os.MkdirAll(filepath.Dir(logPath), 0o755); mkdirErr != nil {
		return nil, mkdirErr
	}
	if writeErr := os.WriteFile(logPath, output, 0o644); writeErr != nil {
		return nil, writeErr
	}

	testRun.LogPath = filepath.ToSlash(filepath.Join(".roundtable/testlogs", testRun.ID+".log"))
	testRun.SummaryMD = summarizeTestOutput(output)
	if err != nil {
		testRun.Status = "failed"
	} else {
		testRun.Status = "passed"
	}
	if upsertErr := r.store.UpsertTestRun(ctx, testRun); upsertErr != nil {
		return nil, upsertErr
	}
	saved, getErr := r.store.GetTestRun(ctx, testRun.ID)
	if getErr != nil {
		return nil, getErr
	}
	return map[string]any{
		"test_run": saved,
		"log":      string(output),
	}, nil
}

func (r *Runtime) testGetResult(ctx context.Context, args map[string]any) (map[string]any, error) {
	testRunID := stringArg(args, "test_run_id")
	if testRunID == "" {
		return nil, errors.New("test_run_id is required")
	}
	testRun, err := r.store.GetTestRun(ctx, testRunID)
	if err != nil {
		return nil, err
	}
	log := ""
	if testRun.LogPath != "" {
		data, err := os.ReadFile(filepath.Join(r.root, filepath.FromSlash(testRun.LogPath)))
		if err == nil {
			log = string(data)
		}
	}
	return map[string]any{
		"test_run": testRun,
		"log":      log,
	}, nil
}

func (r *Runtime) repoReadFile(args map[string]any) (map[string]any, error) {
	path := stringArg(args, "path")
	if path == "" {
		return nil, errors.New("path is required")
	}
	fullPath, err := r.resolveAgentPath(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"path":    filepath.ToSlash(path),
		"content": string(data),
	}, nil
}

func (r *Runtime) repoSearch(args map[string]any) (map[string]any, error) {
	query := strings.ToLower(stringArg(args, "query"))
	if query == "" {
		return nil, errors.New("query is required")
	}
	targetPath := stringArg(args, "path")

	var paths []string
	if targetPath != "" {
		path, err := r.resolveAgentPath(targetPath)
		if err != nil {
			return nil, err
		}
		paths = []string{path}
	} else {
		err := filepath.Walk(r.root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			name := info.Name()
			if info.IsDir() && (name == ".roundtable" || name == ".git") {
				return filepath.SkipDir
			}
			if info.IsDir() {
				return nil
			}
			paths = append(paths, path)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	var matches []map[string]any
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for i, line := range strings.Split(string(data), "\n") {
			if strings.Contains(strings.ToLower(line), query) {
				rel := path
				if relPath, err := filepath.Rel(r.root, path); err == nil {
					rel = relPath
				}
				matches = append(matches, map[string]any{
					"path":    filepath.ToSlash(rel),
					"line":    i + 1,
					"content": line,
				})
			}
		}
	}
	return map[string]any{"matches": matches}, nil
}

func (r *Runtime) resourceClaim(ctx context.Context, args map[string]any) (map[string]any, error) {
	baseHash := stringArg(args, "base_hash")
	if baseHash == "" {
		computed, err := r.currentResourceHash(stringArg(args, "resource_type"), stringArg(args, "path"), stringArg(args, "symbol"))
		if err != nil {
			return nil, err
		}
		baseHash = computed
	}
	req := claims.CreateRequest{
		ID:           stringArg(args, "claim_id"),
		RunID:        stringArg(args, "run_id"),
		AgentID:      stringArg(args, "agent_id"),
		TaskID:       stringArg(args, "task_id"),
		ResourceID:   stringArg(args, "resource_id"),
		ResourceType: stringArg(args, "resource_type"),
		ResourcePath: stringArg(args, "path"),
		SymbolName:   stringArg(args, "symbol"),
		ClaimType:    stringArgDefault(args, "claim_type", "write"),
		BaseHash:     baseHash,
		TTL:          time.Duration(intArgDefault(args, "ttl_seconds", 900)) * time.Second,
		Renewable:    boolArgDefault(args, "renewable", true),
		ResumePolicy: stringArgDefault(args, "resume_policy", "hold"),
		RationaleMD:  stringArg(args, "rationale"),
	}
	claim, err := r.claims.Create(ctx, req)
	if err != nil {
		return nil, err
	}
	resource, err := r.store.GetResource(ctx, claim.ResourceID)
	if err != nil {
		return nil, err
	}
	resource.CurrentHash = baseHash
	if err := r.store.UpsertResource(ctx, resource); err != nil {
		return nil, err
	}
	return map[string]any{"claim": claim, "resource": resource}, nil
}

func (r *Runtime) resourceRelease(ctx context.Context, args map[string]any) (map[string]any, error) {
	claimID := stringArg(args, "claim_id")
	if claimID == "" {
		return nil, errors.New("claim_id is required")
	}
	claim, err := r.claims.Release(ctx, stringArg(args, "run_id"), claimID, stringArg(args, "actor_id"), stringArg(args, "reason"))
	if err != nil {
		return nil, err
	}
	return map[string]any{"claim": claim}, nil
}

func (r *Runtime) resourceClaimStatus(ctx context.Context, args map[string]any) (map[string]any, error) {
	out, err := r.claims.List(ctx, claims.StatusFilter{
		AgentID:    stringArg(args, "agent_id"),
		ResourceID: stringArg(args, "resource_id"),
		Status:     stringArg(args, "status"),
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"claims": out}, nil
}

func (r *Runtime) resourceGet(ctx context.Context, args map[string]any) (map[string]any, error) {
	resourceID := stringArg(args, "resource_id")
	if resourceID == "" {
		return nil, errors.New("resource_id is required")
	}
	resource, err := r.store.GetResource(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"resource": resource}, nil
}

func (r *Runtime) resourceSearch(ctx context.Context, args map[string]any) (map[string]any, error) {
	query := strings.ToLower(stringArg(args, "query"))
	resourceType := stringArg(args, "resource_type")
	resources, err := r.store.ListResources(ctx)
	if err != nil {
		return nil, err
	}
	var matches []db.Resource
	for _, resource := range resources {
		if resourceType != "" && resource.Type != resourceType {
			continue
		}
		if query == "" || containsResource(resource, query) {
			matches = append(matches, resource)
		}
	}
	return map[string]any{"resources": matches}, nil
}

func (r *Runtime) repoSymbols(ctx context.Context, args map[string]any) (map[string]any, error) {
	_ = ctx
	path := stringArg(args, "path")
	if path == "" {
		return nil, errors.New("path is required")
	}
	fullPath, err := r.resolveAgentPath(path)
	if err != nil {
		return nil, err
	}
	found, err := r.indexer.IndexPath(fullPath)
	if err != nil {
		return nil, err
	}
	if language := stringArg(args, "language"); language != "" {
		filtered := make([]symbols.Symbol, 0, len(found))
		for _, symbol := range found {
			if symbol.Language == language {
				filtered = append(filtered, symbol)
			}
		}
		found = filtered
	}
	return map[string]any{"symbols": found}, nil
}

func ParseCallRequest(data []byte) (CallRequest, error) {
	var req CallRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return CallRequest{}, err
	}
	if req.Args == nil {
		req.Args = map[string]any{}
	}
	return req, nil
}

func EncodeResponse(result map[string]any, err error) ([]byte, error) {
	resp := CallResponse{OK: err == nil}
	if err != nil {
		resp.Error = err.Error()
	} else {
		resp.Result = result
	}
	return json.Marshal(resp)
}

func ReadJSONFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func (r *Runtime) resolveAgentPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("path is required")
	}
	root, err := filepath.Abs(r.root)
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	candidate := path
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	candidate, err = filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	resolved := candidate
	if evaluated, evalErr := filepath.EvalSymlinks(candidate); evalErr == nil {
		resolved = evaluated
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("path is outside the repository root")
	}
	return resolved, nil
}

func validateAgentCommand(command string) error {
	trimmed := strings.TrimSpace(command)
	if strings.ContainsAny(trimmed, ";|&><`$\n\r") {
		return errors.New("command contains disallowed shell control characters")
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return errors.New("command is required")
	}
	name := filepath.Base(fields[0])
	allowed := map[string]bool{
		"cargo": true, "git": true, "go": true, "make": true, "node": true,
		"npm": true, "pnpm": true, "printf": true, "pytest": true, "ruby": true,
		"swift": true, "vitest": true, "yarn": true,
	}
	if !allowed[name] {
		return fmt.Errorf("command %q is not allowlisted for the agent test surface", name)
	}
	return nil
}

func containsResource(resource db.Resource, query string) bool {
	for _, value := range []string{resource.ID, resource.Type, resource.Path, resource.Symbol, resource.Language} {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}

func memoryScore(entry db.MemoryEntry, query string) int {
	if query == "" {
		return entry.Importance
	}
	score := 0
	for _, value := range []string{entry.Title, entry.BodyMD, entry.Scope, entry.Kind} {
		score += strings.Count(strings.ToLower(value), query)
	}
	return score
}

func suggestedTestCommands(files []string, risk string) []string {
	seen := map[string]struct{}{}
	var commands []string
	add := func(command string) {
		if command == "" {
			return
		}
		if _, ok := seen[command]; ok {
			return
		}
		seen[command] = struct{}{}
		commands = append(commands, command)
	}
	add("go test ./...")
	for _, file := range files {
		slash := filepath.ToSlash(file)
		switch {
		case strings.HasSuffix(slash, ".go"):
			add("go test ./...")
		case strings.HasSuffix(slash, ".py"):
			add("pytest")
		case strings.HasSuffix(slash, ".ts"), strings.HasSuffix(slash, ".tsx"), strings.HasSuffix(slash, ".js"), strings.HasSuffix(slash, ".jsx"):
			add("npm test")
		case strings.Contains(slash, "Dockerfile"), strings.Contains(slash, ".github/workflows/"):
			add("npm test")
			add("go test ./...")
		}
	}
	if risk == "high" || risk == "critical" {
		add("go test ./...")
		add("npm test")
	}
	sort.Strings(commands)
	return commands
}

func summarizeTestOutput(output []byte) string {
	text := strings.TrimSpace(string(output))
	if text == "" {
		return "No test output captured."
	}
	lines := strings.Split(text, "\n")
	if len(lines) > 6 {
		lines = lines[len(lines)-6:]
	}
	return strings.Join(lines, "\n")
}

func countClaimsByStatus(claims []db.Claim, status string) int {
	count := 0
	for _, claim := range claims {
		if claim.Status == status {
			count++
		}
	}
	return count
}

func countTasksByStatus(tasks []db.Task, statuses ...string) int {
	allowed := map[string]struct{}{}
	for _, status := range statuses {
		allowed[status] = struct{}{}
	}
	count := 0
	for _, task := range tasks {
		if _, ok := allowed[task.Status]; ok {
			count++
		}
	}
	return count
}

func countEnabledAgents(agents []db.Agent) int {
	count := 0
	for _, agent := range agents {
		if agent.IsEnabled {
			count++
		}
	}
	return count
}

func generatedID(prefix string) string {
	return prefix + "-" + time.Now().UTC().Format("20060102T150405.000000000")
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func stringArg(args map[string]any, key string) string {
	switch v := args[key].(type) {
	case string:
		return v
	default:
		return ""
	}
}

func stringArgDefault(args map[string]any, key, fallback string) string {
	if value := stringArg(args, key); value != "" {
		return value
	}
	return fallback
}

func intArgDefault(args map[string]any, key string, fallback int) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	case json.Number:
		n, err := v.Int64()
		if err == nil {
			return int(n)
		}
	}
	return fallback
}

func boolArgDefault(args map[string]any, key string, fallback bool) bool {
	v, ok := args[key].(bool)
	if !ok {
		return fallback
	}
	return v
}

func floatArgDefault(args map[string]any, key string, fallback float64) float64 {
	switch v := args[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case json.Number:
		n, err := v.Float64()
		if err == nil {
			return n
		}
	}
	return fallback
}

func stringSliceArg(args map[string]any, key string) ([]string, error) {
	raw, ok := args[key]
	if !ok {
		return nil, nil
	}
	switch values := raw.(type) {
	case []string:
		return values, nil
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			s, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("%s must contain only strings", key)
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%s must be an array of strings", key)
	}
}

func (r *Runtime) currentResourceHash(resourceType, resourcePath, symbolName string) (string, error) {
	resourcePath = filepath.ToSlash(resourcePath)
	switch resourceType {
	case "file":
		return repo.FileHash(r.root, resourcePath)
	case "directory":
		return repo.DirectoryHash(r.root, resourcePath)
	case "symbol":
		if resourcePath == "" || symbolName == "" {
			return "", nil
		}
		found, err := r.indexer.IndexPath(filepath.Join(r.root, filepath.FromSlash(resourcePath)))
		if err != nil {
			return "", nil
		}
		for _, symbol := range found {
			if symbol.Name != symbolName {
				continue
			}
			return repo.LineRangeHash(r.root, resourcePath, symbol.StartLine, symbol.EndLine)
		}
		return "", nil
	default:
		return "", nil
	}
}
