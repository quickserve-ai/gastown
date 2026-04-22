package session

import "github.com/steveyegge/gastown/internal/config"

// EnvLookup gets the value of a tmux session environment variable.
// Mirrors tmux.Tmux.GetEnvironment's signature so managers can pass the
// method value directly without coupling to their package-local tmuxOps.
type EnvLookup func(sessionName, key string) (string, error)

// ExpectedAgentName reports the agent alias that a manager would spawn right
// now, given an optional explicit override and the config-resolved runtime.
// Mirrors the priority used by config.BuildStartupCommandWithAgentOverride
// when it stamps GT_AGENT into the session environment: override wins, then
// rc.ResolvedAgent, else empty.
func ExpectedAgentName(agentOverride string, rc *config.RuntimeConfig) string {
	if agentOverride != "" {
		return agentOverride
	}
	if rc != nil && rc.ResolvedAgent != "" {
		return rc.ResolvedAgent
	}
	return ""
}

// AgentDrift reports whether a running session's stored GT_AGENT differs from
// the agent the manager would spawn today.
//
// Persistent tmux sessions (deacon, witness) bake the resolved agent into
// their pane_start_command at creation; tmux auto-respawn replays that stored
// command verbatim, so later edits to role_agents/default_agent are invisible
// to a live session until it is killed and recreated. Managers call this
// before returning ErrAlreadyRunning, and fall through to the kill path on
// drift.
//
// Any lookup error — including a missing GT_AGENT env var on legacy sessions
// — is treated as drift: we cannot prove the baked-in command is correct, so
// rebuild to be safe. The rebuild cost is a single Claude re-initialization;
// the cost of NOT rebuilding is replaying the wrong model indefinitely.
func AgentDrift(getEnv EnvLookup, sessionID, expected string) bool {
	stored, err := getEnv(sessionID, "GT_AGENT")
	if err != nil {
		return true
	}
	return stored != expected
}
