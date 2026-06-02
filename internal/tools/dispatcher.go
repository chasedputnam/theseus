package tools

import (
	"context"
	"fmt"

	"github.com/chaseputnam/theseus/internal/agent"
)

// Deps holds all dependencies tools may need.
type Deps struct {
	DataDir string
	DB      interface{} // *db.DB — kept as interface to avoid circular imports at this layer
}

// Dispatcher routes tool blocks to their implementations.
type Dispatcher struct {
	deps *Deps
}

// New creates a Dispatcher.
func New(deps *Deps) *Dispatcher {
	return &Dispatcher{deps: deps}
}

// Execute dispatches a ToolBlock to the correct do_* function.
func (d *Dispatcher) Execute(ctx context.Context, block agent.ToolBlock, owner string, privileges map[string]any) (string, error) {
	// Privilege checks
	switch block.ToolType {
	case "bash", "python":
		if !getBoolPriv(privileges, "can_use_bash") {
			return "", fmt.Errorf("permission denied: %s tool requires can_use_bash privilege", block.ToolType)
		}
	case "generate_image":
		if !getBoolPriv(privileges, "can_generate_images") {
			return "", fmt.Errorf("permission denied: image generation requires can_generate_images privilege")
		}
	case "manage_memory":
		if !getBoolPriv(privileges, "can_manage_memory") {
			return "", fmt.Errorf("permission denied: memory management requires can_manage_memory privilege")
		}
	}

	switch block.ToolType {
	case "bash":
		return DoBash(ctx, block.Content)
	case "python":
		return DoPython(ctx, block.Content)
	case "read_file":
		return DoReadFile(ctx, block.Content, d.deps.DataDir)
	case "write_file":
		return DoWriteFile(ctx, block.Content, d.deps.DataDir)
	case "web_search":
		return DoWebSearch(ctx, block.Content)
	default:
		return "", fmt.Errorf("tool %q is not implemented", block.ToolType)
	}
}

func getBoolPriv(privs map[string]any, key string) bool {
	if privs == nil {
		return true // no restrictions
	}
	v, ok := privs[key]
	if !ok {
		return true
	}
	b, _ := v.(bool)
	return b
}
