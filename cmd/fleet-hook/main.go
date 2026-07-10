// Command fleet-hook is a thin standalone wrapper around agent.RunHook, the
// PreToolUse hook body. fleet.exe now self-invokes as its own hook (see the
// --agent-hook sentinel in the app's main), so this separate binary is not
// required at runtime; it is kept so a standalone hook can still be built if
// ever needed. It reads the tool call JSON on stdin, forwards it to the running
// fleet app over the loopback URL in FLEET_HOOK_URL, and prints the user's
// allow/deny decision. Any failure fails safe to deny.
package main

import (
	"net/http"
	"os"
	"time"

	"github.com/hoijun/fleet/internal/agent"
)

func main() {
	// Generous timeout: the app holds the request open while a human approves.
	client := &http.Client{Timeout: 15 * time.Minute}
	agent.RunHook(os.Stdin, os.Stdout, os.Getenv("FLEET_HOOK_URL"), client)
}
