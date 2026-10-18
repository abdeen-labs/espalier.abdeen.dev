// espalierd serves espalier.abdeen.dev: Espalier's page and its assets, built
// into the binary, with the download pointing at the newest release in
// Espalier's appcast, which it reads again every few minutes.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	log.SetFlags(0)
	port := envOr("PORT", "8080")
	feed := envOr("APPCAST_URL", defaultAppcastURL)

	releases, err := newReleases(feed, &http.Client{Timeout: 20 * time.Second})
	if err != nil {
		log.Fatalf("espalierd: %v", err)
	}
	site, err := newSite(releases)
	if err != nil {
		log.Fatalf("espalierd: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go releases.run(ctx, refreshInterval)

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           site,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()

	log.Printf("espalierd: listening on :%s, release from %s", port, feed)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("espalierd: %v", err)
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
