package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"bet9ja-vfl/services/api"
	"bet9ja-vfl/services/ingest"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	migrationsDir := "db/migrations"
	listen := "127.0.0.1:8090"
	token := os.Getenv("INTERNAL_API_TOKEN")
	auth := api.AuthConfigFromEnv()
	flag.StringVar(&databaseURL, "database-url", databaseURL, "PostgreSQL connection URL")
	flag.StringVar(&migrationsDir, "migrations-dir", migrationsDir, "directory containing SQL migrations")
	flag.StringVar(&listen, "listen", listen, "private listen address; default is loopback")
	flag.StringVar(&token, "internal-token", token, "optional internal request token")
	flag.Parse()
	if databaseURL == "" {
		fatal("set DATABASE_URL or -database-url")
	}
	if !strings.HasPrefix(listen, "127.0.0.1:") && !strings.HasPrefix(listen, "localhost:") && !strings.HasPrefix(listen, "[::1]:") && strings.TrimSpace(token) == "" {
		fatal("non-loopback operational API requires -internal-token or INTERNAL_API_TOKEN")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := ingest.OpenDatabase(ctx, databaseURL)
	if err != nil {
		fatal(err.Error())
	}
	defer db.Close()
	if err := ingest.ApplyMigrations(ctx, db, migrationsDir); err != nil {
		fatal(err.Error())
	}
	if strings.TrimSpace(auth.AuthorizedEmail) != "" {
		if err := api.EnsureAuthorizedAccount(ctx, db, auth.AuthorizedEmail); err != nil {
			fatal(err.Error())
		}
	}
	server := &http.Server{Addr: listen, Handler: api.NewServerWithAuthConfig(db, token, auth).Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fatal(err.Error())
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}

func fatal(message string) {
	_, _ = fmt.Fprintln(os.Stderr, "ops-api:", message)
	os.Exit(1)
}
