package cli

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/R0n1ns/gogateway/internal/config"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestRunHelpUnknownAndFlagErrors(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"-h"}, {"--help"}} {
		var out bytes.Buffer
		if code := Run(args, &out, io.Discard); code != 0 || !strings.Contains(out.String(), "GateForge") {
			t.Fatalf("args=%v code=%d out=%s", args, code, out.String())
		}
	}
	var stderr bytes.Buffer
	if code := Run([]string{"unknown"}, io.Discard, &stderr); code != 2 || !strings.Contains(stderr.String(), "unknown") {
		t.Fatalf("code=%d err=%s", code, stderr.String())
	}
	commands := [][]string{{"validate", "-bad"}, {"format", "-bad"}, {"simulate", "-bad"}, {"test", "-bad"}, {"run", "-bad"}}
	for _, args := range commands {
		if code := Run(args, io.Discard, io.Discard); code != 2 {
			t.Fatalf("args=%v code=%d", args, code)
		}
	}
}

func TestCLIFileAndJSONErrorBranches(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.json")
	for _, args := range [][]string{{"validate", "-config", missing}, {"format", "-config", missing}, {"test", "-config", missing}, {"run", "-config", missing}} {
		if code := Run(args, io.Discard, io.Discard); code != 1 {
			t.Fatalf("args=%v code=%d", args, code)
		}
	}
	invalid := filepath.Join(dir, "invalid.json")
	if err := os.WriteFile(invalid, []byte(`{`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := Run([]string{"format", "-config", invalid}, io.Discard, io.Discard); code != 1 {
		t.Fatalf("format invalid code=%d", code)
	}
	if code := Run([]string{"simulate", "-config", invalid, "-request", invalid}, io.Discard, io.Discard); code != 1 {
		t.Fatalf("simulate config code=%d", code)
	}
	if code := Run([]string{"simulate"}, io.Discard, io.Discard); code != 2 {
		t.Fatalf("simulate missing request code=%d", code)
	}
}

func TestFormatWriteAndSimulateBranches(t *testing.T) {
	root := repoRoot(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	source, err := os.ReadFile(filepath.Join(root, "configs", "gateforge.json"))
	if err != nil {
		t.Fatal(err)
	}
	compact := bytes.ReplaceAll(source, []byte("  "), nil)
	if err = os.WriteFile(configPath, compact, 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := Run([]string{"format", "-write", "-config", configPath}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "отформатирована") {
		t.Fatalf("code=%d out=%s err=%s", code, out.String(), errOut.String())
	}
	formatted, _ := os.ReadFile(configPath)
	if !bytes.Contains(formatted, []byte("\n  \"version\"")) {
		t.Fatalf("not formatted: %s", formatted)
	}
	if code := Run([]string{"simulate", "-config", configPath, "-request", filepath.Join(root, "configs", "simulate-request.json")}, &out, &errOut); code != 0 {
		t.Fatalf("simulate code=%d err=%s", code, errOut.String())
	}
	if code := Run([]string{"simulate", "-config", configPath, "-request", filepath.Join(dir, "missing-request.json")}, io.Discard, io.Discard); code != 1 {
		t.Fatalf("missing request code=%d", code)
	}
	badRequest := filepath.Join(dir, "bad-request.json")
	_ = os.WriteFile(badRequest, []byte(`{`), 0o600)
	if code := Run([]string{"simulate", "-config", configPath, "-request", badRequest}, io.Discard, io.Discard); code != 1 {
		t.Fatalf("bad request code=%d", code)
	}
}

func TestPolicyCasesFailureAndLoadErrors(t *testing.T) {
	root := repoRoot(t)
	dir := t.TempDir()
	cfg := filepath.Join(root, "configs", "gateforge.json")
	if code := Run([]string{"test", "-config", cfg, "-cases", filepath.Join(dir, "missing.json")}, io.Discard, io.Discard); code != 1 {
		t.Fatalf("missing cases code=%d", code)
	}
	invalid := filepath.Join(dir, "invalid-cases.json")
	_ = os.WriteFile(invalid, []byte(`{`), 0o600)
	if code := Run([]string{"test", "-config", cfg, "-cases", invalid}, io.Discard, io.Discard); code != 1 {
		t.Fatalf("invalid cases code=%d", code)
	}
	failed := filepath.Join(dir, "failed.json")
	_ = os.WriteFile(failed, []byte(`{"cases":[{"name":"wrong","request":{"path":"/api"},"expected":{"status":418}}]}`), 0o600)
	var out bytes.Buffer
	if code := Run([]string{"test", "-config", cfg, "-cases", failed}, &out, io.Discard); code != 1 || !strings.Contains(out.String(), "FAIL") {
		t.Fatalf("code=%d out=%s", code, out.String())
	}
}

func TestWriteJSONAndNewServer(t *testing.T) {
	var errOut bytes.Buffer
	if code := writeJSON(failingWriter{}, &errOut, map[string]string{"ok": "yes"}); code != 1 || !strings.Contains(errOut.String(), "write failed") {
		t.Fatalf("code=%d err=%s", code, errOut.String())
	}
	server := newServer(config.Server{Address: ":1234", ReadTimeout: config.Duration{}, WriteTimeout: config.Duration{}, IdleTimeout: config.Duration{}}, http.NotFoundHandler())
	if server.Addr != ":1234" || server.Handler == nil {
		t.Fatalf("server=%+v", server)
	}
}

func TestRunGatewayPostgresConnectionError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "postgres.json")
	document := `{"version":1,"public":{"address":":0"},"admin":{"address":"127.0.0.1:0"},"storage":{"driver":"postgres","dsn":"postgres://bad:bad@127.0.0.1:1/db?sslmode=disable&connect_timeout=1"},"routes":[],"policies":[]}`
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := Run([]string{"run", "-config", path}, io.Discard, io.Discard); code != 1 {
		t.Fatalf("code=%d", code)
	}
}
