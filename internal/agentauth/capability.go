package agentauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var ErrUnauthorized = errors.New("agent capability is missing, expired, revoked, or not authorized for this operation")

type Identity struct {
	SessionID     string
	AgentID       string
	TaskID        string
	ProposalID    string
	RunID         string
	Role          string
	BaseRevision  string
	BaseStateHash string
	SnapshotRoot  string
}

type capability struct {
	identity Identity
	expires  time.Time
}

type Authority struct {
	mu      sync.RWMutex
	entries map[[32]byte]capability
	now     func() time.Time
}

func New(now func() time.Time) *Authority {
	if now == nil {
		now = time.Now
	}
	return &Authority{entries: make(map[[32]byte]capability), now: now}
}

func (a *Authority) Issue(identity Identity, ttl time.Duration) (string, error) {
	identity.Role = normalizeRole(identity.Role)
	if identity.SessionID == "" || identity.AgentID == "" || identity.TaskID == "" || identity.RunID == "" || identity.Role == "" || identity.BaseRevision == "" || identity.BaseStateHash == "" {
		return "", errors.New("agent capability requires a session, agent, task, run, role, and pinned base revision")
	}
	if ttl <= 0 || ttl > 30*time.Minute {
		return "", errors.New("agent capability lifetime must be between 1ns and 30m")
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return "", fmt.Errorf("generate agent capability: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(secret[:])
	digest := sha256.Sum256([]byte(token))
	a.mu.Lock()
	a.entries[digest] = capability{identity: identity, expires: a.now().Add(ttl)}
	a.mu.Unlock()
	return token, nil
}

func (a *Authority) Resolve(token string) (Identity, error) {
	if token == "" {
		return Identity{}, ErrUnauthorized
	}
	digest := sha256.Sum256([]byte(token))
	a.mu.RLock()
	entry, ok := a.entries[digest]
	a.mu.RUnlock()
	if !ok || !a.now().Before(entry.expires) {
		if ok {
			a.mu.Lock()
			delete(a.entries, digest)
			a.mu.Unlock()
		}
		return Identity{}, ErrUnauthorized
	}
	return entry.identity, nil
}

func (a *Authority) Authorize(token, tool string) error {
	identity, err := a.Resolve(token)
	if err != nil {
		return err
	}
	if !allowed(identity.Role, tool) {
		return ErrUnauthorized
	}
	return nil
}

func AllowedTool(role, tool string) bool {
	return allowed(normalizeRole(role), tool)
}

func (a *Authority) AuthorizeIdentity(token, tool string) (Identity, error) {
	identity, err := a.Resolve(token)
	if err != nil {
		return Identity{}, err
	}
	if !allowed(identity.Role, tool) {
		return Identity{}, ErrUnauthorized
	}
	return identity, nil
}

func (a *Authority) Revoke(sessionID string) error {
	if sessionID == "" {
		return errors.New("session_id is required")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for digest, entry := range a.entries {
		if entry.identity.SessionID == sessionID {
			delete(a.entries, digest)
		}
	}
	return nil
}

func normalizeRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "implementer":
		return "implementer"
	case "reviewer":
		return "reviewer"
	case "tester":
		return "tester"
	case "security", "security_agent":
		return "security"
	default:
		return ""
	}
}

func allowed(role, tool string) bool {
	commonRead := map[string]bool{
		"task.get": true, "proposal.get": true, "proposal.list": true,
		"repo.read_file": true, "repo.search": true, "repo.symbols": true,
		"resource.claim_status": true,
	}
	if commonRead[tool] {
		return true
	}
	switch role {
	case "implementer":
		return oneOf(tool, "agent.turn_request", "agent.turn_start", "agent.turn_complete", "table.watch", "resource.claim", "resource.release", "proposal.create", "proposal.attach_patch", "proposal.request_review", "test.suggest")
	case "reviewer":
		return oneOf(tool, "resource.claim", "resource.release", "vote.cast", "vote.list", "proposal.request_review")
	case "tester":
		return oneOf(tool, "resource.claim", "resource.release", "test.suggest", "test.evidence.submit")
	case "security":
		return oneOf(tool, "resource.claim", "resource.release", "security.review", "test.suggest")
	default:
		return false
	}
}

func oneOf(value string, choices ...string) bool {
	for _, choice := range choices {
		if value == choice {
			return true
		}
	}
	return false
}
