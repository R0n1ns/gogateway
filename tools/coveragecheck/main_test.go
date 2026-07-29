package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseProfile(t *testing.T) {
	profile := "mode: set\nmodule/a.go:1.1,2.2 2 1\nmodule/a.go:3.1,4.2 3 0\n"
	result, err := parseProfile(strings.NewReader(profile))
	if err != nil {
		t.Fatalf("parseProfile() вернул ошибку: %v", err)
	}
	if result.statements != 5 || result.covered != 2 || result.percent() != 40 {
		t.Fatalf("получен неожиданный результат: %+v, percent=%.1f", result, result.percent())
	}
}

func TestParseProfileОшибки(t *testing.T) {
	tests := map[string]string{
		"пустой профиль":          "mode: set\n",
		"мало полей":              "broken\n",
		"число операторов":        "module/a.go:1.1,2.2 x 1\n",
		"отрицательные операторы": "module/a.go:1.1,2.2 -1 1\n",
		"счётчик":                 "module/a.go:1.1,2.2 1 x\n",
	}
	for name, profile := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := parseProfile(strings.NewReader(profile)); err == nil {
				t.Fatal("ожидалась ошибка")
			}
		})
	}
}

func TestRunПроверяетПорог(t *testing.T) {
	directory := t.TempDir()
	profilePath := filepath.Join(directory, "coverage.out")
	profile := []byte("mode: atomic\nmodule/a.go:1.1,2.2 8 1\nmodule/a.go:3.1,4.2 2 0\n")
	if err := os.WriteFile(profilePath, profile, 0o600); err != nil {
		t.Fatalf("записать профиль: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"-profile", profilePath, "-min", "80"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run() code=%d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "80.0%") {
		t.Fatalf("stdout=%q", stdout.String())
	}

	stderr.Reset()
	if code := run([]string{"-profile", profilePath, "-min", "81"}, &stdout, &stderr); code != 1 {
		t.Fatalf("run() code=%d, ожидался 1", code)
	}
	if !strings.Contains(stderr.String(), "ниже обязательного порога") {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

func TestRunОшибкиАргументовИФайла(t *testing.T) {
	tests := [][]string{
		{"-min", "101"},
		{"-min", "wrong"},
		{"-profile", filepath.Join(t.TempDir(), "missing.out")},
	}
	for _, args := range tests {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code == 0 {
			t.Fatalf("run(%v) завершился успешно", args)
		}
	}
}
