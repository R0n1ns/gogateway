package config

// DeepCopy создаёт независимый mutable документ. Runtime хранит только копии,
// поэтому изменение maps/slices через control API не затрагивает активный snapshot.
func (c *Config) DeepCopy() *Config {
	if c == nil {
		return nil
	}
	out := *c
	out.Runtime.TrustedProxies = cloneSlice(c.Runtime.TrustedProxies)
	out.Routes = make([]Route, len(c.Routes))
	for i := range c.Routes {
		out.Routes[i] = c.Routes[i]
		out.Routes[i].Upstreams = cloneSlice(c.Routes[i].Upstreams)
		out.Routes[i].UpstreamPool = cloneSlice(c.Routes[i].UpstreamPool)
		out.Routes[i].RemoveHeaders = cloneSlice(c.Routes[i].RemoveHeaders)
		out.Routes[i].AddHeaders = cloneMap(c.Routes[i].AddHeaders)
		out.Routes[i].AddResponseHeaders = cloneMap(c.Routes[i].AddResponseHeaders)
		out.Routes[i].Retry.Methods = cloneSlice(c.Routes[i].Retry.Methods)
		out.Routes[i].Retry.Statuses = cloneSlice(c.Routes[i].Retry.Statuses)
	}
	out.Policies = make([]Policy, len(c.Policies))
	for i := range c.Policies {
		out.Policies[i] = c.Policies[i]
		out.Policies[i].Match = cloneMatch(c.Policies[i].Match)
		if c.Policies[i].Limit != nil {
			value := *c.Policies[i].Limit
			out.Policies[i].Limit = &value
		}
		if c.Policies[i].Rewrite != nil {
			value := *c.Policies[i].Rewrite
			value.SetHeaders = cloneMap(value.SetHeaders)
			out.Policies[i].Rewrite = &value
		}
		if c.Policies[i].Redirect != nil {
			value := *c.Policies[i].Redirect
			out.Policies[i].Redirect = &value
		}
		if c.Policies[i].Auth != nil {
			value := *c.Policies[i].Auth
			out.Policies[i].Auth = &value
		}
	}
	return &out
}

func cloneMatch(source Match) Match {
	out := source
	out.Methods = cloneSlice(source.Methods)
	out.SourceCIDRs = cloneSlice(source.SourceCIDRs)
	out.RouteIDs = cloneSlice(source.RouteIDs)
	out.Headers = cloneMap(source.Headers)
	out.Query = cloneMap(source.Query)
	out.HeaderMatches = cloneSlice(source.HeaderMatches)
	out.QueryMatches = cloneSlice(source.QueryMatches)
	if source.Path != nil {
		value := *source.Path
		out.Path = &value
	}
	out.All = make([]Match, len(source.All))
	for i := range source.All {
		out.All[i] = cloneMatch(source.All[i])
	}
	out.Any = make([]Match, len(source.Any))
	for i := range source.Any {
		out.Any[i] = cloneMatch(source.Any[i])
	}
	if source.Not != nil {
		value := cloneMatch(*source.Not)
		out.Not = &value
	}
	return out
}

func cloneSlice[T any](source []T) []T { return append([]T(nil), source...) }
func cloneMap[K comparable, V any](source map[K]V) map[K]V {
	if source == nil {
		return nil
	}
	out := make(map[K]V, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}
