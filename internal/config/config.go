// Package config описывает конфигурацию GateForge.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"
)

const (
	ModeBlock       = "block"
	ModeShadow      = "shadow"
	ActionAllow     = "allow"
	ActionDeny      = "deny"
	ActionRateLimit = "rate_limit"
)

type Duration struct{ time.Duration }

func (d *Duration) UnmarshalJSON(data []byte) error {
	var v string
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	x, err := time.ParseDuration(v)
	if err != nil {
		return err
	}
	if x <= 0 {
		return errors.New("длительность должна быть больше нуля")
	}
	d.Duration = x
	return nil
}
func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

type Config struct {
	Version   int         `json:"version"`
	Public    Server      `json:"public"`
	Admin     AdminServer `json:"admin"`
	Runtime   Runtime     `json:"runtime"`
	Storage   Storage     `json:"storage"`
	Routes    []Route     `json:"routes"`
	Policies  []Policy    `json:"policies"`
	AccessLog AccessLog   `json:"access_log"`
}
type Server struct {
	Address      string   `json:"address"`
	ReadTimeout  Duration `json:"read_timeout"`
	WriteTimeout Duration `json:"write_timeout"`
	IdleTimeout  Duration `json:"idle_timeout"`
}
type AdminServer struct {
	Server
	APIKey string `json:"api_key"`
}
type Runtime struct {
	ShutdownTimeout Duration `json:"shutdown_timeout"`
}
type Storage struct {
	Driver string `json:"driver"`
	DSN    string `json:"dsn"`
}
type AccessLog struct {
	Enabled bool   `json:"enabled"`
	Format  string `json:"format"`
}
type Route struct {
	ID              string            `json:"id"`
	Host            string            `json:"host"`
	PathPrefix      string            `json:"path_prefix"`
	Upstreams       []string          `json:"upstreams"`
	MaxBodyBytes    int64             `json:"max_body_bytes"`
	StripPrefix     bool              `json:"strip_prefix"`
	AddHeaders      map[string]string `json:"add_headers"`
	RemoveHeaders   []string          `json:"remove_headers"`
	UpstreamTimeout Duration          `json:"upstream_timeout"`
}
type Policy struct {
	ID       string `json:"id"`
	Priority int    `json:"priority"`
	Mode     string `json:"mode"`
	Action   string `json:"action"`
	Match    Match  `json:"match"`
	Limit    *Limit `json:"limit,omitempty"`
}
type Match struct {
	Methods      []string          `json:"methods"`
	PathPrefix   string            `json:"path_prefix"`
	SourceCIDRs  []string          `json:"source_cidrs"`
	Headers      map[string]string `json:"headers"`
	Query        map[string]string `json:"query"`
	MinBodyBytes int64             `json:"min_body_bytes"`
	MaxBodyBytes int64             `json:"max_body_bytes"`
	RouteIDs     []string          `json:"route_ids"`
}
type Limit struct {
	Requests int      `json:"requests"`
	Window   Duration `json:"window"`
	Burst    int      `json:"burst"`
	Key      string   `json:"key"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("прочитать конфигурацию: %w", err)
	}
	var c Config
	if err = json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("разобрать конфигурацию: %w", err)
	}
	c.Defaults()
	if err = c.Validate(); err != nil {
		return nil, err
	}
	c.Normalize()
	return &c, nil
}
func (c *Config) Defaults() {
	if c.Public.ReadTimeout.Duration == 0 {
		c.Public.ReadTimeout.Duration = 10 * time.Second
	}
	if c.Public.WriteTimeout.Duration == 0 {
		c.Public.WriteTimeout.Duration = 30 * time.Second
	}
	if c.Public.IdleTimeout.Duration == 0 {
		c.Public.IdleTimeout.Duration = time.Minute
	}
	if c.Admin.ReadTimeout.Duration == 0 {
		c.Admin.ReadTimeout.Duration = 5 * time.Second
	}
	if c.Admin.WriteTimeout.Duration == 0 {
		c.Admin.WriteTimeout.Duration = 10 * time.Second
	}
	if c.Admin.IdleTimeout.Duration == 0 {
		c.Admin.IdleTimeout.Duration = 30 * time.Second
	}
	if c.Runtime.ShutdownTimeout.Duration == 0 {
		c.Runtime.ShutdownTimeout.Duration = 10 * time.Second
	}
	if c.AccessLog.Format == "" {
		c.AccessLog.Format = "json"
	}
	if c.Storage.Driver == "" {
		c.Storage.Driver = "memory"
	}
	for i := range c.Routes {
		if c.Routes[i].UpstreamTimeout.Duration == 0 {
			c.Routes[i].UpstreamTimeout.Duration = 30 * time.Second
		}
	}
	for i := range c.Policies {
		if c.Policies[i].Limit != nil && c.Policies[i].Limit.Window.Duration == 0 {
			c.Policies[i].Limit.Window.Duration = time.Minute
		}
	}
}
func (c *Config) Normalize() {
	for i := range c.Routes {
		c.Routes[i].Host = strings.ToLower(c.Routes[i].Host)
		if c.Routes[i].PathPrefix == "" {
			c.Routes[i].PathPrefix = "/"
		}
	}
	for i := range c.Policies {
		if c.Policies[i].Mode == "" {
			c.Policies[i].Mode = ModeBlock
		}
		for j := range c.Policies[i].Match.Methods {
			c.Policies[i].Match.Methods[j] = strings.ToUpper(c.Policies[i].Match.Methods[j])
		}
	}
	slices.SortStableFunc(c.Policies, func(a, b Policy) int { return a.Priority - b.Priority })
}
func (c *Config) Validate() error {
	var es []error
	if c.Version < 1 {
		es = append(es, errors.New("version должна быть не меньше 1"))
	}
	if c.Public.Address == "" {
		es = append(es, errors.New("public.address обязателен"))
	}
	if c.Admin.Address == "" {
		es = append(es, errors.New("admin.address обязателен"))
	}
	if c.Storage.Driver != "" && c.Storage.Driver != "memory" && c.Storage.Driver != "postgres" {
		es = append(es, errors.New("storage.driver должен быть memory или postgres"))
	}
	ids := map[string]bool{}
	for i, r := range c.Routes {
		p := fmt.Sprintf("routes[%d]", i)
		if r.ID == "" {
			es = append(es, fmt.Errorf("%s.id обязателен", p))
		}
		if ids[r.ID] {
			es = append(es, fmt.Errorf("%s.id %q повторяется", p, r.ID))
		}
		ids[r.ID] = true
		if r.PathPrefix != "" && !strings.HasPrefix(r.PathPrefix, "/") {
			es = append(es, fmt.Errorf("%s.path_prefix должен начинаться с /", p))
		}
		if len(r.Upstreams) == 0 {
			es = append(es, fmt.Errorf("%s.upstreams не должен быть пустым", p))
		}
		for _, v := range r.Upstreams {
			u, e := url.Parse(v)
			if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
				es = append(es, fmt.Errorf("%s содержит некорректный upstream %q", p, v))
			}
		}
		if r.MaxBodyBytes < 0 {
			es = append(es, fmt.Errorf("%s.max_body_bytes не может быть отрицательным", p))
		}
	}
	ids = map[string]bool{}
	for i, r := range c.Policies {
		p := fmt.Sprintf("policies[%d]", i)
		if r.ID == "" {
			es = append(es, fmt.Errorf("%s.id обязателен", p))
		}
		if ids[r.ID] {
			es = append(es, fmt.Errorf("%s.id %q повторяется", p, r.ID))
		}
		ids[r.ID] = true
		if r.Mode != "" && r.Mode != ModeBlock && r.Mode != ModeShadow {
			es = append(es, fmt.Errorf("%s.mode должен быть block или shadow", p))
		}
		if r.Action != ActionAllow && r.Action != ActionDeny && r.Action != ActionRateLimit {
			es = append(es, fmt.Errorf("%s.action не поддерживается", p))
		}
		if r.Action == ActionRateLimit && (r.Limit == nil || r.Limit.Requests <= 0) {
			es = append(es, fmt.Errorf("%s.limit.requests должен быть больше 0", p))
		}
		for _, v := range r.Match.SourceCIDRs {
			if _, _, e := net.ParseCIDR(v); e != nil {
				es = append(es, fmt.Errorf("%s содержит некорректный CIDR %q", p, v))
			}
		}
	}
	return errors.Join(es...)
}
