package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/phuc-nt/dandori-cli/internal/server"
	"github.com/phuc-nt/dandori-cli/internal/serverdb"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dbCfg := serverdb.Config{
		Host:     getEnv("DANDORI_DB_HOST", "localhost"),
		Port:     5432,
		Database: getEnv("DANDORI_DB_NAME", "dandori"),
		User:     getEnv("DANDORI_DB_USER", "dandori"),
		Password: getEnv("DANDORI_DB_PASSWORD", "dandori"),
		MaxConns: 20,
	}

	db, err := serverdb.Connect(ctx, dbCfg)
	if err != nil {
		slog.Error("connect db", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := db.Migrate(ctx); err != nil {
		slog.Error("migrate db", "error", err)
		os.Exit(1)
	}
	slog.Info("database migrated")

	srvCfg := server.Config{
		Listen:         getEnv("DANDORI_LISTEN", ":8080"),
		SSEIntervalSec: 10,
	}

	srv := server.New(db, srvCfg)
	srv.StartSSEBroadcast(ctx, 10*time.Second)

	httpSrv := &http.Server{
		Addr:    srvCfg.Listen,
		Handler: srv,
	}

	go func() {
		slog.Info("server starting", "listen", srvCfg.Listen)
		if err := httpSrv.ListenAndServe(); err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	slog.Info("shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	httpSrv.Shutdown(shutdownCtx)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
