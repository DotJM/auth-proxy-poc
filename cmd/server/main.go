package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dotjm/auth-proxy-poc/internal/api"
	"github.com/dotjm/auth-proxy-poc/internal/proxy"
	"github.com/dotjm/auth-proxy-poc/internal/store"
)

func main() {
	dbURL := envOrDefault("DATABASE_URL", "postgres://authproxy:authproxy@localhost:5432/authproxy?sslmode=disable")
	apiAddr := envOrDefault("API_ADDR", ":8080")
	proxyAddr := envOrDefault("PROXY_ADDR", ":8888")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Connect to PostgreSQL
	log.Printf("connecting to database...")
	s, err := store.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer s.Close()
	log.Printf("database connected")

	// Start API server
	apiMux := http.NewServeMux()
	apiHandler := api.NewHandler(s)
	apiHandler.RegisterRoutes(apiMux)

	apiServer := &http.Server{Addr: apiAddr, Handler: apiMux}
	go func() {
		log.Printf("API server listening on %s", apiAddr)
		if err := apiServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("API server error: %v", err)
		}
	}()

	// Start proxy server
	proxyServer := proxy.NewProxyServer(s)
	proxySrv := &http.Server{Addr: proxyAddr, Handler: proxyServer}
	go func() {
		log.Printf("proxy server listening on %s", proxyAddr)
		if err := proxySrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("proxy server error: %v", err)
		}
	}()

	fmt.Println("auth-proxy-poc is running")
	fmt.Printf("  API:   http://localhost%s/api/sessions\n", apiAddr)
	fmt.Printf("  Proxy: http://localhost%s\n", proxyAddr)

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("shutting down...")
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutCancel()
	apiServer.Shutdown(shutCtx)
	proxySrv.Shutdown(shutCtx)
	log.Println("shutdown complete")
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
