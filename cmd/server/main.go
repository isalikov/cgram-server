package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	pb "github.com/isalikov/cgram-proto/gen/proto"
	"github.com/isalikov/cgram-server/internal/auth"
	"github.com/isalikov/cgram-server/internal/config"
	"github.com/isalikov/cgram-server/internal/contacts"
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
	contactsService := contacts.NewService(db.Pool())

	// Wire up presence notifications: when a user comes online/offline,
	// notify all users who have them as a contact.
	relayService.SetPresenceNotifier(func(ctx context.Context, userID, _ string, online bool) {
		// Look up username for the event
		var username string
		db.Pool().QueryRow(ctx, "SELECT username FROM users WHERE id = $1", userID).Scan(&username)

		owners, err := contactsService.GetContactOwners(ctx, userID)
		if err != nil {
			log.Printf("presence fan-out: %v", err)
			return
		}

		event := &pb.PresenceEvent{
			UserId:   userID,
			Username: username,
			Online:   online,
		}

		for _, ownerID := range owners {
			relayService.SendPresenceEvent(ctx, ownerID, event)
		}
	})

	handler := ws.NewHandler(authService, keyService, relayService, contactsService)

	mux := http.NewServeMux()
	mux.Handle("/ws", handler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := db.Pool().Ping(r.Context()); err != nil {
			http.Error(w, "db unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		log.Println("shutting down...")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown error: %v", err)
		}
		cancel()
	}()

	log.Printf("listening on %s", cfg.ListenAddr)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}
