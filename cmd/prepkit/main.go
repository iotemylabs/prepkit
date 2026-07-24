// Command prepkit is a self-hostable emergency preparedness manager.
//
// It tracks preparedness kits, the items in them, and the expiration dates of
// the consumables inside them, then scores real holdings against reusable kit
// templates. See https://github.com/iotemylabs/prepkit-spec for the format
// this implements.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const (
	defaultListen  = "127.0.0.1:8214"
	envAdminSecret = "PREPKIT_ADMIN_SECRET"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	cmd := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	switch cmd {
	case "serve":
		return cmdServe(args)
	case "validate", "export-static", "export-json":
		fmt.Fprintf(os.Stderr, "prepkit %s: not implemented yet\n", cmd)
		return 2
	case "help":
		usage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "prepkit: unknown command %q\n\n", cmd)
		usage(os.Stderr)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `prepkit - self-hostable emergency preparedness manager

Usage:

  prepkit serve         start the web server (default when no command given)
  prepkit validate      validate template packs and exit nonzero on failure
  prepkit export-json   write a full JSON export to stdout or a file
  prepkit export-static write the offline static HTML export to a directory
  prepkit help          show this help

Run 'prepkit <command> -h' for the flags a command accepts.
`)
}

func cmdServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	listen := fs.String("listen", defaultListen, "address to bind, host:port")
	dataDir := fs.String("data-dir", "data", "directory for the SQLite database and generated files")
	secretFile := fs.String("admin-secret-file", "", "file containing the admin secret (overrides "+envAdminSecret+")")
	allowPublic := fs.Bool("allow-public-bind", false, "permit binding a non-loopback interface (prints a warning)")
	fs.Parse(args)

	secret, err := adminSecret(*secretFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "prepkit: %v\n", err)
		return 1
	}
	_ = secret // consumed by the web layer once it exists

	host, _, err := net.SplitHostPort(*listen)
	if err != nil {
		fmt.Fprintf(os.Stderr, "prepkit: invalid -listen address %q: %v\n", *listen, err)
		return 1
	}
	if !isLoopback(host) {
		if !*allowPublic {
			fmt.Fprintf(os.Stderr, "prepkit: refusing to bind non-loopback address %q without -allow-public-bind\n", *listen)
			return 1
		}
		printPublicBindWarning(*listen)
	}

	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "prepkit: creating data dir: %v\n", err)
		return 1
	}

	return serve(*listen)
}

// adminSecret resolves the admin secret from the given file, or failing that
// the environment. An unset secret is a startup error, not a warning: the
// threat model forbids default credentials.
func adminSecret(file string) (string, error) {
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("reading -admin-secret-file: %w", err)
		}
		s := strings.TrimSpace(string(b))
		if s == "" {
			return "", fmt.Errorf("-admin-secret-file %s is empty", file)
		}
		return s, nil
	}
	if s := os.Getenv(envAdminSecret); s != "" {
		return s, nil
	}
	return "", fmt.Errorf("no admin secret configured; set %s or -admin-secret-file (refusing to start without credentials)", envAdminSecret)
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func printPublicBindWarning(addr string) {
	fmt.Fprintf(os.Stderr, `
********************************************************************************
* WARNING: PrepKit is binding %-48s *
* This instance is reachable from other machines. A populated instance holds  *
* an indexed inventory of household valuables and identity documents. Do NOT  *
* expose it to the internet. Put it behind your LAN, a VPN, or a reverse      *
* proxy that authenticates before PrepKit ever sees the request.              *
********************************************************************************
`, addr)
}

func serve(addr string) int {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<!doctype html><title>PrepKit</title><h1>PrepKit</h1><p>Phase 1 scaffold. The application is under construction.</p>")
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		fmt.Printf("prepkit: listening on http://%s\n", addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "prepkit: %v\n", err)
			return 1
		}
	case <-ctx.Done():
		fmt.Println("prepkit: shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "prepkit: shutdown: %v\n", err)
			return 1
		}
	}
	return 0
}
