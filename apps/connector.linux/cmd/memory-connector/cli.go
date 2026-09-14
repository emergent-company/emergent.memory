package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/emergent-company/memory.web-ui/connector/internal/account"
	"github.com/emergent-company/memory.web-ui/connector/internal/config"
)

// exitError carries an explicit process exit code out of a cobra RunE. main
// translates it into os.Exit, so cobra never prints anything itself (which
// would duplicate the message the runX implementation already wrote).
type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

// runResult adapts a runX return code to a cobra RunE error: 0 becomes nil,
// anything else an exitError so main can exit with that code.
func runResult(code int) error {
	if code == 0 {
		return nil
	}
	return exitError{code: code}
}

// stdlibArgs reconstructs the argument list an existing stdlib flag.FlagSet
// expects from a cobra command's parsed flags plus positional arguments. Only
// flags the user actually set are emitted (as --name=value) so the stdlib
// defaults and "was it set" detection (fs.Visit) behave exactly as before.
func stdlibArgs(cmd *cobra.Command, args []string) []string {
	var out []string
	cmd.Flags().Visit(func(f *pflag.Flag) {
		out = append(out, "--"+f.Name+"="+f.Value.String())
	})
	return append(out, args...)
}

// leaf builds a cobra leaf command that registers the given flags and delegates
// to run, passing the reconstructed stdlib-style arguments. The run function is
// the existing runX implementation, so output, exit codes, and the direct unit
// tests that call runX are unchanged.
func leaf(use, short, long string, flags func(*cobra.Command), run func([]string, io.Writer, io.Writer) int) *cobra.Command {
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Long:  long,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runResult(run(stdlibArgs(cmd, args), cmd.OutOrStdout(), cmd.ErrOrStderr()))
		},
	}
	if flags != nil {
		flags(cmd)
	}
	return cmd
}

// newRootCommand assembles the whole command tree. It is a constructor (not a
// package global) so tests can build isolated roots with their own streams.
func newRootCommand(stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:   "memory-connector",
		Short: "Relay local tools to a Memory project over the MCP relay",
		Long: `memory-connector relays tools running on this machine to a Memory project
over the MCP relay, using a single outbound WebSocket (no inbound ports).

Run 'memory-connector <command> --help' for details on any command, and
'memory-connector completion <shell>' to install shell completion.`,
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "memory-connector: unknown command %q\n", args[0])
				return exitError{code: 2}
			}
			// Bare invocation shows the command list, so a new user sees what
			// they can do instead of only the version.
			return cmd.Help()
		},
	}
	root.SetVersionTemplate("memory-connector {{.Version}}\n")
	root.SetOut(stdout)
	root.SetErr(stderr)

	root.AddCommand(
		newInitCmd(),
		leaf("relay", "run the MCP relay client", "Run the MCP relay client in the foreground, serving this machine's tools over the project's MCP relay until interrupted.", relayFlags, runRelay),
		leaf("daemon", "run the relay client under a single-instance lock", "Run the MCP relay client as a long-lived daemon. Identical to 'relay' but holds an exclusive lock beside the config file so two daemons cannot serve the same config.", relayFlags, runDaemon),
		newStatusCmd(),
		newAuthCmd(),
		newProjectsCmd(),
		leaf("install", "install + start the systemd user unit (Linux only)", "Install and start the systemd user unit that runs 'memory-connector daemon'. Linux only.", installFlags, runInstall),
		leaf("uninstall", "stop + remove the systemd user unit (Linux only)", "Stop, disable, and remove the systemd user unit installed by 'memory-connector install'. Linux only and idempotent.", configFlags, runUninstall),
		newUpgradeCmd(),
	)
	return root
}

// configFlags registers the shared --config flag.
func configFlags(cmd *cobra.Command) {
	cmd.Flags().String("config", config.DefaultConfigPath(), "config file path")
}

// relayFlags registers the flags shared by relay and daemon.
func relayFlags(cmd *cobra.Command) {
	configFlags(cmd)
	cmd.Flags().Int("api-port", 0, "loopback management API port (127.0.0.1; 0 disables)")
}

// installFlags registers install's flags.
func installFlags(cmd *cobra.Command) {
	configFlags(cmd)
	cmd.Flags().String("bin", "", "path to the memory-connector binary (default: this executable)")
}

func newInitCmd() *cobra.Command {
	return leaf("init", "configure server, token, and instance id", "Write the connector config (mode 0600) from flags, then verify connectivity against the hub. On probe failure the config file is removed so an unusable config never persists.", func(cmd *cobra.Command) {
		cmd.Flags().String("server-url", "", "Memory server base URL (required)")
		cmd.Flags().String("token", "", "project-scoped bearer token (required)")
		cmd.Flags().String("project-id", "", "project id (optional when the token is project-scoped)")
		cmd.Flags().String("instance-id", "", fmt.Sprintf("instance id (default %q)", config.DefaultInstanceID()))
		configFlags(cmd)
	}, runInit)
}

func newStatusCmd() *cobra.Command {
	return leaf("status", "show local state and hub presence", "Print the local instance id and version, the registered tool names, and a best-effort hub presence/parity check. Exits 0 even when the instance is not connected or the hub is unreachable.", func(cmd *cobra.Command) {
		configFlags(cmd)
		cmd.Flags().Bool("json", false, "emit machine-readable JSON")
	}, runStatus)
}

// authCommonFlags registers the --config/--server flags shared by auth
// subcommands.
func authCommonFlags(cmd *cobra.Command) {
	configFlags(cmd)
	cmd.Flags().String("server", "", "Memory server URL (defaults to config server_url)")
}

// authJSONFlags registers the shared flags plus --json.
func authJSONFlags(cmd *cobra.Command) {
	authCommonFlags(cmd)
	cmd.Flags().Bool("json", false, "emit machine-readable JSON")
}

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "sign in, sign out, or show account status",
		Long: `Sign in with OAuth 2.0 (device flow or PKCE), sign out, inspect stored accounts,
or print the current account's access token.

Use 'memory-connector auth <subcommand> --help' for details.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "memory-connector auth: unknown subcommand %q\n", args[0])
				return exitError{code: 2}
			}
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "memory-connector auth: missing subcommand (login, logout, start, complete, cancel, access-token, import, list, or status)")
			return exitError{code: 2}
		},
	}
	cmd.AddCommand(
		leaf("login", "sign in with the device authorization flow", "Sign in with the OAuth 2.0 device authorization flow. Resolves the server's OIDC issuer, prints a URL and code, and stores the resulting session.", func(cmd *cobra.Command) {
			authCommonFlags(cmd)
			cmd.Flags().String("client-id", account.ClientID, "OAuth client id (defaults to the connector client)")
			cmd.Flags().String("issuer", "", "OIDC issuer URL (skips /api/auth/issuer discovery)")
		}, runAuthLogin),
		leaf("logout", "sign out and clear the stored session", "Sign out of the resolved (or active) account and drop its locally stored project tokens. Idempotent.", authCommonFlags, runAuthLogout),
		leaf("status", "show the signed-in account status", "Print the server, email, issuer, and token expiry for the resolved (or active) account.", authJSONFlags, runAuthStatus),
		leaf("start", "begin a two-step PKCE login", "Begin a two-step PKCE login for a coordinating app (for example the macOS app, which owns the browser and custom-scheme callback).", func(cmd *cobra.Command) {
			authJSONFlags(cmd)
			cmd.Flags().String("redirect-uri", "", "OAuth redirect URI registered by the app")
			cmd.Flags().String("client-id", account.ClientID, "OAuth client id (defaults to the connector client)")
			cmd.Flags().String("issuer", "", "OIDC issuer URL (skips /api/auth/issuer discovery; --server may be omitted)")
		}, runAuthStart),
		leaf("complete", "finish a PKCE login", "Complete a PKCE login started by 'auth start' and report the resulting account.", func(cmd *cobra.Command) {
			authJSONFlags(cmd)
			cmd.Flags().String("login-id", "", "login id returned by 'auth start'")
			cmd.Flags().String("code", "", "authorization code from the callback")
			cmd.Flags().String("state", "", "state from the callback (must match 'auth start')")
		}, runAuthComplete),
		leaf("cancel", "discard a pending PKCE login", "Discard a pending PKCE login started by 'auth start'. Best-effort.", func(cmd *cobra.Command) {
			configFlags(cmd)
			cmd.Flags().String("login-id", "", "login id returned by 'auth start'")
		}, runAuthCancel),
		leaf("access-token", "print the stored account's access token", "Print the stored account's OAuth access token, refreshing it first when expired or within 60 seconds of expiry. Never reads or prints the refresh token.", authJSONFlags, runAuthAccessToken),
		leaf("import", "store a session piped on stdin", "Read a single session JSON object from stdin and store it. It is the migration path for clients (such as the macOS app) that can read sessions the CLI cannot.", func(cmd *cobra.Command) {
			configFlags(cmd)
			cmd.Flags().String("server", "", "Memory server URL (required)")
			cmd.Flags().Bool("json", false, "emit machine-readable JSON")
		}, runAuthImport),
		leaf("list", "list stored accounts", "Enumerate the servers this connector has stored sessions for. An empty list is a valid, non-error result.", func(cmd *cobra.Command) {
			configFlags(cmd)
			cmd.Flags().Bool("json", false, "emit machine-readable JSON")
		}, runAuthList),
	)
	return cmd
}

// projectCommonFlags registers the --config/--server flags shared by projects
// subcommands.
func projectCommonFlags(cmd *cobra.Command) {
	configFlags(cmd)
	cmd.Flags().String("server", "", "Memory server URL (defaults to config server_url or the active account)")
}

// projectJSONFlags registers the shared flags plus --json.
func projectJSONFlags(cmd *cobra.Command) {
	projectCommonFlags(cmd)
	cmd.Flags().Bool("json", false, "emit machine-readable JSON")
}

func newProjectsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "projects",
		Short: "list projects and select the active one",
		Long: `List the projects available to the signed-in account, select the active one,
and materialize the connector config so 'relay' can use it.

Use 'memory-connector projects <subcommand> --help' for details.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "memory-connector projects: unknown subcommand %q\n", args[0])
				return exitError{code: 2}
			}
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "memory-connector projects: missing subcommand (list, use, or current)")
			return exitError{code: 2}
		},
	}
	cmd.AddCommand(
		leaf("list", "list projects for the signed-in account", "List the projects available to the signed-in account and mark the active one.", projectJSONFlags, runProjectsList),
		leaf("use", "select the active project and write the config", "Resolve a project by id or name, remember it as the active project, ensure a project token, and materialize the connector config.", func(cmd *cobra.Command) {
			projectJSONFlags(cmd)
			cmd.Flags().String("instance-id", "", "instance id to write into the config (defaults to preserving the existing value)")
			cmd.Flags().String("disabled-tools", "", "comma-separated tool names to disable (empty string clears; omit to preserve)")
		}, runProjectsUse),
		leaf("current", "show the active project", "Show the active project for the resolved (or active) account.", projectJSONFlags, runProjectsCurrent),
	)
	return cmd
}
