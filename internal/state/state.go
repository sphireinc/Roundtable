package state

import (
	"context"

	"roundtable/internal/db"
)

type Snapshot struct {
	Runs            []db.Run
	Agents          []db.Agent
	Tasks           []db.Task
	Claims          []db.Claim
	Proposals       []db.Proposal
	Transactions    []db.Transaction
	HumanApprovals  []db.HumanApproval
	SecurityReviews []db.SecurityReview
	Memories        []db.MemoryEntry
}

func LoadSnapshot(ctx context.Context, store *db.Store) (Snapshot, error) {
	runs, err := store.ListRuns(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	agents, err := store.ListAgents(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	tasks, err := store.ListTasks(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	claims, err := store.ListClaims(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	proposals, err := store.ListProposals(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	transactions, err := store.ListTransactions(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	approvals, err := store.ListHumanApprovals(ctx, "")
	if err != nil {
		return Snapshot{}, err
	}
	securityReviews, err := store.ListSecurityReviews(ctx, "")
	if err != nil {
		return Snapshot{}, err
	}
	memories, err := store.ListMemoryEntries(ctx)
	if err != nil {
		return Snapshot{}, err
	}

	return Snapshot{
		Runs:            runs,
		Agents:          agents,
		Tasks:           tasks,
		Claims:          claims,
		Proposals:       proposals,
		Transactions:    transactions,
		HumanApprovals:  approvals,
		SecurityReviews: securityReviews,
		Memories:        memories,
	}, nil
}
