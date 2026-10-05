package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/sifatulrabbi/prledger/internal/server"
	"github.com/sifatulrabbi/prledger/internal/web"
)

func newServeCmd(d Deps, g *globalFlags) *cobra.Command {
	var port int
	var noOpen bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Show your pull requests in the browser",
		Long:  "Starts a local web server on 127.0.0.1, prints its address and opens it in your browser. Press Ctrl-C to stop.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			t, err := resolveTarget(cmd, d, g)
			if err != nil {
				return err
			}
			tr, err := t.tracker(d)
			if err != nil {
				return err
			}
			if cerr := tr.CacheError(); cerr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: ignoring the cache: %v\n", cerr)
			}
			ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
			if err != nil {
				return fmt.Errorf("starting the server: %w", err)
			}
			// Start fetching now; the page shows the cached snapshot meanwhile
			// and its first refresh joins this fetch.
			go tr.Refresh(context.Background())

			url := fmt.Sprintf("http://%s/", ln.Addr())
			srv := &http.Server{
				Handler:           server.New(tr, web.Page()),
				ReadHeaderTimeout: 10 * time.Second,
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Serving %s at %s (Ctrl-C to stop)\n", t.repo, url)
			if !noOpen {
				if err := d.OpenBrowser(url); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "could not open the browser (%v); open %s yourself\n", err, url)
				}
			}
			return serveUntilDone(cmd.Context(), srv, ln)
		},
	}
	cmd.Flags().IntVar(&port, "port", 0, "port to listen on (default: any free port)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "do not open the browser")
	return cmd
}

// serveUntilDone serves until ctx is cancelled (Ctrl-C), then shuts down.
func serveUntilDone(ctx context.Context, srv *http.Server, ln net.Listener) error {
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil {
		return err
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
