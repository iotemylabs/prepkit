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
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"path/filepath"

	"github.com/iotemylabs/prepkit/internal/export"
	"github.com/iotemylabs/prepkit/internal/store"
	"github.com/iotemylabs/prepkit/internal/template"
	"github.com/iotemylabs/prepkit/templates"
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
	case "validate":
		return cmdValidate(args)
	case "export-json":
		return cmdExportJSON(args)
	case "export-static":
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
	templatesDir := fs.String("templates", "", "directory of additional template packs to load alongside the bundled ones")
	secretFile := fs.String("admin-secret-file", "", "file containing the admin secret (overrides "+envAdminSecret+")")
	allowPublic := fs.Bool("allow-public-bind", false, "permit binding a non-loopback interface (prints a warning)")
	fs.Parse(args)

	secret, err := adminSecret(*secretFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "prepkit: %v\n", err)
		return 1
	}
	_ = secret // consumed by the web layer once it exists

	// A bad pack is rejected at startup, with the same diagnostics
	// `prepkit validate` prints, rather than failing mysteriously later.
	pack, report := loadTemplates(*templatesDir)
	if !report.OK() {
		fmt.Fprintf(os.Stderr, "prepkit: refusing to start: %d template error(s)\n", len(report.Errors))
		for _, e := range report.Errors {
			fmt.Fprintf(os.Stderr, "  x %s\n", e)
		}
		return 1
	}
	fmt.Printf("prepkit: loaded %d template(s)\n", len(pack.IDs))

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

	db, err := store.Open(filepath.Join(*dataDir, "prepkit.db"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "prepkit: opening database: %v\n", err)
		return 1
	}
	defer db.Close()

	return serve(*listen)
}

// loadTemplates loads the bundled packs plus, when extraDir is nonempty, the
// user's pack directory, all in one id namespace so user templates may extend
// bundled ones.
func loadTemplates(extraDir string) (*template.Pack, *template.Report) {
	sources := []fs.FS{templates.FS}
	if extraDir != "" {
		sources = append(sources, os.DirFS(extraDir))
	}
	pack, report := template.Load(sources...)
	template.Validate(pack, report)
	return pack, report
}

// cmdValidate is the CLI face of the same checks the server runs at startup,
// so pack authors get identical feedback. With no argument it validates the
// bundled packs; with a directory argument, that directory alone.
func cmdValidate(args []string) int {
	fl := flag.NewFlagSet("validate", flag.ExitOnError)
	fl.Parse(args)

	var pack *template.Pack
	var report *template.Report
	label := "bundled templates"
	if dir := fl.Arg(0); dir != "" {
		label = dir
		pack, report = template.Load(os.DirFS(dir))
	} else {
		pack, report = template.Load(templates.FS)
	}
	template.Validate(pack, report)

	fmt.Printf("Loaded %d template(s) from %s\n\n", len(pack.IDs), label)
	for _, s := range report.Summaries {
		fmt.Printf("  %s  (%s)\n", s.File, s.ID)
		if s.Extends != "" {
			fmt.Printf("    extends %s: %d own lines -> %d resolved\n", s.Extends, s.OwnLines, s.Resolved)
		} else {
			fmt.Printf("    %d items\n", s.Resolved)
		}
		if s.HasCeiling {
			status := "ok"
			if s.EstimateKg > s.CeilingKg {
				status = "OVER"
			}
			fmt.Printf("    weight estimate: %.2f kg vs ceiling %g kg [%s]\n", s.EstimateKg, s.CeilingKg, status)
		}
		fmt.Printf("    %d essential, %d consumable\n\n", s.Essential, s.Consumable)
	}

	if len(report.Warnings) > 0 {
		fmt.Printf("%d warning(s):\n", len(report.Warnings))
		for _, w := range report.Warnings {
			fmt.Printf("  ! %s\n", w)
		}
		fmt.Println()
	}
	if len(report.Errors) > 0 {
		fmt.Printf("%d error(s):\n", len(report.Errors))
		for _, e := range report.Errors {
			fmt.Printf("  x %s\n", e)
		}
		return 1
	}
	fmt.Println("All templates valid.")
	return 0
}

// cmdExportJSON writes the full JSON export (inventory.schema.json) to a
// file or stdout. Sensitive fields are excluded by default per the threat
// model; -include-sensitive restores them for local backup, with a warning
// naming exactly what that includes. Documents of kind identity, financial,
// insurance, or medical never export under any flag.
func cmdExportJSON(args []string) int {
	fl := flag.NewFlagSet("export-json", flag.ExitOnError)
	dataDir := fl.String("data-dir", "data", "directory containing the SQLite database")
	out := fl.String("o", "", "output file (default stdout)")
	includeSensitive := fl.Bool("include-sensitive", false,
		"include serial numbers, costs, per-person needs, location notes, and documents regardless of export_ok (for local backup)")
	fl.Parse(args)

	db, err := store.Open(filepath.Join(*dataDir, "prepkit.db"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "prepkit: opening database: %v\n", err)
		return 1
	}
	defer db.Close()

	inv, err := db.Snapshot(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "prepkit: reading inventory: %v\n", err)
		return 1
	}
	if *includeSensitive {
		fmt.Fprintln(os.Stderr, "prepkit: WARNING: this export includes serial numbers, costs, per-person needs,")
		fmt.Fprintln(os.Stderr, "prepkit: location notes, and documents that did not opt in via export_ok.")
		fmt.Fprintln(os.Stderr, "prepkit: Treat the file as confidential. Identity, financial, insurance, and")
		fmt.Fprintln(os.Stderr, "prepkit: medical documents are never exported, even with this flag.")
	}
	b, err := export.JSON(*inv, time.Now(), export.Options{IncludeSensitive: *includeSensitive})
	if err != nil {
		fmt.Fprintf(os.Stderr, "prepkit: %v\n", err)
		return 1
	}
	if *out == "" {
		os.Stdout.Write(b)
		return 0
	}
	if err := os.WriteFile(*out, b, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "prepkit: writing %s: %v\n", *out, err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "prepkit: wrote %s\n", *out)
	return 0
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
