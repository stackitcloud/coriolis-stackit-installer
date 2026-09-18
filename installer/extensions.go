package installer

import "context"

// Extension adds an optional deployment action without coupling the public
// installer to a particular integration. Implementations live in separate Go
// modules and are injected by a custom command.
type Extension interface {
	Name() string
	Configure(configPath string) error
	AfterBootstrap(context.Context, ExtensionRuntime, DeploymentTarget) error
}

// ExtensionRuntime exposes the minimum management channel needed by a
// deployment extension. Commands run through the STACKIT Server Agent and do
// not alter the appliance's support SSH configuration.
type ExtensionRuntime interface {
	WaitServerAgent(context.Context, string) error
	RunShellScript(context.Context, string, string, bool) (string, error)
}

// DeploymentTarget identifies the provisioned appliance passed to an
// extension after the standard bootstrap has completed.
type DeploymentTarget struct {
	ProjectID string
	Region    string
	ServerID  string
	FQDN      string
}

func (c *Cloud) WaitServerAgent(ctx context.Context, serverID string) error {
	return c.waitServerAgent(ctx, c.project, serverID)
}

func (c *Cloud) RunShellScript(ctx context.Context, serverID, script string, streamOutput bool) (string, error) {
	return c.runShellScriptWithOutput(ctx, c.project, serverID, script, streamOutput)
}
