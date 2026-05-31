package agent

import (
	"encoding/json"
	"strings"
)

type InitInfo struct {
	Target          string   `json:"target"`
	DisplayName     string   `json:"display_name,omitempty"`
	Aliases         []string `json:"aliases,omitempty"`
	Summary         string   `json:"summary,omitempty"`
	ConfigHint      string   `json:"config_hint,omitempty"`
	ConfigFormat    string   `json:"config_format,omitempty"`
	ConfigPathHints []string `json:"config_path_hints,omitempty"`
	ConfigSnippet   string   `json:"config_snippet,omitempty"`
	CommandHint     string   `json:"command_hint,omitempty"`
	CommandSnippet  string   `json:"command_snippet,omitempty"`
	RuleHint        string   `json:"rule_hint,omitempty"`
	RulePathHints   []string `json:"rule_path_hints,omitempty"`
	RuleSnippet     string   `json:"rule_snippet,omitempty"`
	SetupSteps      []Step   `json:"setup_steps,omitempty"`
}

type Step struct {
	ID           string       `json:"id"`
	Kind         string       `json:"kind,omitempty"`
	Title        string       `json:"title,omitempty"`
	Summary      string       `json:"summary,omitempty"`
	Format       string       `json:"format,omitempty"`
	PathHints    []string     `json:"path_hints,omitempty"`
	Platforms    []string     `json:"platforms,omitempty"`
	AppliesWhen  []Condition  `json:"applies_when,omitempty"`
	WritesFiles  []Artifact   `json:"writes_files,omitempty"`
	Artifacts    []Artifact   `json:"artifacts,omitempty"`
	Snippet      string       `json:"snippet,omitempty"`
	Optional     bool         `json:"optional,omitempty"`
	Priority     int          `json:"priority,omitempty"`
	Blocking     bool         `json:"blocking,omitempty"`
	Verification Verification `json:"verification,omitempty"`
}

type Condition struct {
	Field string   `json:"field"`
	AnyOf []string `json:"any_of,omitempty"`
	Note  string   `json:"note,omitempty"`
}

type Artifact struct {
	Kind        string   `json:"kind,omitempty"`
	Paths       []string `json:"paths,omitempty"`
	Summary     string   `json:"summary,omitempty"`
	ConsumableBy []string `json:"consumable_by,omitempty"`
}

type Verification struct {
	Kind        string `json:"kind,omitempty"`
	Command     string `json:"command,omitempty"`
	Expectation string `json:"expectation,omitempty"`
}

func Targets() []InitInfo {
	catalog := targetCatalog()
	targets := make([]InitInfo, 0, len(catalog))
	for _, info := range catalog {
		targets = append(targets, finalizeInfo(info))
	}
	return targets
}

func LookupTarget(target string) (InitInfo, bool) {
	normalized := strings.ToLower(strings.TrimSpace(target))
	for _, candidate := range targetCatalog() {
		if normalized == candidate.Target {
			return finalizeInfo(candidate), true
		}
		for _, alias := range candidate.Aliases {
			if normalized == alias {
				return finalizeInfo(candidate), true
			}
		}
	}
	return InitInfo{}, false
}

func SupportedTargets() []string {
	return append([]string{}, "codex", "cc", "cursor", "opencode", "trae", "openclaw", "hermes")
}

func Render(info InitInfo) string {
	info = finalizeInfo(info)
	sections := []string{}
	for _, step := range info.SetupSteps {
		if step.Snippet == "" {
			continue
		}
		section := step.Snippet
		if step.Title != "" {
			section = "# " + step.Title + "\n" + section
		}
		if len(step.PathHints) > 0 {
			section = "# Path hint: " + strings.Join(step.PathHints, " | ") + "\n" + section
		}
		if len(step.Platforms) > 0 {
			section = "# Platforms: " + strings.Join(step.Platforms, ", ") + "\n" + section
		}
		if len(step.AppliesWhen) > 0 {
			section = "# Applies when: " + renderConditions(step.AppliesWhen) + "\n" + section
		}
		if len(step.WritesFiles) > 0 {
			section = "# Writes: " + renderArtifacts(step.WritesFiles) + "\n" + section
		}
		if len(step.Artifacts) > 0 {
			section = "# Produces: " + renderArtifacts(step.Artifacts) + "\n" + section
		}
		sections = append(sections, section)
	}
	return strings.Join(sections, "\n\n") + "\n"
}

func renderConditions(conditions []Condition) string {
	rendered := make([]string, 0, len(conditions))
	for _, condition := range conditions {
		part := condition.Field
		if len(condition.AnyOf) > 0 {
			part += "=" + strings.Join(condition.AnyOf, "|")
		}
		if condition.Note != "" {
			part += " (" + condition.Note + ")"
		}
		rendered = append(rendered, part)
	}
	return strings.Join(rendered, "; ")
}

func renderArtifacts(artifacts []Artifact) string {
	rendered := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		part := artifact.Kind
		if artifact.Summary != "" {
			part += ": " + artifact.Summary
		}
		if len(artifact.Paths) > 0 {
			part += " @ " + strings.Join(artifact.Paths, " | ")
		}
		if len(artifact.ConsumableBy) > 0 {
			part += " -> " + strings.Join(artifact.ConsumableBy, ",")
		}
		rendered = append(rendered, part)
	}
	return strings.Join(rendered, "; ")
}

func cloneInfo(info InitInfo) InitInfo {
	info.Aliases = append([]string{}, info.Aliases...)
	info.ConfigPathHints = append([]string{}, info.ConfigPathHints...)
	info.RulePathHints = append([]string{}, info.RulePathHints...)
	info.SetupSteps = cloneSteps(info.SetupSteps)
	return info
}

func cloneSteps(steps []Step) []Step {
	if len(steps) == 0 {
		return nil
	}
	cloned := make([]Step, 0, len(steps))
	for _, step := range steps {
		step.PathHints = append([]string{}, step.PathHints...)
		step.Platforms = append([]string{}, step.Platforms...)
		step.AppliesWhen = cloneConditions(step.AppliesWhen)
		step.WritesFiles = cloneArtifacts(step.WritesFiles)
		step.Artifacts = cloneArtifacts(step.Artifacts)
		step.Verification = cloneVerification(step.Verification)
		cloned = append(cloned, step)
	}
	return cloned
}

func cloneConditions(conditions []Condition) []Condition {
	if len(conditions) == 0 {
		return nil
	}
	cloned := make([]Condition, 0, len(conditions))
	for _, condition := range conditions {
		condition.AnyOf = append([]string{}, condition.AnyOf...)
		cloned = append(cloned, condition)
	}
	return cloned
}

func cloneArtifacts(artifacts []Artifact) []Artifact {
	if len(artifacts) == 0 {
		return nil
	}
	cloned := make([]Artifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		artifact.Paths = append([]string{}, artifact.Paths...)
		artifact.ConsumableBy = append([]string{}, artifact.ConsumableBy...)
		cloned = append(cloned, artifact)
	}
	return cloned
}

func cloneVerification(verification Verification) Verification {
	return verification
}

func finalizeInfo(info InitInfo) InitInfo {
	if len(info.SetupSteps) == 0 {
		info.SetupSteps = defaultSetupSteps(info)
	}
	return cloneInfo(info)
}

func defaultSetupSteps(info InitInfo) []Step {
	steps := []Step{}
	if info.ConfigSnippet != "" {
		steps = append(steps, Step{
			ID:        "config",
			Kind:      "config",
			Title:     info.ConfigHint,
			Summary:   "Add the agtx MCP server definition to the target agent configuration.",
			Format:    info.ConfigFormat,
			PathHints: append([]string{}, info.ConfigPathHints...),
			Platforms: []string{"macos"},
			AppliesWhen: []Condition{
				{
					Field: "config_scope",
					AnyOf: []string{"user"},
					Note:  "Use this when the MCP setup should only apply to the current user.",
				},
			},
			WritesFiles: []Artifact{
				{
					Kind:        "config_file",
					Paths:       append([]string{}, info.ConfigPathHints...),
					Summary:     "Updates the target agent config with an agtx MCP server entry.",
					ConsumableBy: []string{info.Target},
				},
			},
			Artifacts: []Artifact{
				{
					Kind:        "mcp_server_entry",
					Summary:     "An agtx MCP server definition that launches `agtx mcp`.",
					ConsumableBy: []string{info.Target, "agtx-mcp-client"},
				},
			},
			Snippet:   info.ConfigSnippet,
			Priority:  10,
			Blocking:  true,
			Verification: Verification{
				Kind:        "manual",
				Expectation: "The target agent config now contains an agtx MCP server entry that launches `agtx mcp`.",
			},
		})
	}
	if info.CommandSnippet != "" {
		steps = append(steps, Step{
			ID:       "command",
			Kind:     "command",
			Title:    info.CommandHint,
			Summary:  "Run the target-specific command that registers or enables agtx.",
			Format:   "shell",
			Platforms: []string{"macos"},
			Artifacts: []Artifact{
				{
					Kind:        "registered_mcp_server",
					Summary:     "A persisted agtx MCP registration inside the target agent runtime.",
					ConsumableBy: []string{info.Target},
				},
			},
			Snippet:  info.CommandSnippet,
			Priority: 20,
			Blocking: true,
			Verification: Verification{
				Kind:        "command",
				Command:     info.CommandSnippet,
				Expectation: "The command completes successfully and the target agent lists agtx as an enabled MCP server.",
			},
		})
	}
	if info.RuleSnippet != "" {
		steps = append(steps, Step{
			ID:        "rule",
			Kind:      "rule",
			Title:     info.RuleHint,
			Summary:   "Add a workflow rule so the agent uses agtx safely and predictably.",
			Format:    "text",
			PathHints: append([]string{}, info.RulePathHints...),
			Platforms: []string{"macos"},
			AppliesWhen: []Condition{
				{
					Field: "instruction_scope",
					AnyOf: []string{"project"},
					Note:  "Use this when your agent keeps project-local instructions or rule files.",
				},
			},
			WritesFiles: []Artifact{
				{
					Kind:        "instruction_file",
					Paths:       append([]string{}, info.RulePathHints...),
					Summary:     "Updates the project instruction/rules file with the agtx usage guidance.",
					ConsumableBy: []string{info.Target},
				},
			},
			Artifacts: []Artifact{
				{
					Kind:        "workflow_rule",
					Summary:     "A reusable agtx usage rule that can be displayed in onboarding UI or copied into repo docs.",
					ConsumableBy: []string{info.Target, "project-maintainer"},
				},
			},
			Snippet:   info.RuleSnippet,
			Optional:  true,
			Priority:  30,
			Blocking:  false,
			Verification: Verification{
				Kind:        "manual",
				Expectation: "The agent instruction file includes the agtx usage rule before interactive sessions begin.",
			},
		})
	}
	return steps
}

func targetCatalog() []InitInfo {
	payload := map[string]any{
		"mcpServers": map[string]any{
			"agtx": map[string]any{
				"command": "agtx",
				"args":    []string{"mcp"},
			},
		},
	}
	data, _ := json.MarshalIndent(payload, "", "  ")
	jsonSnippet := string(data)
	genericRule := `Use agtx through MCP for native skills.
Search before installing.
Never set yes=true for install, upgrade, or rollback without stating the intended mutation.`
	userConfigStep := func(title string, pathHints []string, note string) Step {
		return Step{
			ID:        "config_user",
			Kind:      "config",
			Title:     title,
			Summary:   "Add the agtx MCP server to a user-scoped JSON config for the current machine.",
			Format:    "json",
			PathHints: append([]string{}, pathHints...),
			Platforms: []string{"macos"},
			AppliesWhen: []Condition{
				{
					Field: "config_scope",
					AnyOf: []string{"user"},
					Note:  note,
				},
			},
			WritesFiles: []Artifact{
				{
					Kind:        "config_file",
					Paths:       append([]string{}, pathHints...),
					Summary:     "Updates the user-scoped agent MCP config with an agtx server entry.",
					ConsumableBy: []string{"agent-user-profile"},
				},
			},
			Artifacts: []Artifact{
				{
					Kind:        "mcp_server_entry",
					Summary:     "A user-level agtx MCP server definition that launches `agtx mcp`.",
					ConsumableBy: []string{"agent-user-profile", "agtx-mcp-client"},
				},
			},
			Snippet:   jsonSnippet,
			Priority:  10,
			Blocking:  true,
			Verification: Verification{
				Kind:        "manual",
				Expectation: "The user MCP config now includes an agtx server entry that launches `agtx mcp`.",
			},
		}
	}
	workspaceConfigStep := func(pathHint string) Step {
		return Step{
			ID:        "config_workspace",
			Kind:      "config",
			Title:     "Merge into workspace MCP config JSON",
			Summary:   "Add the agtx MCP server to a workspace-shared MCP config so teammates inherit the same setup.",
			Format:    "json",
			PathHints: []string{pathHint},
			Platforms: []string{"macos"},
			AppliesWhen: []Condition{
				{
					Field: "config_scope",
					AnyOf: []string{"workspace_shared"},
					Note:  "Choose this when your team shares agent configuration inside the repository or workspace.",
				},
			},
			WritesFiles: []Artifact{
				{
					Kind:        "workspace_config_file",
					Paths:       []string{pathHint},
					Summary:     "Updates the shared workspace MCP config with an agtx server entry.",
					ConsumableBy: []string{"workspace-teammates"},
				},
			},
			Artifacts: []Artifact{
				{
					Kind:        "shared_mcp_server_entry",
					Summary:     "A workspace-level agtx MCP server definition that teammates can reuse without per-user setup.",
					ConsumableBy: []string{"workspace-teammates", "agtx-mcp-client"},
				},
			},
			Snippet:   jsonSnippet,
			Priority:  11,
			Blocking:  true,
			Verification: Verification{
				Kind:        "manual",
				Expectation: "The workspace MCP config now includes an agtx server entry and teammates can load it without per-user edits.",
			},
		}
	}
	projectRuleStep := func(pathHints []string) Step {
		return Step{
			ID:        "rule",
			Kind:      "rule",
			Title:     "Suggested agent rule",
			Summary:   "Add a workflow rule so the agent uses agtx safely and predictably.",
			Format:    "text",
			PathHints: append([]string{}, pathHints...),
			Platforms: []string{"macos"},
			AppliesWhen: []Condition{
				{
					Field: "instruction_scope",
					AnyOf: []string{"project"},
					Note:  "Use this when your agent keeps project-local instructions or rule files.",
				},
			},
			WritesFiles: []Artifact{
				{
					Kind:        "instruction_file",
					Paths:       append([]string{}, pathHints...),
					Summary:     "Updates the project rule or instruction file with the agtx workflow guidance.",
					ConsumableBy: []string{"project-teammates"},
				},
			},
			Artifacts: []Artifact{
				{
					Kind:        "workflow_rule",
					Summary:     "A project-scoped agtx usage rule for consistent agent behavior.",
					ConsumableBy: []string{"project-teammates", "project-maintainer"},
				},
			},
			Snippet:   genericRule,
			Optional:  true,
			Priority:  30,
			Blocking:  false,
			Verification: Verification{
				Kind:        "manual",
				Expectation: "The instruction or rules file includes the agtx usage rule before interactive sessions begin.",
			},
		}
	}

	return []InitInfo{
		{
			Target:          "codex",
			DisplayName:     "Codex CLI",
			Summary:         "User-scoped TOML MCP config plus an optional project instruction.",
			ConfigHint:      "Add to Codex config.toml",
			ConfigFormat:    "toml",
			ConfigPathHints: []string{"user-scoped Codex config.toml"},
			ConfigSnippet: `[mcp_servers.agtx]
command = "agtx"
args = ["mcp"]`,
			RuleHint:      "Suggested project instruction",
			RulePathHints: []string{"project instruction file used by your Codex workflow"},
			RuleSnippet: `Use agtx through MCP for native skills.
Prefer search_skills before install_skill.
Pass yes=true only after explaining install/upgrade/rollback intent.`,
		},
		{
			Target:         "claude-code",
			DisplayName:    "Claude Code",
			Aliases:        []string{"cc", "claude"},
			Summary:        "One-time MCP registration command plus an optional CLAUDE.md rule.",
			CommandHint:    "Run in Claude Code",
			CommandSnippet: `claude mcp add agtx -- agtx mcp`,
			RuleHint:       "Suggested CLAUDE.md note",
			RulePathHints:  []string{"CLAUDE.md"},
			RuleSnippet: `Use the agtx MCP server for native skill discovery, install, and execution.
Mutating tools require yes=true and should be explained before use.`,
		},
		{
			Target:          "cursor",
			DisplayName:     "Cursor",
			Summary:         "Merge the MCP server into Cursor's JSON settings and keep the rule in your project instructions.",
			ConfigHint:      "Merge into the agent MCP config JSON",
			ConfigFormat:    "json",
			ConfigPathHints: []string{"agent MCP settings JSON", "workspace MCP JSON if your team shares agent config"},
			ConfigSnippet:   jsonSnippet,
			RuleHint:        "Suggested agent rule",
			RulePathHints:   []string{"workspace instruction or rules file"},
			RuleSnippet:     genericRule,
			SetupSteps: []Step{
				userConfigStep("Merge into user MCP config JSON", []string{"agent MCP settings JSON"}, "Use this when agtx should only be enabled for your local Cursor profile."),
				workspaceConfigStep("workspace MCP JSON if your team shares agent config"),
				projectRuleStep([]string{"workspace instruction or rules file"}),
			},
		},
		{
			Target:          "opencode",
			DisplayName:     "OpenCode",
			Summary:         "Merge the MCP server into the agent JSON config and keep the usage rule with your project instructions.",
			ConfigHint:      "Merge into the agent MCP config JSON",
			ConfigFormat:    "json",
			ConfigPathHints: []string{"agent MCP config JSON"},
			ConfigSnippet:   jsonSnippet,
			RuleHint:        "Suggested agent rule",
			RulePathHints:   []string{"project instruction file"},
			RuleSnippet:     genericRule,
			SetupSteps: []Step{
				userConfigStep("Merge into user MCP config JSON", []string{"agent MCP config JSON"}, "Use this when the OpenCode MCP config is stored per user."),
				projectRuleStep([]string{"project instruction file"}),
			},
		},
		{
			Target:          "trae",
			DisplayName:     "Trae",
			Summary:         "Merge the MCP server into Trae's JSON config and keep the workflow rule with project instructions.",
			ConfigHint:      "Merge into the agent MCP config JSON",
			ConfigFormat:    "json",
			ConfigPathHints: []string{"agent MCP config JSON"},
			ConfigSnippet:   jsonSnippet,
			RuleHint:        "Suggested agent rule",
			RulePathHints:   []string{"project instruction file"},
			RuleSnippet:     genericRule,
			SetupSteps: []Step{
				userConfigStep("Merge into user MCP config JSON", []string{"agent MCP config JSON"}, "Use this when the Trae MCP config is stored per user."),
				projectRuleStep([]string{"project instruction file"}),
			},
		},
		{
			Target:          "openclaw",
			DisplayName:     "OpenClaw",
			Summary:         "Merge the MCP server into OpenClaw's JSON config and keep the workflow rule with project instructions.",
			ConfigHint:      "Merge into the agent MCP config JSON",
			ConfigFormat:    "json",
			ConfigPathHints: []string{"agent MCP config JSON"},
			ConfigSnippet:   jsonSnippet,
			RuleHint:        "Suggested agent rule",
			RulePathHints:   []string{"project instruction file"},
			RuleSnippet:     genericRule,
			SetupSteps: []Step{
				userConfigStep("Merge into user MCP config JSON", []string{"agent MCP config JSON"}, "Use this when the OpenClaw MCP config is stored per user."),
				projectRuleStep([]string{"project instruction file"}),
			},
		},
		{
			Target:          "hermes",
			DisplayName:     "Hermes Agent",
			Summary:         "Merge the MCP server into Hermes JSON config and keep the workflow rule with project instructions.",
			ConfigHint:      "Merge into the agent MCP config JSON",
			ConfigFormat:    "json",
			ConfigPathHints: []string{"agent MCP config JSON"},
			ConfigSnippet:   jsonSnippet,
			RuleHint:        "Suggested agent rule",
			RulePathHints:   []string{"project instruction file"},
			RuleSnippet:     genericRule,
			SetupSteps: []Step{
				userConfigStep("Merge into user MCP config JSON", []string{"agent MCP config JSON"}, "Use this when the Hermes MCP config is stored per user."),
				projectRuleStep([]string{"project instruction file"}),
			},
		},
	}
}
