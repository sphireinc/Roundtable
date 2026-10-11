package agentauth

import (
	"testing"
	"time"
)

func TestCapabilitiesAreOpaqueSessionBoundAndRoleScoped(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	authority := New(func() time.Time { return now })
	identity := Identity{SessionID: "S-1", AgentID: "A-1", TaskID: "T-1", RunID: "RUN-1", Role: "implementer", BaseRevision: "rev-1", BaseStateHash: "tree-1"}
	token, err := authority.Issue(identity, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || len(token) < 40 {
		t.Fatalf("token is not a high-entropy opaque bearer: %q", token)
	}
	if err := authority.Authorize(token, "proposal.create"); err != nil {
		t.Fatalf("authorized implementer operation rejected: %v", err)
	}
	for _, tool := range []string{"patch.apply", "test.run", "test.get_result", "human.request_approval", "decision.record", "task.create", "resource.search", "resource.get", "table.get_state", "memory.query"} {
		if err := authority.Authorize(token, tool); err == nil {
			t.Errorf("implementer authorized for privileged tool %q", tool)
		}
	}
	if _, err := authority.Resolve(token); err != nil {
		t.Fatalf("resolve issued capability: %v", err)
	}
	if err := authority.Revoke(identity.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := authority.Resolve(token); err == nil {
		t.Fatal("revoked capability remained valid")
	}
}

func TestCapabilitiesRejectUnknownExpiredAndInvalidIdentities(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	authority := New(func() time.Time { return now })
	if _, err := authority.Issue(Identity{SessionID: "S-1", AgentID: "A-1", TaskID: "T-1", RunID: "RUN-1", Role: "Human Operator", BaseRevision: "rev", BaseStateHash: "tree"}, time.Minute); err == nil {
		t.Fatal("accepted a human/operator identity for an external agent")
	}
	token, err := authority.Issue(Identity{SessionID: "S-2", AgentID: "A-2", TaskID: "T-2", RunID: "RUN-2", Role: "implementer", BaseRevision: "rev", BaseStateHash: "tree"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	if _, err := authority.Resolve(token); err == nil {
		t.Fatal("expired capability remained valid")
	}
	if _, err := authority.Resolve("caller-supplied-role=admin"); err == nil {
		t.Fatal("accepted an unknown caller-controlled identity")
	}
}

func TestReviewerAndTesterCapabilitiesAreSeparated(t *testing.T) {
	authority := New(time.Now)
	reviewer, err := authority.Issue(Identity{SessionID: "S-review", AgentID: "A-review", TaskID: "T-1", RunID: "RUN-1", Role: "reviewer", BaseRevision: "rev", BaseStateHash: "tree"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.Authorize(reviewer, "security.review"); err == nil {
		t.Fatal("ordinary reviewer was granted security approval authority")
	}
	if err := authority.Authorize(reviewer, "resource.claim"); err != nil {
		t.Fatalf("reviewer cannot request a constrained review claim: %v", err)
	}
	securityAgent, err := authority.Issue(Identity{SessionID: "S-security", AgentID: "A-security", TaskID: "T-1", RunID: "RUN-1", Role: "security", BaseRevision: "rev", BaseStateHash: "tree"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.Authorize(securityAgent, "security.review"); err != nil {
		t.Fatalf("security agent cannot submit security review: %v", err)
	}
	if err := authority.Authorize(reviewer, "test.run"); err == nil {
		t.Fatal("reviewer unexpectedly received test execution capability")
	}
	tester, err := authority.Issue(Identity{SessionID: "S-test", AgentID: "A-test", TaskID: "T-1", RunID: "RUN-1", Role: "tester", BaseRevision: "rev", BaseStateHash: "tree"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.Authorize(tester, "test.run"); err == nil {
		t.Fatal("tester unexpectedly received host-side test execution capability")
	}
	if err := authority.Authorize(tester, "vote.cast"); err == nil {
		t.Fatal("tester unexpectedly received governance vote capability")
	}
	if err := authority.Authorize(tester, "table.watch"); err == nil {
		t.Fatal("tester unexpectedly received the implementer turn queue capability")
	}
	if err := authority.Authorize(tester, "resource.claim"); err != nil {
		t.Fatalf("tester cannot request a constrained read claim: %v", err)
	}
	if err := authority.Authorize(tester, "test.evidence.submit"); err != nil {
		t.Fatalf("tester cannot submit proposal-bound test evidence: %v", err)
	}
	if err := authority.Authorize(reviewer, "test.evidence.submit"); err == nil {
		t.Fatal("reviewer unexpectedly received tester evidence capability")
	}
}
