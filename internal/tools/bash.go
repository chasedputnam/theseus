package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

const maxOutputLen = 10_000

// DoBash executes a shell command using the incoming context for timeout control.
// Callers (e.g. agent loop) set their own deadline via context.WithTimeout.
func DoBash(ctx context.Context, command string) (string, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	result := out.String()
	if len(result) > maxOutputLen {
		result = result[:maxOutputLen] + fmt.Sprintf("\n... (truncated, %d chars total)", len(result))
	}
	if ctx.Err() == context.DeadlineExceeded {
		return result, fmt.Errorf("command timed out")
	}
	if err != nil {
		if result == "" {
			result = fmt.Sprintf("[exit: %v]", err)
		}
	}
	return strings.TrimRight(result, "\n"), nil
}

// DoPython executes Python code using the incoming context for timeout control.
func DoPython(ctx context.Context, code string) (string, error) {
	cmd := exec.CommandContext(ctx, "python3", "-c", code)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	result := out.String()
	if len(result) > maxOutputLen {
		result = result[:maxOutputLen] + fmt.Sprintf("\n... (truncated, %d chars total)", len(result))
	}
	if ctx.Err() == context.DeadlineExceeded {
		return result, fmt.Errorf("python timed out")
	}
	if err != nil {
		if result == "" {
			result = fmt.Sprintf("[exit: %v]", err)
		}
	}
	return strings.TrimRight(result, "\n"), nil
}
