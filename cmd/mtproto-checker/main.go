package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rahgozar94725/MTProto-Checker/internal/server"
)

const (
	defaultHost = "127.0.0.1"
	defaultPort = 3000
)

var version = "dev"

// resolveAddr builds the listen address from the HOST and PORT env values.
// Loopback by default; exposing the server (e.g. HOST=0.0.0.0) is an explicit
// opt-in. PORT parsing is deliberately as lenient as it always was: the
// Sscanf error is ignored, so garbage keeps the default and a numeric prefix
// is used as-is.
func resolveAddr(hostEnv, portEnv string) string {
	host := hostEnv
	if host == "" {
		host = defaultHost
	}
	port := defaultPort
	if portEnv != "" {
		fmt.Sscanf(portEnv, "%d", &port)
	}
	return net.JoinHostPort(host, fmt.Sprintf("%d", port))
}

func main() {
	mux := server.NewMux(version)

	addr := resolveAddr(os.Getenv("HOST"), os.Getenv("PORT"))
	log.Printf("MTProto Checker %s", version)
	log.Printf("Server running at http://%s", addr)

	server.AllowedHosts = server.HostAllowlist(addr)
	if len(server.AllowedHosts) == 0 {
		log.Printf("WARNING: %s is not loopback — the Host check is off and there is no auth", addr)
	}

	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 300 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Listen error: %v", err)
	}

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	<-done
	log.Println("Shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), server.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Shutdown error: %v", err)
	}
	log.Println("Server stopped")
}
