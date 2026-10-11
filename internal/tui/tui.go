package tui

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"roundtable/internal/db"
	"roundtable/internal/state"
)

type Options struct {
	Goal            string
	SocketPath      string
	RunID           string
	Snapshot        state.Snapshot
	WatchFeed       []string
	RefreshInterval time.Duration
	Refresh         func(context.Context) (state.Snapshot, []string, error)
	ExecuteCommand  func(context.Context, string) (string, error)
	ctx             context.Context
}

func Run(ctx context.Context, output io.Writer, opts Options) error {
	opts.ctx = ctx
	program := tea.NewProgram(newModel(opts), tea.WithOutput(output), tea.WithContext(ctx))
	_, err := program.Run()
	return err
}

type model struct {
	width         int
	height        int
	opts          Options
	commandInput  string
	commandOutput []string
	commandBusy   bool
}

type refreshMsg struct {
	snapshot  state.Snapshot
	watchFeed []string
	err       error
}

type commandResultMsg struct {
	output string
	err    error
}

func newModel(opts Options) model {
	return model{opts: opts}
}

func (m model) Init() tea.Cmd {
	if m.opts.Refresh == nil {
		return nil
	}
	return scheduleRefresh(m.opts.RefreshInterval, m.opts.Refresh)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		}
		switch msg.Type {
		case tea.KeyEnter:
			if m.commandBusy {
				return m, nil
			}
			command := strings.TrimSpace(m.commandInput)
			m.commandInput = ""
			if command == "" {
				return m, nil
			}
			m.commandOutput = appendCommandLines(m.commandOutput, "$ "+command)
			if m.opts.ExecuteCommand == nil {
				m.commandOutput = appendCommandLines(m.commandOutput, "! command execution is unavailable")
				return m, nil
			}
			m.commandBusy = true
			return m, executeCommand(m.opts.ctx, m.opts.ExecuteCommand, command)
		case tea.KeyBackspace, tea.KeyDelete:
			runes := []rune(m.commandInput)
			if len(runes) > 0 {
				m.commandInput = string(runes[:len(runes)-1])
			}
		case tea.KeyCtrlU:
			m.commandInput = ""
		case tea.KeyRunes:
			if m.commandInput == "" && msg.String() == "q" {
				return m, tea.Quit
			}
			if len([]rune(m.commandInput))+len(msg.Runes) <= 2048 {
				m.commandInput += string(msg.Runes)
			}
		}
	case refreshMsg:
		if msg.err == nil {
			m.opts.Snapshot = msg.snapshot
			m.opts.WatchFeed = msg.watchFeed
		}
		if m.opts.Refresh != nil {
			return m, scheduleRefresh(m.opts.RefreshInterval, m.opts.Refresh)
		}
	case commandResultMsg:
		m.commandBusy = false
		output := msg.output
		if len(output) > 16*1024 {
			output = "... output truncated ...\n" + output[len(output)-16*1024:]
		}
		output = strings.ToValidUTF8(output, "?")
		if output = strings.TrimSpace(output); output != "" {
			m.commandOutput = appendCommandLines(m.commandOutput, strings.Split(output, "\n")...)
		}
		if msg.err != nil {
			errText := msg.err.Error()
			if len(errText) > 2048 {
				errText = errText[:2048] + "..."
			}
			m.commandOutput = appendCommandLines(m.commandOutput, "! "+strings.ToValidUTF8(errText, "?"))
		}
	}
	return m, nil
}

func executeCommand(ctx context.Context, execute func(context.Context, string) (string, error), command string) tea.Cmd {
	if ctx == nil {
		ctx = context.Background()
	}
	return func() tea.Msg {
		output, err := execute(ctx, command)
		return commandResultMsg{output: output, err: err}
	}
}

func appendCommandLines(lines []string, additions ...string) []string {
	for _, addition := range additions {
		for _, line := range strings.Split(addition, "\n") {
			line = strings.TrimRight(line, "\r")
			if len([]rune(line)) > 512 {
				line = string([]rune(line)[:512]) + "..."
			}
			lines = append(lines, line)
		}
	}
	if len(lines) > 80 {
		lines = append([]string(nil), lines[len(lines)-80:]...)
	}
	return lines
}

func scheduleRefresh(interval time.Duration, refresh func(context.Context) (state.Snapshot, []string, error)) tea.Cmd {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	return tea.Tick(interval, func(time.Time) tea.Msg {
		snapshot, watchFeed, err := refresh(context.Background())
		return refreshMsg{
			snapshot:  snapshot,
			watchFeed: watchFeed,
			err:       err,
		}
	})
}

func (m model) View() string {
	leftWidth := max(20, m.width/2-1)
	rightWidth := max(20, m.width-leftWidth-3)
	topHeight := max(6, m.height/2-1)

	live := pane("LIVE TABLE", leftWidth, topHeight, m.liveTableLines())
	watch := pane("WATCH FEED", rightWidth, topHeight, m.watchFeedLines())
	human := pane("COMMAND / HUMAN TERMINAL", leftWidth, max(6, m.height-topHeight-3), m.commandLines())
	inspector := pane("INSPECTOR", rightWidth, max(6, m.height-topHeight-3), m.inspectorLines())

	top := joinHorizontal(live, watch)
	bottom := joinHorizontal(human, inspector)
	return top + "\n" + bottom
}

func (m model) liveTableLines() []string {
	runID, runStatus := currentRunSummary(m.opts)
	activeClaims := countClaims(m.opts.Snapshot.Claims, "active")
	suspendedClaims := countClaims(m.opts.Snapshot.Claims, "suspended")
	pendingProposals := countProposals(m.opts.Snapshot.Proposals, "pending", "in_review")
	blockedTasks := countTasks(m.opts.Snapshot.Tasks, "blocked")
	lines := []string{
		fmt.Sprintf("Run: %s [%s]", blankIfUnset(runID, "none"), blankIfUnset(runStatus, "unknown")),
		fmt.Sprintf("Goal: %s", blankIfUnset(m.opts.Goal, "not set")),
		fmt.Sprintf("Tasks: %d active, %d blocked", countTasks(m.opts.Snapshot.Tasks, "open", "in_progress"), blockedTasks),
		fmt.Sprintf("Agents: %d enabled", countEnabledAgents(m.opts.Snapshot.Agents)),
		fmt.Sprintf("Claims: %d active, %d suspended", activeClaims, suspendedClaims),
		fmt.Sprintf("Proposals: %d pending", pendingProposals),
		fmt.Sprintf("Human approvals: %d requested", countApprovals(m.opts.Snapshot.HumanApprovals, "requested")),
	}
	for _, tx := range latestTransactions(m.opts.Snapshot.Transactions, 2) {
		lines = append(lines, fmt.Sprintf("TX %s [%s]", tx.ID, tx.Status))
	}
	return lines
}

func (m model) watchFeedLines() []string {
	if len(m.opts.WatchFeed) > 0 {
		return m.opts.WatchFeed
	}
	return []string{
		"MCP server listening",
		fmt.Sprintf("Socket: %s", blankIfUnset(m.opts.SocketPath, "not set")),
		"Press q to quit",
	}
}

func (m model) commandLines() []string {
	contentHeight := max(1, max(6, m.height-max(6, m.height/2-1)-3)-2)
	lines := []string{}
	if m.commandBusy {
		lines = append(lines, "working...")
	}
	if len(m.commandOutput) == 0 {
		lines = append(lines, "Roundtable commands; no shell", "Try: mcp call --tool proposal.list")
	} else {
		outputHeight := max(0, contentHeight-2-len(lines))
		if outputHeight > len(m.commandOutput) {
			outputHeight = len(m.commandOutput)
		}
		if outputHeight > 0 {
			lines = append(lines, m.commandOutput[len(m.commandOutput)-outputHeight:]...)
		}
	}
	lines = append(lines, "rt> "+m.commandInput, "Enter run; Ctrl+U clear; q quit")
	return lines
}

func (m model) inspectorLines() []string {
	lines := []string{"Selected item: auto"}
	if proposal := latestProposal(m.opts.Snapshot.Proposals); proposal != nil {
		lines = append(lines,
			fmt.Sprintf("Proposal: %s [%s]", proposal.ID, proposal.Status),
			fmt.Sprintf("Summary: %s", proposal.Title),
		)
	}
	if tx := latestTransaction(m.opts.Snapshot.Transactions); tx != nil {
		lines = append(lines,
			fmt.Sprintf("Transaction: %s [%s]", tx.ID, tx.Status),
			fmt.Sprintf("Applied by: %s", blankIfUnset(tx.AppliedBy, "unknown")),
		)
	}
	if approval := latestApproval(m.opts.Snapshot.HumanApprovals); approval != nil {
		lines = append(lines, fmt.Sprintf("Approval: %s [%s]", approval.ID, approval.Status))
	}
	if len(lines) == 1 {
		lines = append(lines, "No proposals, transactions, or approvals yet.")
	}
	return lines
}

func pane(title string, width, height int, body []string) string {
	contentHeight := max(1, height-2)
	lines := make([]string, 0, contentHeight)
	for i := 0; i < contentHeight; i++ {
		if i < len(body) {
			lines = append(lines, pad(body[i], width-2))
			continue
		}
		lines = append(lines, strings.Repeat(" ", width-2))
	}

	var b strings.Builder
	b.WriteString("+" + strings.Repeat("-", width-2) + "+\n")
	b.WriteString("|" + pad(title, width-2) + "|\n")
	for _, line := range lines {
		b.WriteString("|" + line + "|\n")
	}
	b.WriteString("+" + strings.Repeat("-", width-2) + "+")
	return b.String()
}

func joinHorizontal(left, right string) string {
	leftLines := strings.Split(left, "\n")
	rightLines := strings.Split(right, "\n")
	maxLines := max(len(leftLines), len(rightLines))
	for len(leftLines) < maxLines {
		leftLines = append(leftLines, strings.Repeat(" ", len(leftLines[0])))
	}
	for len(rightLines) < maxLines {
		rightLines = append(rightLines, strings.Repeat(" ", len(rightLines[0])))
	}
	rows := make([]string, 0, maxLines)
	for i := 0; i < maxLines; i++ {
		rows = append(rows, leftLines[i]+" "+rightLines[i])
	}
	return strings.Join(rows, "\n")
}

func pad(s string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) > width {
		return string(runes[:width])
	}
	return s + strings.Repeat(" ", width-len(runes))
}

func blankIfUnset(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func currentRunSummary(opts Options) (string, string) {
	for i := len(opts.Snapshot.Runs) - 1; i >= 0; i-- {
		run := opts.Snapshot.Runs[i]
		if opts.RunID == "" || run.ID == opts.RunID {
			return run.ID, run.Status
		}
	}
	return opts.RunID, ""
}

func countClaims(claims []db.Claim, statuses ...string) int {
	return countByStatus(statuses, func(status string) int {
		count := 0
		for _, claim := range claims {
			if claim.Status == status {
				count++
			}
		}
		return count
	})
}

func countTasks(tasks []db.Task, statuses ...string) int {
	return countByStatus(statuses, func(status string) int {
		count := 0
		for _, task := range tasks {
			if task.Status == status {
				count++
			}
		}
		return count
	})
}

func countProposals(proposals []db.Proposal, statuses ...string) int {
	return countByStatus(statuses, func(status string) int {
		count := 0
		for _, proposal := range proposals {
			if proposal.Status == status {
				count++
			}
		}
		return count
	})
}

func countApprovals(approvals []db.HumanApproval, statuses ...string) int {
	return countByStatus(statuses, func(status string) int {
		count := 0
		for _, approval := range approvals {
			if approval.Status == status {
				count++
			}
		}
		return count
	})
}

func countByStatus(statuses []string, countFn func(string) int) int {
	total := 0
	for _, status := range statuses {
		total += countFn(status)
	}
	return total
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

func latestTransactions(transactions []db.Transaction, limit int) []db.Transaction {
	if len(transactions) == 0 {
		return nil
	}
	copied := append([]db.Transaction(nil), transactions...)
	sort.Slice(copied, func(i, j int) bool {
		return copied[i].AppliedAt > copied[j].AppliedAt
	})
	if limit > 0 && len(copied) > limit {
		copied = copied[:limit]
	}
	return copied
}

func latestTransaction(transactions []db.Transaction) *db.Transaction {
	found := latestTransactions(transactions, 1)
	if len(found) == 0 {
		return nil
	}
	return &found[0]
}

func latestProposal(proposals []db.Proposal) *db.Proposal {
	if len(proposals) == 0 {
		return nil
	}
	copied := append([]db.Proposal(nil), proposals...)
	sort.Slice(copied, func(i, j int) bool {
		return copied[i].CreatedAt > copied[j].CreatedAt
	})
	return &copied[0]
}

func latestApproval(approvals []db.HumanApproval) *db.HumanApproval {
	if len(approvals) == 0 {
		return nil
	}
	copied := append([]db.HumanApproval(nil), approvals...)
	sort.Slice(copied, func(i, j int) bool {
		return copied[i].UpdatedAt > copied[j].UpdatedAt
	})
	return &copied[0]
}
