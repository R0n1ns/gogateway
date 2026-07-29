package config

// Config — единый документ конфигурации GateForge.
type Config struct {
	Schema        string        `json:"$schema,omitempty"`
	Version       int           `json:"version"`
	Revision      int64         `json:"revision,omitempty"`
	Public        Server        `json:"public"`
	Admin         AdminServer   `json:"admin"`
	Runtime       Runtime       `json:"runtime"`
	Storage       Storage       `json:"storage"`
	Observability Observability `json:"observability,omitempty"`
	Routes        []Route       `json:"routes"`
	Policies      []Policy      `json:"policies"`
	AccessLog     AccessLog     `json:"access_log"`
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
	TrustedProxies  []string `json:"trusted_proxies,omitempty"`
	MaxConfigBytes  int64    `json:"max_config_bytes,omitempty"`
}

type Storage struct {
	Driver       string `json:"driver"`
	DSN          string `json:"dsn,omitempty"`
	MaxOpenConns int    `json:"max_open_conns,omitempty"`
	MaxIdleConns int    `json:"max_idle_conns,omitempty"`
}

type Observability struct {
	ServiceName    string  `json:"service_name,omitempty"`
	TracingEnabled bool    `json:"tracing_enabled,omitempty"`
	SampleRate     float64 `json:"sample_rate,omitempty"`
	OTLPEndpoint   string  `json:"otlp_endpoint,omitempty"`
	OTLPInsecure   bool    `json:"otlp_insecure,omitempty"`
}

type AccessLog struct {
	Enabled bool   `json:"enabled"`
	Format  string `json:"format"`
}

// Route описывает virtual host, преобразования и пул upstream.
type Route struct {
	ID                 string            `json:"id"`
	Host               string            `json:"host"`
	PathPrefix         string            `json:"path_prefix"`
	Upstreams          []string          `json:"upstreams,omitempty"`
	UpstreamPool       []Upstream        `json:"upstream_pool,omitempty"`
	Balance            Balance           `json:"balance,omitempty"`
	HealthCheck        HealthCheck       `json:"health_check,omitempty"`
	Retry              Retry             `json:"retry,omitempty"`
	CircuitBreaker     CircuitBreaker    `json:"circuit_breaker,omitempty"`
	MaxBodyBytes       int64             `json:"max_body_bytes"`
	MaxConcurrent      int               `json:"max_concurrent,omitempty"`
	StripPrefix        bool              `json:"strip_prefix"`
	AddHeaders         map[string]string `json:"add_headers,omitempty"`
	RemoveHeaders      []string          `json:"remove_headers,omitempty"`
	AddResponseHeaders map[string]string `json:"add_response_headers,omitempty"`
	UpstreamTimeout    Duration          `json:"upstream_timeout"`
}

type Upstream struct {
	ID     string `json:"id,omitempty"`
	URL    string `json:"url"`
	Weight int    `json:"weight,omitempty"`
}

type Balance struct {
	Strategy string `json:"strategy,omitempty"`
	HashKey  string `json:"hash_key,omitempty"`
}

type HealthCheck struct {
	Enabled            bool     `json:"enabled,omitempty"`
	Path               string   `json:"path,omitempty"`
	Interval           Duration `json:"interval,omitempty"`
	Timeout            Duration `json:"timeout,omitempty"`
	HealthyThreshold   int      `json:"healthy_threshold,omitempty"`
	UnhealthyThreshold int      `json:"unhealthy_threshold,omitempty"`
}

type Retry struct {
	Attempts int      `json:"attempts,omitempty"`
	Backoff  Duration `json:"backoff,omitempty"`
	Statuses []int    `json:"statuses,omitempty"`
	Methods  []string `json:"methods,omitempty"`
}

type CircuitBreaker struct {
	FailureThreshold int      `json:"failure_threshold,omitempty"`
	OpenTimeout      Duration `json:"open_timeout,omitempty"`
	HalfOpenRequests int      `json:"half_open_requests,omitempty"`
}

// Policy содержит match-дерево и выполняемое действие.
type Policy struct {
	ID       string    `json:"id"`
	Priority int       `json:"priority"`
	Mode     string    `json:"mode"`
	Action   string    `json:"action"`
	Match    Match     `json:"match"`
	Limit    *Limit    `json:"limit,omitempty"`
	Rewrite  *Rewrite  `json:"rewrite,omitempty"`
	Redirect *Redirect `json:"redirect,omitempty"`
	Auth     *Auth     `json:"auth,omitempty"`
}

type Match struct {
	Methods       []string          `json:"methods,omitempty"`
	PathPrefix    string            `json:"path_prefix,omitempty"`
	Path          *StringMatch      `json:"path,omitempty"`
	SourceCIDRs   []string          `json:"source_cidrs,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	HeaderMatches []NamedMatch      `json:"header_matches,omitempty"`
	Query         map[string]string `json:"query,omitempty"`
	QueryMatches  []NamedMatch      `json:"query_matches,omitempty"`
	MinBodyBytes  int64             `json:"min_body_bytes,omitempty"`
	MaxBodyBytes  int64             `json:"max_body_bytes,omitempty"`
	RouteIDs      []string          `json:"route_ids,omitempty"`
	All           []Match           `json:"all,omitempty"`
	Any           []Match           `json:"any,omitempty"`
	Not           *Match            `json:"not,omitempty"`
}

type StringMatch struct {
	Exact  string `json:"exact,omitempty"`
	Prefix string `json:"prefix,omitempty"`
	Suffix string `json:"suffix,omitempty"`
	Regex  string `json:"regex,omitempty"`
	Absent bool   `json:"absent,omitempty"`
}

type NamedMatch struct {
	Name  string      `json:"name"`
	Match StringMatch `json:"match"`
}

type Limit struct {
	Algorithm     string   `json:"algorithm,omitempty"`
	Requests      int      `json:"requests"`
	Window        Duration `json:"window"`
	Burst         int      `json:"burst"`
	Key           string   `json:"key"`
	MaxConcurrent int      `json:"max_concurrent,omitempty"`
	IdleTTL       Duration `json:"idle_ttl,omitempty"`
}

type Rewrite struct {
	PathPrefix string            `json:"path_prefix,omitempty"`
	SetHeaders map[string]string `json:"set_headers,omitempty"`
}

type Redirect struct {
	URL    string `json:"url"`
	Status int    `json:"status,omitempty"`
}

type Auth struct {
	Type        string `json:"type"`
	Header      string `json:"header,omitempty"`
	Value       string `json:"value,omitempty"`
	Username    string `json:"username,omitempty"`
	Password    string `json:"password,omitempty"`
	JWTSecret   string `json:"jwt_secret,omitempty"`
	JWTIssuer   string `json:"jwt_issuer,omitempty"`
	JWTAudience string `json:"jwt_audience,omitempty"`
}
