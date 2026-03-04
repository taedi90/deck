package main

import (
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/taedi90/deck/internal/agent"
	"github.com/taedi90/deck/internal/bundle"
	"github.com/taedi90/deck/internal/config"
	"github.com/taedi90/deck/internal/diagnose"
	"github.com/taedi90/deck/internal/install"
	"github.com/taedi90/deck/internal/prepare"
	"github.com/taedi90/deck/internal/server"
	"github.com/taedi90/deck/internal/validate"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "deck: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usageError()
	}

	switch args[0] {
	case "apply":
		return runApply(args[1:])
	case "validate":
		return runValidate(args[1:])
	case "run":
		return runRun(args[1:])
	case "resume":
		return runResume(args[1:])
	case "diagnose":
		return runDiagnose(args[1:])
	case "bundle":
		return runBundle(args[1:])
	case "server":
		return runServer(args[1:])
	case "agent":
		return runAgent(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runAgent(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: deck agent start --server <url> [--interval <duration>] [--once] | deck agent run-once --server <url>")
	}

	mode := args[0]
	if mode != "start" && mode != "run-once" {
		return errors.New("usage: deck agent start --server <url> [--interval <duration>] [--once] | deck agent run-once --server <url>")
	}

	fs := flag.NewFlagSet("agent start", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	serverURL := fs.String("server", "", "agent control server url")
	intervalRaw := fs.String("interval", "10s", "heartbeat interval")
	once := fs.Bool("once", false, "send one heartbeat and exit")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *serverURL == "" {
		return errors.New("--server is required")
	}

	interval, err := time.ParseDuration(*intervalRaw)
	if err != nil {
		return fmt.Errorf("invalid --interval: %w", err)
	}

	runOnce := *once || mode == "run-once"
	if err := agent.Run(agent.RunOptions{ServerURL: *serverURL, Interval: interval, Once: runOnce}); err != nil {
		return err
	}

	if runOnce {
		fmt.Fprintln(os.Stdout, "agent start: heartbeat sent")
	}
	return nil
}

func runApply(args []string) error {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	file := fs.String("file", "", "path to workflow file")
	bundlePath := fs.String("bundle", "", "bundle path")
	skipPreflight := fs.Bool("skip-preflight", false, "skip preflight checks")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" {
		return errors.New("--file is required")
	}

	if err := validate.File(*file); err != nil {
		return err
	}

	wf, err := config.Load(*file)
	if err != nil {
		return err
	}

	bundleRoot := *bundlePath
	if bundleRoot == "" {
		bundleRoot = wf.Context.BundleRoot
	}

	if err := prepare.Run(wf, prepare.RunOptions{BundleRoot: bundleRoot}); err != nil {
		return err
	}

	if !*skipPreflight {
		if _, err := diagnose.Preflight(wf, diagnose.RunOptions{WorkflowPath: *file, BundleRoot: bundleRoot}); err != nil {
			return err
		}
	}

	if err := install.Run(wf, install.RunOptions{BundleRoot: bundleRoot}); err != nil {
		return err
	}

	fmt.Fprintln(os.Stdout, "apply: ok")
	return nil
}

func runServer(args []string) error {
	if len(args) == 0 || args[0] != "start" {
		return errors.New("usage: deck server start --root <dir> --addr <host:port> [--report-max <n>] [--registry-seed-dir <dir>] [--registry-seed-registries <csv>] [--tls-cert <crt> --tls-key <key> | --tls-self-signed]")
	}

	fs := flag.NewFlagSet("server start", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	root := fs.String("root", "./bundle", "server content root")
	addr := fs.String("addr", ":8080", "server listen address")
	reportMax := fs.Int("report-max", 200, "max retained in-memory reports")
	registryEnable := fs.Bool("registry-enable", true, "enable embedded registry at /v2")
	registryRoot := fs.String("registry-root", "", "embedded registry storage root (default: <root>/.deck/registry)")
	registrySeedDir := fs.String("registry-seed-dir", "", "directory with docker-archive .tar files for registry seeding")
	registrySeedRegistries := fs.String("registry-seed-registries", "registry.k8s.io,docker.io,quay.io,ghcr.io,gcr.io,k8s.gcr.io", "comma-separated source registries whose domain is stripped when seeding")
	tlsCert := fs.String("tls-cert", "", "TLS certificate path")
	tlsKey := fs.String("tls-key", "", "TLS private key path")
	tlsSelfSigned := fs.Bool("tls-self-signed", false, "auto-generate and use self-signed TLS cert")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	if (*tlsCert == "") != (*tlsKey == "") {
		return errors.New("--tls-cert and --tls-key must be provided together")
	}
	if *tlsSelfSigned && (*tlsCert != "" || *tlsKey != "") {
		return errors.New("--tls-self-signed cannot be combined with --tls-cert/--tls-key")
	}
	if *reportMax <= 0 {
		return errors.New("--report-max must be > 0")
	}
	if strings.TrimSpace(*registrySeedDir) == "" {
		*registrySeedDir = filepath.Join(*root, "images")
	}

	certPath := *tlsCert
	keyPath := *tlsKey
	if *tlsSelfSigned {
		var err error
		certPath, keyPath, err = server.EnsureSelfSignedTLS(*root, *addr)
		if err != nil {
			return err
		}
	}

	effectiveRegistryRoot := strings.TrimSpace(*registryRoot)
	if effectiveRegistryRoot == "" {
		effectiveRegistryRoot = server.DefaultRegistryRoot(*root)
	}

	var (
		registryHandler http.Handler
		err             error
	)
	if *registryEnable {
		registryHandler, err = server.NewRegistryHandler(effectiveRegistryRoot)
		if err != nil {
			return err
		}
		auditLogPath := filepath.Join(*root, ".deck", "logs", "server-audit.log")
		if err := server.SeedRegistryFromDir(registryHandler, server.RegistrySeedOptions{SeedDir: *registrySeedDir, StripRegistries: strings.Split(*registrySeedRegistries, ","), AuditLogPath: auditLogPath}); err != nil {
			return err
		}
	}

	h, err := server.NewHandler(*root, server.HandlerOptions{ReportMax: *reportMax, RegistryEnable: *registryEnable, RegistryRoot: effectiveRegistryRoot, RegistryHandler: registryHandler})
	if err != nil {
		return err
	}
	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	if certPath != "" {
		fmt.Fprintf(os.Stdout, "server start: listening on https://%s (root=%s)\n", *addr, *root)
		return httpServer.ListenAndServeTLS(certPath, keyPath)
	}

	fmt.Fprintf(os.Stdout, "server start: listening on http://%s (root=%s)\n", *addr, *root)
	return httpServer.ListenAndServe()
}

func runBundle(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: deck bundle verify --bundle <path> | deck bundle import --file <bundle.tar> --dest <dir> | deck bundle collect --bundle <dir> --output <bundle.tar>")
	}

	switch args[0] {
	case "verify":
		fs := flag.NewFlagSet("bundle verify", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		bundlePath := fs.String("bundle", "", "bundle path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *bundlePath == "" {
			return errors.New("--bundle is required")
		}

		if err := bundle.VerifyManifest(*bundlePath); err != nil {
			return err
		}

		fmt.Fprintf(os.Stdout, "bundle verify: ok (%s)\n", *bundlePath)
		return nil

	case "import":
		fs := flag.NewFlagSet("bundle import", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		archiveFile := fs.String("file", "", "bundle archive file path")
		destDir := fs.String("dest", "", "destination directory")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *archiveFile == "" {
			return errors.New("--file is required")
		}
		if *destDir == "" {
			return errors.New("--dest is required")
		}

		if err := bundle.ImportArchive(*archiveFile, *destDir); err != nil {
			return err
		}

		fmt.Fprintf(os.Stdout, "bundle import: ok (%s -> %s)\n", *archiveFile, *destDir)
		return nil

	case "collect":
		fs := flag.NewFlagSet("bundle collect", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		bundleDir := fs.String("bundle", "", "bundle directory")
		outputFile := fs.String("output", "", "output tar archive path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *bundleDir == "" {
			return errors.New("--bundle is required")
		}
		if *outputFile == "" {
			return errors.New("--output is required")
		}

		if err := bundle.CollectArchive(*bundleDir, *outputFile); err != nil {
			return err
		}

		fmt.Fprintf(os.Stdout, "bundle collect: ok (%s -> %s)\n", *bundleDir, *outputFile)
		return nil

	default:
		return fmt.Errorf("unknown bundle command %q", args[0])
	}
}

func runValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var file string
	fs.StringVar(&file, "f", "", "path to workflow file")
	fs.StringVar(&file, "file", "", "path to workflow file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if file == "" {
		return errors.New("--file (or -f) is required")
	}

	if err := validate.File(file); err != nil {
		return err
	}

	fmt.Fprintln(os.Stdout, "validate: ok")
	return nil
}

func runRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	file := fs.String("file", "", "path to workflow file")
	phase := fs.String("phase", "", "phase to execute (prepare/install)")
	bundle := fs.String("bundle", "", "bundle output path")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *file == "" {
		return errors.New("--file is required")
	}
	if *phase == "" {
		return errors.New("--phase is required")
	}

	if err := validate.File(*file); err != nil {
		return err
	}

	wf, err := config.Load(*file)
	if err != nil {
		return err
	}

	switch *phase {
	case "prepare":
		if err := prepare.Run(wf, prepare.RunOptions{BundleRoot: *bundle}); err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, "run prepare: ok")
		return nil
	case "install":
		if err := install.Run(wf, install.RunOptions{BundleRoot: *bundle}); err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, "run install: ok")
		return nil
	default:
		return fmt.Errorf("unsupported phase: %s", *phase)
	}
}

func runResume(args []string) error {
	fs := flag.NewFlagSet("resume", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	file := fs.String("file", "", "path to workflow file")
	bundle := fs.String("bundle", "", "bundle path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" {
		return errors.New("--file is required")
	}

	if err := validate.File(*file); err != nil {
		return err
	}

	wf, err := config.Load(*file)
	if err != nil {
		return err
	}
	if err := install.Run(wf, install.RunOptions{BundleRoot: *bundle}); err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, "resume install: ok")
	return nil
}

func runDiagnose(args []string) error {
	fs := flag.NewFlagSet("diagnose", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	file := fs.String("file", "", "path to workflow file")
	bundle := fs.String("bundle", "", "bundle path")
	preflight := fs.Bool("preflight", false, "run preflight checks")
	out := fs.String("out", "reports/diagnose.json", "diagnose report output path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" {
		return errors.New("--file is required")
	}
	if !*preflight {
		return errors.New("only --preflight mode is supported")
	}

	if err := validate.File(*file); err != nil {
		return err
	}

	wf, err := config.Load(*file)
	if err != nil {
		return err
	}

	report, err := diagnose.Preflight(wf, diagnose.RunOptions{WorkflowPath: *file, BundleRoot: *bundle, OutputPath: *out, EnforceHostChecks: true})
	if err != nil {
		fmt.Fprintf(os.Stdout, "diagnose preflight: failed (%d failed checks)\n", report.Summary.Failed)
		return err
	}

	fmt.Fprintf(os.Stdout, "diagnose preflight: ok (%d checks)\n", report.Summary.Passed)
	fmt.Fprintf(os.Stdout, "diagnose report: %s\n", *out)
	return nil
}

func usageError() error {
	return errors.New("usage: deck apply --file <file> | deck validate --file <file> (-f alias) | deck run --file <file> --phase <phase> | deck resume --file <file> | deck diagnose --preflight --file <file> | deck bundle verify --bundle <path> | deck bundle import --file <bundle.tar> --dest <dir> | deck bundle collect --bundle <dir> --output <bundle.tar> | deck server start --root <dir> --addr <host:port> [--registry-seed-dir <dir>] [--registry-seed-registries <csv>] | deck agent start --server <url> | deck agent run-once --server <url>")
}
