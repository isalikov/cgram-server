package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/isalikov/cgram-server/internal/auth"
	"github.com/isalikov/cgram-server/internal/config"
	"github.com/isalikov/cgram-server/internal/keystore"
	"github.com/isalikov/cgram-server/internal/relay"
	"github.com/isalikov/cgram-server/internal/store"
	"github.com/isalikov/cgram-server/internal/ws"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(ctx); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	authService := auth.NewService(db.Pool())
	keyService := keystore.NewService(db.Pool())
	relayService := relay.NewService(db.Pool())

	handler := ws.NewHandler(authService, keyService, relayService)

	mux := http.NewServeMux()
	mux.Handle("/ws", handler)

	server := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		log.Println("shutting down...")
		server.Shutdown(ctx)
	}()

	log.Printf("listening on %s", cfg.ListenAddr)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}
