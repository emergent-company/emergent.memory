package cmd

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/emergent-company/emergent.memory/tools/cli/internal/graphexplore"
	"github.com/spf13/cobra"
)

var (
	graphExplorePortFlag    int
	graphExploreHostFlag    string
	graphExploreProjectFlag string
)

var graphExploreCmd = &cobra.Command{
	Use:   "explore",
	Short: "Explore the knowledge graph visually in the browser",
	Long: `Start a local web server and open an interactive graph visualizer in your browser.

The visualizer lets you search for nodes, click to expand neighbors, and follow
paths through the knowledge graph. All API calls are proxied through the CLI
process — no credentials are ever sent to the browser.

Examples:
  memory graph explore
  memory graph explore --port 7734
  memory graph explore --host 0.0.0.0 --port 7734
  memory graph explore --project my-project`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Resolve client + auth
		c, err := getClient(cmd)
		if err != nil {
			return err
		}

		projectID, err := resolveProjectContext(cmd, graphExploreProjectFlag)
		if err != nil {
			return err
		}
		c.SetContext("", projectID)

		serverURL := c.BaseURL()
		authHeader := c.AuthorizationHeader()

		// Find a free port
		port := graphExplorePortFlag
		if port == 0 {
			port = 7734
		}
		host := graphExploreHostFlag
		if host == "" {
			host = "127.0.0.1"
		}
		addr := fmt.Sprintf("%s:%d", host, port)

		// Check if the port is already in use
		if ln, err := net.Listen("tcp", addr); err != nil {
			return fmt.Errorf("port %d is already in use. Try --port <number>", port)
		} else {
			ln.Close()
		}

		// Create the graph explore server with templ+HTMX UI
		exploreSrv := graphexplore.NewServer(projectID, serverURL, authHeader)

		mux := http.NewServeMux()
		exploreSrv.RegisterRoutes(mux)

		srv := &http.Server{
			Addr:         addr,
			Handler:      mux,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 60 * time.Second,
		}

		// When binding to all interfaces, open localhost in the browser
		browserHost := host
		if host == "0.0.0.0" || host == "::" {
			browserHost = "localhost"
		}
		explorerURL := fmt.Sprintf("http://%s:%d", browserHost, port)
		fmt.Fprintf(os.Stderr, "\033[2;36mGraph Explorer running at %s\033[0m\n", explorerURL)
		fmt.Fprintf(os.Stderr, "\033[2;36mProject: %s\033[0m\n", projectID)
		fmt.Fprintf(os.Stderr, "\033[2mPress Ctrl+C to stop.\033[0m\n")

		// Open the browser after a short delay to let the server start
		go func() {
			time.Sleep(300 * time.Millisecond)
			openBrowser(explorerURL)
		}()

		return srv.ListenAndServe()
	},
}

// openBrowser opens the given URL in the default browser across platforms.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default: // linux and others
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

func init() {
	graphExploreCmd.Flags().IntVar(&graphExplorePortFlag, "port", 7734, "Local port to listen on")
	graphExploreCmd.Flags().StringVar(&graphExploreHostFlag, "host", "127.0.0.1", "Host/IP to bind (use 0.0.0.0 to listen on all interfaces)")
	graphExploreCmd.Flags().StringVar(&graphExploreProjectFlag, "project", "", "Project ID (overrides config/env)")

	graphCmd.AddCommand(graphExploreCmd)
}
