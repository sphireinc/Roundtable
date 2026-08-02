package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"roundtable/internal/db"
)

const DefaultPolicyFile = "POLICIES.ROUNDTABLE.md"

const markdownFence = "```"
const yamlFence = "```yaml"

const defaultPolicyMarkdown = `# Roundtable Policies

This file defines default governance and risk policy. It should compile into runtime policy rules.

## Consensus defaults

` + yamlFence + `
consensus:
  default:
    approvals_required: 2
    blockers_allowed: 0
    tests_required: true

  architecture:
    required_roles:
      - Architect
      - Reviewer
    approvals_required: 2
    blockers_allowed: 0

  security:
    required_roles:
      - Security
    security_veto_blocks: true
    human_approval_required: true

  dependency_change:
    required_roles:
      - Security
      - Tester
    human_approval_required: true

  destructive_command:
    human_approval_required: true
    security_approval_required: true

  migration:
    required_roles:
      - Architect
      - Security
      - Tester
    human_approval_required: true
    rollback_plan_required: true
` + markdownFence + `

## High-risk paths

` + yamlFence + `
risk:
  high_paths:
    - ".env*"
    - "**/.env*"
    - "auth/**"
    - "security/**"
    - "infra/**"
    - "migrations/**"
    - "package.json"
    - "package-lock.json"
    - "pnpm-lock.yaml"
    - "yarn.lock"
    - "go.mod"
    - "go.sum"
    - "Dockerfile"
    - "docker-compose*.yml"
    - ".github/workflows/**"
` + markdownFence + `

## Dangerous commands

Commands requiring explicit claims and likely human approval:

` + yamlFence + `
commands:
  dangerous:
    - "rm"
    - "chmod"
    - "chown"
    - "mv"
    - "git reset"
    - "git clean"
    - "git push"
    - "docker system prune"
    - "kubectl"
    - "terraform apply"
    - "npm install"
    - "pnpm add"
    - "yarn add"
    - "go get"
    - "pip install"
` + markdownFence + `

## Security veto

Security veto blocks a proposal unless Human explicitly overrides.

Security must veto if a proposal:

- exposes secrets
- logs tokens, credentials, private keys, session ids, or PII
- weakens authentication or authorization
- introduces command injection, path traversal, SQL injection, unsafe deserialization, SSRF, or XSS risk
- introduces unaudited dependency risk
- changes production infrastructure without appropriate policy
- runs destructive or irreversible commands without approval

## Human approval

Human approval is required for:

- high-risk resources
- production infrastructure
- auth/security/payment changes
- database migrations
- dependency changes
- destructive commands
- policy changes
- overriding a Security veto

## Claim conflict policy

A claim conflicts if it overlaps another active claim in a way that may cause semantic or textual conflict.

Conflict examples:

- directory claim conflicts with files below it
- file claim conflicts with symbols in that file
- symbol claims conflict if they overlap ranges
- schema claim conflicts with migrations touching that schema
- command claim conflicts with another mutating command

## Resume policy

When resuming:

- Roundtable state is authoritative.
- External CLI session memory is non-authoritative.
- Claims with stale base hashes become suspended.
- High-risk suspended claims require Human review.
- Chair decides whether to renew, revoke, or transfer stale claims.
`

type Result struct {
	Approvals     int      `json:"approvals"`
	Blockers      int      `json:"blockers"`
	Vetoes        int      `json:"vetoes"`
	Approvable    bool     `json:"approvable"`
	NeedsHuman    bool     `json:"needs_human"`
	Summary       string   `json:"summary"`
	ApprovalVotes []string `json:"approval_votes"`
	BlockingVotes []string `json:"blocking_votes"`
	HighRiskPaths []string `json:"high_risk_paths"`
	Profiles      []string `json:"profiles"`
}

type ConsensusRule struct {
	ApprovalsRequired      int
	BlockersAllowed        int
	RequiredRoles          []string
	TestsRequired          bool
	SecurityVetoBlocks     bool
	HumanApprovalRequired  bool
	SecurityApprovalNeeded bool
	RollbackPlanRequired   bool
}

type Rules struct {
	Consensus         map[string]ConsensusRule
	HighRiskPatterns  []string
	DangerousCommands []string
}

type Engine struct {
	rules Rules
}

func (e *Engine) Rules() Rules {
	return e.rules
}

func Load(root string) (*Engine, error) {
	defaults, err := parseMarkdown(defaultPolicyMarkdown)
	if err != nil {
		return nil, fmt.Errorf("parse built-in policy: %w", err)
	}
	path := filepath.Join(root, DefaultPolicyFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Engine{rules: defaults}, nil
		}
		return nil, fmt.Errorf("read policy file: %w", err)
	}
	overrides, err := parseMarkdown(string(data))
	if err != nil {
		return nil, fmt.Errorf("parse policy file %s: %w", path, err)
	}
	return &Engine{rules: mergeRules(defaults, overrides)}, nil
}

func DefaultEngine() *Engine {
	rules, err := parseMarkdown(defaultPolicyMarkdown)
	if err != nil {
		panic(err)
	}
	return &Engine{rules: rules}
}

func EvaluateProposal(risk string, touchedFiles []string, votes []db.Vote) Result {
	return DefaultEngine().EvaluateProposal(risk, touchedFiles, votes)
}

func (e *Engine) EvaluateProposal(risk string, touchedFiles []string, votes []db.Vote) Result {
	result := Result{}
	for _, vote := range votes {
		switch vote.Vote {
		case "approve", "approve_with_notes":
			result.Approvals++
			result.ApprovalVotes = append(result.ApprovalVotes, vote.AgentID+":"+vote.Vote)
		case "revise", "reject":
			result.Blockers++
			result.BlockingVotes = append(result.BlockingVotes, vote.AgentID+":"+vote.Vote)
		case "veto":
			result.Vetoes++
			result.Blockers++
			result.BlockingVotes = append(result.BlockingVotes, vote.AgentID+":"+vote.Vote)
		}
	}
	profiles := e.profilesForPaths(touchedFiles)
	result.Profiles = append(result.Profiles, profiles...)
	for _, path := range touchedFiles {
		if e.isHighRiskPath(path) {
			result.HighRiskPaths = append(result.HighRiskPaths, filepath.ToSlash(path))
		}
	}

	defaultRule := e.rules.Consensus["default"]
	securityRule := e.rules.Consensus["security"]
	approvalsRequired := maxInt(defaultRule.ApprovalsRequired, 1)
	blockersAllowed := defaultRule.BlockersAllowed
	needsHuman := risk == "high" || risk == "critical"
	if len(result.HighRiskPaths) > 0 && securityRule.HumanApprovalRequired {
		needsHuman = true
	}
	if result.Vetoes > 0 && securityRule.SecurityVetoBlocks {
		needsHuman = true
	}
	if rule, ok := e.rules.Consensus["dependency_change"]; ok && containsString(profiles, "dependency_change") && rule.HumanApprovalRequired {
		needsHuman = true
	}
	if rule, ok := e.rules.Consensus["migration"]; ok && containsString(profiles, "migration") && rule.HumanApprovalRequired {
		needsHuman = true
	}

	result.NeedsHuman = needsHuman
	result.Approvable = result.Approvals >= approvalsRequired && result.Blockers <= blockersAllowed && !result.NeedsHuman

	switch {
	case result.Vetoes > 0 && securityRule.SecurityVetoBlocks:
		result.Summary = "security veto blocks proposal"
	case len(result.HighRiskPaths) > 0 && securityRule.HumanApprovalRequired:
		result.Summary = "human approval required for high-risk paths"
	case (risk == "high" || risk == "critical") && result.NeedsHuman:
		result.Summary = "human approval required for high-risk proposal"
	case result.Approvals < approvalsRequired:
		result.Summary = fmt.Sprintf("insufficient approvals: need %d", approvalsRequired)
	case result.Blockers > blockersAllowed:
		result.Summary = "blocking votes present"
	default:
		result.Summary = "proposal satisfies policy consensus"
	}
	return result
}

func (e *Engine) profilesForPaths(touchedFiles []string) []string {
	seen := map[string]struct{}{}
	for _, path := range touchedFiles {
		slashPath := filepath.ToSlash(path)
		if strings.HasPrefix(slashPath, "migrations/") {
			seen["migration"] = struct{}{}
		}
		switch filepath.Base(slashPath) {
		case "go.mod", "go.sum", "package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock":
			seen["dependency_change"] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for profile := range seen {
		out = append(out, profile)
	}
	sort.Strings(out)
	return out
}

func (e *Engine) isHighRiskPath(path string) bool {
	for _, pattern := range e.rules.HighRiskPatterns {
		if matchPattern(pattern, path) {
			return true
		}
	}
	return false
}

func mergeRules(base, override Rules) Rules {
	merged := base
	if len(override.Consensus) > 0 {
		if merged.Consensus == nil {
			merged.Consensus = map[string]ConsensusRule{}
		}
		for key, rule := range override.Consensus {
			merged.Consensus[key] = rule
		}
	}
	if len(override.HighRiskPatterns) > 0 {
		merged.HighRiskPatterns = append([]string(nil), override.HighRiskPatterns...)
	}
	if len(override.DangerousCommands) > 0 {
		merged.DangerousCommands = append([]string(nil), override.DangerousCommands...)
	}
	return merged
}

func parseMarkdown(markdown string) (Rules, error) {
	root := map[string]any{}
	lines := strings.Split(markdown, "\n")
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "```yaml" {
			continue
		}
		start := i + 1
		end := start
		for end < len(lines) && strings.TrimSpace(lines[end]) != "```" {
			end++
		}
		if end >= len(lines) {
			return Rules{}, fmt.Errorf("unterminated yaml fence")
		}
		block, err := parseYAMLBlock(lines[start:end])
		if err != nil {
			return Rules{}, err
		}
		mergeMap(root, block)
		i = end
	}
	return decodeRules(root)
}

func parseYAMLBlock(lines []string) (map[string]any, error) {
	index := 0
	return parseMap(lines, &index, 0)
}

func parseMap(lines []string, index *int, indent int) (map[string]any, error) {
	result := map[string]any{}
	for *index < len(lines) {
		raw := strings.TrimRight(lines[*index], " \t")
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			*index += 1
			continue
		}
		currentIndent := indentation(raw)
		if currentIndent < indent {
			break
		}
		if currentIndent > indent {
			return nil, fmt.Errorf("unexpected indentation near %q", raw)
		}
		if strings.HasPrefix(trimmed, "- ") {
			return nil, fmt.Errorf("unexpected list item near %q", raw)
		}
		key, value, hasValue, err := splitKV(trimmed)
		if err != nil {
			return nil, err
		}
		*index += 1
		if hasValue {
			result[key] = parseScalar(value)
			continue
		}
		nextIndent, isList, ok := nextIndentedLine(lines, *index, indent)
		if !ok {
			result[key] = map[string]any{}
			continue
		}
		if isList {
			list, err := parseList(lines, index, nextIndent)
			if err != nil {
				return nil, err
			}
			result[key] = list
			continue
		}
		child, err := parseMap(lines, index, nextIndent)
		if err != nil {
			return nil, err
		}
		result[key] = child
	}
	return result, nil
}

func parseList(lines []string, index *int, indent int) ([]any, error) {
	var result []any
	for *index < len(lines) {
		raw := strings.TrimRight(lines[*index], " \t")
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			*index += 1
			continue
		}
		currentIndent := indentation(raw)
		if currentIndent < indent {
			break
		}
		if currentIndent > indent {
			return nil, fmt.Errorf("unexpected indentation near %q", raw)
		}
		if !strings.HasPrefix(trimmed, "- ") {
			break
		}
		result = append(result, parseScalar(strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))))
		*index += 1
	}
	return result, nil
}

func nextIndentedLine(lines []string, index int, parentIndent int) (indent int, isList bool, ok bool) {
	for index < len(lines) {
		raw := strings.TrimRight(lines[index], " \t")
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			index++
			continue
		}
		currentIndent := indentation(raw)
		if currentIndent <= parentIndent {
			return 0, false, false
		}
		return currentIndent, strings.HasPrefix(trimmed, "- "), true
	}
	return 0, false, false
}

func decodeRules(root map[string]any) (Rules, error) {
	rules := Rules{
		Consensus: map[string]ConsensusRule{},
	}
	if consensusSection, ok := root["consensus"].(map[string]any); ok {
		for name, rawRule := range consensusSection {
			ruleMap, ok := rawRule.(map[string]any)
			if !ok {
				return Rules{}, fmt.Errorf("consensus.%s must be a map", name)
			}
			rules.Consensus[name] = decodeConsensusRule(ruleMap)
		}
	}
	if riskSection, ok := root["risk"].(map[string]any); ok {
		rules.HighRiskPatterns = stringList(riskSection["high_paths"])
	}
	if commandSection, ok := root["commands"].(map[string]any); ok {
		rules.DangerousCommands = stringList(commandSection["dangerous"])
	}
	return rules, nil
}

func decodeConsensusRule(raw map[string]any) ConsensusRule {
	return ConsensusRule{
		ApprovalsRequired:      intValue(raw["approvals_required"]),
		BlockersAllowed:        intValue(raw["blockers_allowed"]),
		RequiredRoles:          stringList(raw["required_roles"]),
		TestsRequired:          boolValue(raw["tests_required"]),
		SecurityVetoBlocks:     boolValue(raw["security_veto_blocks"]),
		HumanApprovalRequired:  boolValue(raw["human_approval_required"]),
		SecurityApprovalNeeded: boolValue(raw["security_approval_required"]),
		RollbackPlanRequired:   boolValue(raw["rollback_plan_required"]),
	}
}

func mergeMap(dst, src map[string]any) {
	for key, value := range src {
		if srcMap, ok := value.(map[string]any); ok {
			if dstMap, ok := dst[key].(map[string]any); ok {
				mergeMap(dstMap, srcMap)
				continue
			}
		}
		dst[key] = value
	}
}

func matchPattern(pattern, path string) bool {
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	path = filepath.ToSlash(path)
	switch {
	case pattern == "":
		return false
	case strings.HasPrefix(pattern, "**/") && strings.HasPrefix(filepath.Base(path), strings.TrimSuffix(strings.TrimPrefix(pattern, "**/"), "*")):
		return true
	case strings.HasSuffix(pattern, "/**"):
		prefix := strings.TrimSuffix(pattern, "/**")
		return path == prefix || strings.HasPrefix(path, prefix+"/")
	default:
		matched, err := filepath.Match(pattern, path)
		if err == nil && matched {
			return true
		}
		if !strings.Contains(pattern, "/") {
			matched, err = filepath.Match(pattern, filepath.Base(path))
			return err == nil && matched
		}
		return false
	}
}

func parseScalar(value string) any {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, `"`)
	if v, err := strconv.Atoi(value); err == nil {
		return v
	}
	if v, err := strconv.ParseBool(value); err == nil {
		return v
	}
	return value
}

func indentation(line string) int {
	count := 0
	for _, ch := range line {
		if ch != ' ' {
			break
		}
		count++
	}
	return count
}

func splitKV(line string) (key, value string, hasValue bool, err error) {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return "", "", false, fmt.Errorf("invalid yaml line %q", line)
	}
	key = strings.TrimSpace(parts[0])
	if key == "" {
		return "", "", false, fmt.Errorf("missing yaml key in %q", line)
	}
	value = strings.TrimSpace(parts[1])
	return key, value, value != "", nil
}

func intValue(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case string:
		n, err := strconv.Atoi(v)
		if err == nil {
			return n
		}
	}
	return 0
}

func boolValue(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		b, err := strconv.ParseBool(v)
		return err == nil && b
	}
	return false
}

func stringList(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok || text == "" {
			continue
		}
		out = append(out, text)
	}
	return out
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
