package main

import (
	"context"
	"flag"
	"github.com/R0n1ns/gogateway/internal/admin"
	"github.com/R0n1ns/gogateway/internal/config"
	"github.com/R0n1ns/gogateway/internal/gateway"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	path := flag.String("config", "configs/gateforge.json", "путь к JSON-конфигурации")
	flag.Parse()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load(*path)
	if err != nil {
		log.Error("configuration error", "error", err)
		os.Exit(1)
	}
	runtime, err := gateway.New(cfg, log)
	if err != nil {
		log.Error("runtime error", "error", err)
		os.Exit(1)
	}
	pub := server(cfg.Public, runtime)
	adm := server(cfg.Admin.Server, admin.New(runtime, cfg.Admin.APIKey))
	errs := make(chan error, 2)
	go func() { log.Info("public server started", "address", pub.Addr); errs <- pub.ListenAndServe() }()
	go func() { log.Info("admin server started", "address", adm.Addr); errs <- adm.ListenAndServe() }()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-signals:
	case err := <-errs:
		log.Error("server stopped", "error", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Runtime.ShutdownTimeout.Duration)
	defer cancel()
	_ = pub.Shutdown(ctx)
	_ = adm.Shutdown(ctx)
}
func server(c config.Server, h http.Handler) *http.Server {
	return &http.Server{Addr: c.Address, Handler: h, ReadTimeout: c.ReadTimeout.Duration, WriteTimeout: c.WriteTimeout.Duration, IdleTimeout: c.IdleTimeout.Duration, ReadHeaderTimeout: 5e9}
}
