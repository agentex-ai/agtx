package cli

import (
	"fmt"

	"github.com/agentex-ai/agtx/internal/agent"
)

type agentInitInfo = agent.InitInfo

func agentSnippet(target string) (agentInitInfo, error) {
	info, ok := agent.LookupTarget(target)
	if !ok {
		return agentInitInfo{}, fmt.Errorf("unsupported agent target: %s", target)
	}
	return info, nil
}

func agentTargets() []agentInitInfo {
	return agent.Targets()
}

func renderAgentSnippet(info agentInitInfo) string {
	return agent.Render(info)
}

func supportedAgentTargets() []string {
	return agent.SupportedTargets()
}
