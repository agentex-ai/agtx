package cli

import (
	"encoding/json"
	"fmt"
	"strings"
)

func agentSnippet(target string) (string, error) {
	switch strings.ToLower(target) {
	case "codex":
		return `# Add to Codex config.toml
[mcp_servers.agtx]
command = "agtx"
args = ["mcp"]

# Suggested project instruction:
# Use agtx through MCP for native skills. Prefer search_skills before install_skill.
# Pass yes=true only after explaining install/upgrade/rollback intent.
`, nil
	case "cc", "claude", "claude-code":
		return `# Claude Code
claude mcp add agtx -- agtx mcp

# Suggested CLAUDE.md note:
# Use the agtx MCP server for native skill discovery, install, and execution.
# Mutating tools require yes=true and should be explained before use.
`, nil
	case "cursor", "opencode", "trae", "openclaw", "hermes":
		payload := map[string]any{
			"mcpServers": map[string]any{
				"agtx": map[string]any{
					"command": "agtx",
					"args":    []string{"mcp"},
				},
			},
		}
		data, _ := json.MarshalIndent(payload, "", "  ")
		return string(data) + `

Suggested agent rule:
Use agtx through MCP for native skills. Search before installing. Never set yes=true for install, upgrade, or rollback without stating the intended mutation.
`, nil
	default:
		return "", fmt.Errorf("unsupported agent target: %s", target)
	}
}
