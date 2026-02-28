package main

import (
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
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
	if len(args) == 0 || args[0] != "start" {
		return errors.New("usage: deck agent start --server <url> [--interval <duration>] [--once]")
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

	if err := agent.Run(agent.RunOptions{ServerURL: *serverURL, Interval: interval, Once: *once}); err != nil {
		return err
	}

	if *once {
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
		return errors.New("usage: deck server start --root <dir> --addr <host:port> [--tls-cert <crt> --tls-key <key> | --tls-self-signed]")
	}

	fs := flag.NewFlagSet("server start", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	root := fs.String("root", "./bundle", "server content root")
	addr := fs.String("addr", ":8080", "server listen address")
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

	certPath := *tlsCert
	keyPath := *tlsKey
	if *tlsSelfSigned {
		var err error
		certPath, keyPath, err = server.EnsureSelfSignedTLS(*root, *addr)
		if err != nil {
			return err
		}
	}

	h, err := server.NewHandler(*root)
	if err != nil {
		return err
	}
	if certPath != "" {
		fmt.Fprintf(os.Stdout, "server start: listening on https://%s (root=%s)\n", *addr, *root)
		return http.ListenAndServeTLS(*addr, certPath, keyPath, h)
	}

	fmt.Fprintf(os.Stdout, "server start: listening on http://%s (root=%s)\n", *addr, *root)
	return http.ListenAndServe(*addr, h)
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

	file := fs.String("f", "", "path to workflow file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *file == "" {
		return errors.New("-f is required")
	}

	if err := validate.File(*file); err != nil {
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

	report, err := diagnose.Preflight(wf, diagnose.RunOptions{WorkflowPath: *file, BundleRoot: *bundle, OutputPath: *out})
	if err != nil {
		fmt.Fprintf(os.Stdout, "diagnose preflight: failed (%d failed checks)\n", report.Summary.Failed)
		return err
	}

	fmt.Fprintf(os.Stdout, "diagnose preflight: ok (%d checks)\n", report.Summary.Passed)
	fmt.Fprintf(os.Stdout, "diagnose report: %s\n", *out)
	return nil
}

func usageError() error {
	return errors.New("usage: deck apply --file <file> | deck validate -f <file> | deck run --file <file> --phase <phase> | deck resume --file <file> | deck diagnose --preflight --file <file> | deck bundle verify --bundle <path> | deck bundle import --file <bundle.tar> --dest <dir> | deck bundle collect --bundle <dir> --output <bundle.tar> | deck server start --root <dir> --addr <host:port> | deck agent start --server <url>")
}
