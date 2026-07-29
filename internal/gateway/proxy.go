package gateway

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/R0n1ns/gogateway/internal/config"
)

var hopHeaders = []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade"}

func cloneRequest(in *http.Request, target *url.URL, route config.Route, clientIP string, body []byte, buffered bool) *http.Request {
	out := in.Clone(in.Context())
	out.RequestURI = ""
	out.URL = &url.URL{}
	*out.URL = *in.URL
	out.URL.Scheme = target.Scheme
	out.URL.Host = target.Host
	out.URL.Path = joinPath(target.Path, in.URL.Path)
	out.Host = target.Host
	out.Header = in.Header.Clone()
	removeHopHeaders(out.Header)
	for key, value := range route.AddHeaders {
		out.Header.Set(key, value)
	}
	for _, key := range route.RemoveHeaders {
		out.Header.Del(key)
	}
	prior := out.Header.Get("X-Forwarded-For")
	if prior == "" {
		out.Header.Set("X-Forwarded-For", clientIP)
	} else {
		out.Header.Set("X-Forwarded-For", prior+", "+clientIP)
	}
	out.Header.Set("X-Forwarded-Host", in.Host)
	out.Header.Set("X-Forwarded-Proto", requestScheme(in))
	if route.StripPrefix {
		out.URL.Path = strings.TrimPrefix(out.URL.Path, route.PathPrefix)
		if out.URL.Path == "" {
			out.URL.Path = "/"
		}
	}
	if buffered {
		out.Body = io.NopCloser(bytes.NewReader(body))
		out.ContentLength = int64(len(body))
	}
	return out
}
func copyHeader(destination, source http.Header) {
	removeHopHeaders(source)
	for key, values := range source {
		destination.Del(key)
		for _, value := range values {
			destination.Add(key, value)
		}
	}
}
func removeHopHeaders(header http.Header) {
	if connection := header.Get("Connection"); connection != "" {
		for _, key := range strings.Split(connection, ",") {
			header.Del(strings.TrimSpace(key))
		}
	}
	for _, key := range hopHeaders {
		header.Del(key)
	}
}
func joinPath(base, path string) string {
	if base == "" {
		return path
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}
func requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}
