package cli

import (
	"bytes"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestValidateAndPolicyTests(t *testing.T) {
	root := repoRoot(t)
	var out, errOut bytes.Buffer
	if code := Run([]string{"validate", "-config", filepath.Join(root, "configs", "gateforge.json")}, &out, &errOut); code != 0 {
		t.Fatalf("validate code=%d err=%s", code, errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := Run([]string{"test", "-config", filepath.Join(root, "configs", "gateforge.json"), "-cases", filepath.Join(root, "configs", "policy-tests.json")}, &out, &errOut); code != 0 || strings.Count(out.String(), "PASS") != 2 {
		t.Fatalf("test code=%d out=%s err=%s", code, out.String(), errOut.String())
	}
}
func TestFormatProducesReloadableJSON(t *testing.T) {
	root := repoRoot(t)
	var out, errOut bytes.Buffer
	if code := Run([]string{"format", "-config", filepath.Join(root, "configs", "gateforge.json")}, &out, &errOut); code != 0 {
		t.Fatalf("format code=%d err=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "\n  \"version\"") {
		t.Fatalf("не форматировано: %s", out.String())
	}
}
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
