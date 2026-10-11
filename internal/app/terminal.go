package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

const terminalOutputLimit = 16 * 1024

type terminalOutputBuffer struct {
	data      []byte
	truncated bool
}

func (b *terminalOutputBuffer) Write(data []byte) (int, error) {
	remaining := terminalOutputLimit - len(b.data)
	if remaining > 0 {
		b.data = append(b.data, data[:min(len(data), remaining)]...)
	}
	if len(data) > remaining {
		b.truncated = true
	}
	return len(data), nil
}

func (b *terminalOutputBuffer) String() string {
	result := string(b.data)
	if b.truncated {
		result += "\n... terminal output truncated ..."
	}
	return result
}

func executeTerminalCommand(ctx context.Context, root, input string) (string, error) {
	args, err := tokenizeTerminalCommand(input)
	if err != nil {
		return "", err
	}
	if err := validateTerminalCommand(args); err != nil {
		return "", err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve project root: %w", err)
	}
	args = pinTerminalRoot(args, absRoot)
	var output terminalOutputBuffer
	err = Run(ctx, args, &output, &output)
	return output.String(), err
}

func pinTerminalRoot(args []string, root string) []string {
	position := 1
	switch args[0] {
	case "claims", "mcp":
		position = min(len(args), 2)
	case "sessions":
		if len(args) > 1 {
			switch args[1] {
			case "register", "heartbeat", "end":
				position = 2
			}
		}
	}
	result := make([]string, 0, len(args)+2)
	result = append(result, args[:position]...)
	result = append(result, "--root", root)
	result = append(result, args[position:]...)
	return result
}

func validateTerminalCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("enter a Roundtable command")
	}
	for _, arg := range args {
		if arg == "--" || arg == "--root" || arg == "-root" || strings.HasPrefix(arg, "--root=") || strings.HasPrefix(arg, "-root=") {
			return errors.New("terminal commands cannot override the active project root")
		}
	}
	allowed := false
	switch args[0] {
	case "table", "watch":
		allowed = true
	case "claims":
		if len(args) >= 2 {
			switch args[1] {
			case "list", "claim", "release", "revoke", "expire", "reconcile":
				allowed = true
			}
		}
	case "sessions":
		allowed = len(args) == 1
		if len(args) >= 2 {
			switch args[1] {
			case "register", "heartbeat", "end":
				allowed = true
			}
		}
	case "mcp":
		allowed = len(args) >= 2 && args[1] == "call"
	}
	if !allowed {
		return errors.New("command is not available in the Roundtable terminal; use table, watch, claims, sessions, or mcp call")
	}
	return nil
}

func tokenizeTerminalCommand(input string) ([]string, error) {
	if len(input) > 4096 {
		return nil, errors.New("terminal command exceeds the 4096-byte limit")
	}
	var args []string
	var token strings.Builder
	var quote rune
	started, escaped := false, false
	flush := func() error {
		if !started {
			return nil
		}
		if token.Len() > 1024 {
			return errors.New("terminal argument exceeds the 1024-byte limit")
		}
		args = append(args, token.String())
		token.Reset()
		started = false
		if len(args) > 64 {
			return errors.New("terminal command exceeds the 64-argument limit")
		}
		return nil
	}
	for _, char := range input {
		if escaped {
			token.WriteRune(char)
			escaped = false
			continue
		}
		if char == '\\' && quote != '\'' {
			escaped = true
			started = true
			continue
		}
		if quote != 0 {
			if char == quote {
				quote = 0
			} else {
				token.WriteRune(char)
			}
			continue
		}
		if char == '\'' || char == '"' {
			quote = char
			started = true
			continue
		}
		if char == '\n' || char == '\r' || char == '\t' || char == ' ' {
			if err := flush(); err != nil {
				return nil, err
			}
			continue
		}
		token.WriteRune(char)
		started = true
	}
	if escaped || quote != 0 {
		return nil, errors.New("terminal command has an unfinished escape or quote")
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return args, nil
}
