package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// DoWebSearch is a stub that will be wired to the real search client in Task 15.
func DoWebSearch(ctx context.Context, argsJSON string) (string, error) {
	var args struct {
		Query string `json:"query"`
		N     int    `json:"n"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		args.Query = argsJSON
	}
	if args.Query == "" {
		return "", fmt.Errorf("query required")
	}
	// Stub — real implementation wired in Task 15
	return fmt.Sprintf("[web_search stub: query=%q — wire search client in Task 15]", args.Query), nil
}
