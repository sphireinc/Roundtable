package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"roundtable/internal/db"
)

const taskHistoryLimit = 100
const proposalPatchReadLimit = 1 << 20

var oracleProposalID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

func proposalBoundTool(tool string) bool {
	switch tool {
	case "proposal.get", "proposal.patch_read", "memory.oracle_report", "vote.cast", "vote.list", "security.review", "test.evidence.submit", "proposal.request_review":
		return true
	default:
		return false
	}
}

func (r *Runtime) memoryTaskHistory(ctx context.Context, args map[string]any) (map[string]any, error) {
	taskID, runID := stringArg(args, "task_id"), stringArg(args, "run_id")
	if taskID == "" || runID == "" {
		return nil, errors.New("task_id and run_id are required")
	}
	task, err := r.store.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	proposals, proposalsTruncated, err := r.store.ListTaskProposals(ctx, taskID, taskHistoryLimit)
	if err != nil {
		return nil, err
	}
	decisions, decisionsTruncated, err := r.store.ListTaskDecisions(ctx, taskID, taskHistoryLimit)
	if err != nil {
		return nil, err
	}
	events, eventsTruncated, err := r.store.ListTaskEvents(ctx, runID, taskID, taskHistoryLimit)
	if err != nil {
		return nil, err
	}
	memories, memoriesTruncated, err := r.store.ListActiveTaskMemory(ctx, taskID, taskHistoryLimit)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"task": task, "proposals": proposals, "decisions": decisions,
		"proposals_truncated": proposalsTruncated, "decisions_truncated": decisionsTruncated,
		"events": events, "events_truncated": eventsTruncated,
		"memory_entries": memories, "memory_entries_truncated": memoriesTruncated,
	}, nil
}

func (r *Runtime) memoryOracleReport(ctx context.Context, args map[string]any) (map[string]any, error) {
	proposalID, taskID, runID := stringArg(args, "proposal_id"), stringArg(args, "task_id"), stringArg(args, "run_id")
	warning := strings.TrimSpace(stringArg(args, "warning_md"))
	if proposalID == "" || taskID == "" || runID == "" || warning == "" {
		return nil, errors.New("proposal_id, task_id, run_id, and warning_md are required")
	}
	if len(warning) > 4000 {
		return nil, errors.New("warning_md must not exceed 4000 bytes")
	}
	proposal, err := r.store.GetProposal(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	if proposal.TaskID != taskID || proposal.Status != "pending" && proposal.Status != "in_review" {
		return nil, errors.New("Memory Oracle warnings require the assigned task's active proposal")
	}
	memoryIDs, err := stringSliceArg(args, "memory_ids")
	if err != nil {
		return nil, err
	}
	if len(memoryIDs) == 0 || len(memoryIDs) > 20 {
		return nil, errors.New("memory_ids must contain between 1 and 20 active source entries")
	}
	seen := make(map[string]struct{}, len(memoryIDs))
	for _, memoryID := range memoryIDs {
		if memoryID == "" {
			return nil, errors.New("memory_ids cannot contain empty IDs")
		}
		if _, duplicate := seen[memoryID]; duplicate {
			return nil, fmt.Errorf("duplicate memory source %q", memoryID)
		}
		seen[memoryID] = struct{}{}
		memory, err := r.store.GetMemoryEntry(ctx, memoryID)
		if err != nil || memory.Status != "active" || memory.Scope != taskID && memory.Scope != "project" {
			return nil, fmt.Errorf("memory source %q is unavailable to this task", memoryID)
		}
	}
	payload, err := json.Marshal(map[string]any{
		"proposal_id": proposalID, "memory_ids": memoryIDs, "warning_md": warning,
		"advisory_only": true,
	})
	if err != nil {
		return nil, fmt.Errorf("encode Memory Oracle warning: %w", err)
	}
	event, err := r.store.AppendEvent(ctx, db.Event{
		RunID: runID, Type: "memory.oracle_warning", ActorID: stringArg(args, "agent_id"),
		TaskID: taskID, PayloadJSON: string(payload),
	})
	if err != nil {
		return nil, fmt.Errorf("persist Memory Oracle warning: %w", err)
	}
	return map[string]any{"warning_event": event, "advisory_only": true}, nil
}

func (r *Runtime) proposalPatchRead(ctx context.Context, args map[string]any) (map[string]any, error) {
	proposalID := stringArg(args, "proposal_id")
	if !oracleProposalID.MatchString(proposalID) {
		return nil, errors.New("proposal_id is invalid")
	}
	proposal, err := r.store.GetProposal(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	expected := filepath.ToSlash(filepath.Join(".roundtable", "patches", proposal.ID+".diff"))
	if proposal.PatchPath != expected {
		return nil, errors.New("proposal patch path is not a canonical stored artifact path")
	}
	root, err := os.OpenRoot(r.root)
	if err != nil {
		return nil, fmt.Errorf("open repository root for proposal patch: %w", err)
	}
	defer root.Close()
	for _, component := range []string{".roundtable", filepath.Join(".roundtable", "patches"), expected} {
		info, err := root.Lstat(component)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("proposal patch path contains a missing or symbolic-link component")
		}
		if component != expected && !info.IsDir() {
			return nil, errors.New("proposal patch path contains a non-directory component")
		}
	}
	file, err := root.Open(filepath.FromSlash(expected))
	if err != nil {
		return nil, fmt.Errorf("open proposal patch: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > proposalPatchReadLimit {
		return nil, errors.New("proposal patch is not a regular file within the 1 MiB read limit")
	}
	data, err := io.ReadAll(io.LimitReader(file, proposalPatchReadLimit+1))
	if err != nil || len(data) > proposalPatchReadLimit {
		return nil, errors.New("proposal patch could not be read within the 1 MiB limit")
	}
	return map[string]any{"proposal_id": proposalID, "patch": string(data)}, nil
}
