package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/fhak/pelagicsociety/internal/db"
	"github.com/fhak/pelagicsociety/internal/mail"
	"github.com/fhak/pelagicsociety/internal/server"
)

func main() {
	port := envOr("PORT", "8080")
	dbPath := envOr("DB_PATH", "./data/pelagicsociety.db")

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}

	sqlDB, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer sqlDB.Close()

	mailer := mail.New(
		context.Background(),
		envOr("MAIL_FROM", "Pelagic Society <no-reply@pelagicsociety.com>"),
		envOr("MAIL_REPLY_TO", ""),
	)

	srv, err := server.New(server.Config{
		DB:            sqlDB,
		Mailer:        mailer,
		ContactToAddr: envOr("CONTACT_TO", ""),
	})
	if err != nil {
		log.Fatalf("init server: %v", err)
	}

	httpSrv := &http.Server{
		Addr:              ":" + port,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	log.Printf("pelagicsociety listening on :%s (db=%s)", port, dbPath)
	if err := httpSrv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
