// Package cli реализует команды gateforge и отделяет их от main.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/R0n1ns/gogateway/internal/admin"
	"github.com/R0n1ns/gogateway/internal/config"
	"github.com/R0n1ns/gogateway/internal/control"
	"github.com/R0n1ns/gogateway/internal/gateway"
	"github.com/R0n1ns/gogateway/internal/observability"
	"github.com/R0n1ns/gogateway/internal/policytest"
	"github.com/R0n1ns/gogateway/internal/simulator"
	"github.com/R0n1ns/gogateway/internal/storage"
)

func Run(args []string, stdout, stderr io.Writer) int {
	command := "run"
	if len(args) > 0 {
		switch args[0] {
		case "help", "-h", "--help":
			printHelp(stdout)
			return 0
		}
		if args[0][0] != '-' {
			command = args[0]
			args = args[1:]
		}
	}
	switch command {
	case "run":
		return runGateway(args, stdout, stderr)
	case "validate":
		return validate(args, stdout, stderr)
	case "format":
		return format(args, stdout, stderr)
	case "simulate":
		return simulate(args, stdout, stderr)
	case "test":
		return testPolicies(args, stdout, stderr)
	case "help", "-h", "--help":
		printHelp(stdout)
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "неизвестная команда %q\n", command)
		printHelp(stderr)
		return 2
	}
}
func printHelp(w io.Writer) {
	_, _ = fmt.Fprintln(w, "GateForge — Application Gateway with Policy Engine\n\nКоманды:\n  run       запустить public и admin серверы\n  validate  проверить конфигурацию\n  format    вывести или перезаписать форматированный JSON\n  simulate  выполнить один запрос в Policy Simulator\n  test      запустить декларативные policy cases")
}
func configFlag(set *flag.FlagSet) *string {
	return set.String("config", "configs/gateforge.json", "путь к JSON-конфигурации")
}
func validate(args []string, out, errOut io.Writer) int {
	set := flag.NewFlagSet("validate", flag.ContinueOnError)
	set.SetOutput(errOut)
	path := configFlag(set)
	if set.Parse(args) != nil {
		return 2
	}
	cfg, err := config.Load(*path)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, err)
		return 1
	}
	_, _ = fmt.Fprintf(out, "конфигурация корректна: routes=%d policies=%d\n", len(cfg.Routes), len(cfg.Policies))
	return 0
}
func format(args []string, out, errOut io.Writer) int {
	set := flag.NewFlagSet("format", flag.ContinueOnError)
	set.SetOutput(errOut)
	path := configFlag(set)
	write := set.Bool("write", false, "перезаписать файл")
	if set.Parse(args) != nil {
		return 2
	}
	data, err := os.ReadFile(*path)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, err)
		return 1
	}
	if _, err = config.Decode(data); err != nil {
		_, _ = fmt.Fprintln(errOut, err)
		return 1
	}
	var formatted bytes.Buffer
	if err = json.Indent(&formatted, data, "", "  "); err != nil {
		_, _ = fmt.Fprintln(errOut, err)
		return 1
	}
	formatted.WriteByte('\n')
	if *write {
		if err = os.WriteFile(*path, formatted.Bytes(), 0o600); err != nil {
			_, _ = fmt.Fprintln(errOut, err)
			return 1
		}
		_, _ = fmt.Fprintln(out, "конфигурация отформатирована")
		return 0
	}
	_, _ = out.Write(formatted.Bytes())
	return 0
}
func simulate(args []string, out, errOut io.Writer) int {
	set := flag.NewFlagSet("simulate", flag.ContinueOnError)
	set.SetOutput(errOut)
	path := configFlag(set)
	requestPath := set.String("request", "", "JSON-файл simulated request")
	if set.Parse(args) != nil {
		return 2
	}
	if *requestPath == "" {
		_, _ = fmt.Fprintln(errOut, "-request обязателен")
		return 2
	}
	cfg, err := config.Load(*path)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, err)
		return 1
	}
	data, err := os.ReadFile(*requestPath)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, err)
		return 1
	}
	var input simulator.Request
	if err = json.Unmarshal(data, &input); err != nil {
		_, _ = fmt.Fprintln(errOut, err)
		return 1
	}
	service, err := simulator.New(cfg)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, err)
		return 1
	}
	return writeJSON(out, errOut, service.Simulate(input))
}
func testPolicies(args []string, out, errOut io.Writer) int {
	set := flag.NewFlagSet("test", flag.ContinueOnError)
	set.SetOutput(errOut)
	path := configFlag(set)
	casesPath := set.String("cases", "configs/policy-tests.json", "путь к policy cases")
	if set.Parse(args) != nil {
		return 2
	}
	cfg, err := config.Load(*path)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, err)
		return 1
	}
	file, err := policytest.Load(*casesPath)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, err)
		return 1
	}
	results, err := policytest.Run(cfg, file)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, err)
		return 1
	}
	failed := false
	for _, result := range results {
		state := "PASS"
		if !result.Passed {
			state = "FAIL"
			failed = true
		}
		_, _ = fmt.Fprintf(out, "%s %s", state, result.Name)
		if result.Message != "" {
			_, _ = fmt.Fprintf(out, ": %s", result.Message)
		}
		_, _ = fmt.Fprintln(out)
	}
	if failed {
		return 1
	}
	return 0
}
func writeJSON(out, errOut io.Writer, value any) int {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		_, _ = fmt.Fprintln(errOut, err)
		return 1
	}
	return 0
}
func runGateway(args []string, out, errOut io.Writer) int {
	set := flag.NewFlagSet("run", flag.ContinueOnError)
	set.SetOutput(errOut)
	path := configFlag(set)
	if set.Parse(args) != nil {
		return 2
	}
	fileConfig, err := config.Load(*path)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, err)
		return 1
	}
	repository, err := storage.Open(context.Background(), fileConfig)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, err)
		return 1
	}
	snapshot, err := repository.Current(context.Background())
	if err != nil {
		_ = repository.Close()
		_, _ = fmt.Fprintln(errOut, err)
		return 1
	}
	cfg := snapshot.Config
	logger := slog.New(slog.NewJSONHandler(out, nil))
	tracer := gateway.NewW3CTracer()
	var telemetry *observability.Provider
	if cfg.Observability.TracingEnabled && cfg.Observability.OTLPEndpoint != "" {
		telemetry, err = observability.NewOTLP(context.Background(), cfg.Observability)
		if err != nil {
			_ = repository.Close()
			_, _ = fmt.Fprintln(errOut, err)
			return 1
		}
		tracer = telemetry.Tracer
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = telemetry.Shutdown(shutdownCtx)
		}()
	}
	runtime, err := gateway.NewWithTracer(cfg, logger, tracer)
	if err != nil {
		_ = repository.Close()
		_, _ = fmt.Fprintln(errOut, err)
		return 1
	}
	service := control.New(runtime, repository)
	defer func() { _ = service.Close() }()
	defer runtime.Close()
	activeConfig := runtime.Config()
	public := newServer(activeConfig.Public, runtime)
	controlServer := newServer(activeConfig.Admin.Server, admin.NewWithControl(runtime, service, activeConfig.Admin.APIKey))
	errs := make(chan error, 2)
	go func() { logger.Info("public server started", "address", public.Addr); errs <- public.ListenAndServe() }()
	go func() {
		logger.Info("admin server started", "address", controlServer.Addr)
		errs <- controlServer.ListenAndServe()
	}()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	exitCode := 0
	select {
	case <-signals:
	case serveErr := <-errs:
		if !errors.Is(serveErr, http.ErrServerClosed) {
			logger.Error("server stopped", "error", serveErr)
			exitCode = 1
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), activeConfig.Runtime.ShutdownTimeout.Duration)
	defer cancel()
	_ = public.Shutdown(ctx)
	_ = controlServer.Shutdown(ctx)
	return exitCode
}
func newServer(cfg config.Server, handler http.Handler) *http.Server {
	return &http.Server{Addr: cfg.Address, Handler: handler, ReadTimeout: cfg.ReadTimeout.Duration, WriteTimeout: cfg.WriteTimeout.Duration, IdleTimeout: cfg.IdleTimeout.Duration, ReadHeaderTimeout: 5_000_000_000}
}
