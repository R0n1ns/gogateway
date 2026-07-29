// Команда coveragecheck проверяет, что покрытие unit-тестами не опустилось ниже заданного порога.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type coverage struct {
	statements int
	covered    int
}

func (c coverage) percent() float64 {
	if c.statements == 0 {
		return 0
	}
	return float64(c.covered) * 100 / float64(c.statements)
}

// parseProfile суммирует операторы из стандартного cover profile Go.
func parseProfile(reader io.Reader) (coverage, error) {
	scanner := bufio.NewScanner(reader)
	result := coverage{}
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 3 {
			return coverage{}, fmt.Errorf("строка %d: неверный формат профиля", lineNumber)
		}
		statements, err := strconv.Atoi(fields[len(fields)-2])
		if err != nil || statements < 0 {
			return coverage{}, fmt.Errorf("строка %d: неверное число операторов", lineNumber)
		}
		count, err := strconv.ParseUint(fields[len(fields)-1], 10, 64)
		if err != nil {
			return coverage{}, fmt.Errorf("строка %d: неверный счётчик покрытия", lineNumber)
		}

		result.statements += statements
		if count > 0 {
			result.covered += statements
		}
	}
	if err := scanner.Err(); err != nil {
		return coverage{}, fmt.Errorf("прочитать профиль: %w", err)
	}
	if result.statements == 0 {
		return coverage{}, errors.New("профиль не содержит операторов")
	}
	return result, nil
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("coveragecheck", flag.ContinueOnError)
	flags.SetOutput(stderr)
	profilePath := flags.String("profile", "coverage.out", "путь к профилю покрытия")
	minimum := flags.Float64("min", 80, "минимальное покрытие в процентах")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *minimum < 0 || *minimum > 100 {
		_, _ = fmt.Fprintln(stderr, "порог покрытия должен находиться в диапазоне от 0 до 100")
		return 2
	}

	file, err := os.Open(*profilePath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "открыть профиль покрытия: %v\n", err)
		return 1
	}
	defer func() {
		_ = file.Close()
	}()

	result, err := parseProfile(file)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	actual := result.percent()
	if _, err := fmt.Fprintf(stdout, "Покрытие unit-тестами: %.1f%% (минимум %.1f%%)\n", actual, *minimum); err != nil {
		return 1
	}
	if actual < *minimum {
		_, _ = fmt.Fprintf(stderr, "покрытие %.1f%% ниже обязательного порога %.1f%%\n", actual, *minimum)
		return 1
	}
	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
