package cli

import (
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestServerBindFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	cfg := map[string]any{"version": 1, "public": map[string]string{"address": listener.Addr().String()}, "admin": map[string]string{"address": "127.0.0.1:0"}}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if code := Run([]string{"run", "-config", path}, io.Discard, io.Discard); code == 0 {
		t.Fatal("bind failure returned success")
	}
}
