// Package config описывает версионируемую конфигурацию data plane и control plane.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
)

const (
	ModeBlock  = "block"
	ModeShadow = "shadow"

	ActionAllow        = "allow"
	ActionDeny         = "deny"
	ActionRateLimit    = "rate_limit"
	ActionRewrite      = "rewrite"
	ActionRedirect     = "redirect"
	ActionAuthenticate = "authenticate"
)

var environmentPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Load читает JSON, подставляет переменные окружения и полностью проверяет
// конфигурацию до её передачи runtime.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("прочитать конфигурацию: %w", err)
	}
	cfg, err := Decode(data)
	if err != nil {
		return nil, fmt.Errorf("разобрать конфигурацию: %w", err)
	}
	return cfg, nil
}

// Decode разбирает ровно один JSON-документ. Неизвестные поля запрещены, чтобы
// опечатка не превращалась в тихо проигнорированную настройку безопасности.
func Decode(data []byte) (*Config, error) {
	var cfg Config
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return nil, err
	}
	cfg.Defaults()
	cfg.ExpandEnvironment()
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}
