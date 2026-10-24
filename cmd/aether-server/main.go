package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/aether/scn/pkg/api"
	"github.com/aether/scn/pkg/cas"
	"github.com/aether/scn/pkg/db"
)

func main() {
	port := flag.String("port", "8080", "Server HTTP port")
	dataDir := flag.String("data-dir", "./data/server", "Base storage directory")
	dbPath := flag.String("db", "", "SQLite database path (defaults to <data-dir>/aether.db)")
	flag.Parse()

	// Environment overrides
	if envPort := os.Getenv("PORT"); envPort != "" {
		*port = envPort
	} else if envPort := os.Getenv("AETHER_PORT"); envPort != "" {
		*port = envPort
	}
	if envData := os.Getenv("AETHER_DATA_DIR"); envData != "" {
		*dataDir = envData
	}
	if *dbPath == "" {
		*dbPath = os.Getenv("DATABASE_URL")
		if *dbPath == "" {
			*dbPath = filepath.Join(*dataDir, "aether.db")
		}
	}

	production := strings.EqualFold(os.Getenv("AETHER_ENV"), "production") ||
		strings.EqualFold(os.Getenv("OPENSCION_ENV"), "production")
	if production && os.Getenv("SCION_AUTH_TOKEN") == "" && os.Getenv("AETHER_AUTH_TOKEN") == "" {
		log.Fatal("SCION_AUTH_TOKEN or AETHER_AUTH_TOKEN is required in production")
	}

	if err := os.MkdirAll(*dataDir, 0755); err != nil {
		log.Fatalf("Failed to create data directory %s: %v", *dataDir, err)
	}

	// Print high-impact server banner
	printBanner(*port, *dataDir, redactDataSource(*dbPath))

	// Initialize CAS store
	casDir := filepath.Join(*dataDir, "cas")
	store, err := cas.NewStore(casDir)
	if err != nil {
		log.Fatalf("Failed to initialize CAS store: %v", err)
	}

	if s3Store, err := cas.NewS3StoreFromEnv(); err == nil && s3Store != nil {
		store.SetRemote(s3Store)
		log.Printf("[OPENSCION] S3 / MinIO distributed CAS backing store connected\n")
	}

	// Initialize Database
	database, err := db.Open(*dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	// Create and start API Server
	server := api.NewServer(database, store)
	addr := ":" + *port
	log.Printf("[OPENSCION] Control plane listening on http://0.0.0.0%s\n", addr)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           server,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       2 * time.Minute,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-shutdown
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			log.Printf("Graceful shutdown failed: %v", err)
		}
	}()
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server exited with error: %v", err)
	}
}

func redactDataSource(dataSource string) string {
	if strings.Contains(dataSource, "://") {
		if parsed, err := url.Parse(dataSource); err == nil && parsed.User != nil {
			parsed.User = url.User(parsed.User.Username())
			return parsed.String()
		}
	}
	return dataSource
}

func printBanner(port, dataDir, dbPath string) {
	fmt.Println("\033[38;5;39m")
	fmt.Println("  ██████╗ ██████╗ ███████╗███╗   ██╗███████╗ ██████╗██╗ ██████╗ ███╗   ██╗")
	fmt.Println(" ██╔═══██╗██╔══██╗██╔════╝████╗  ██║██╔════╝██╔════╝██║██╔═══██╗████╗  ██║")
	fmt.Println(" ██║   ██║██████╔╝█████╗  ██╔██╗ ██║███████╗██║     ██║██║   ██║██╔██╗ ██║")
	fmt.Println(" ██║   ██║██╔═══╝ ██╔══╝  ██║╚██╗██║╚════██║██║     ██║██║   ██║██║╚██╗██║")
	fmt.Println(" ╚██████╔╝██║     ███████╗██║ ╚████║███████║╚██████╗██║╚██████╔╝██║ ╚████║")
	fmt.Println("  ╚═════╝ ╚═╝     ╚══════╝╚═╝  ╚═══╝╚══════╝ ╚═════╝╚═╝ ╚═════╝ ╚═╝  ╚═══╝")
	fmt.Println("    OpenScion: Agent-Scale Server-Centric Version Control (Anthropic Class) ")
	fmt.Println("\033[0m")
	fmt.Printf("\033[1;32m ✔ OpenScion Control Plane Ready\033[0m\n")
	fmt.Printf("   ├─ Port:        \033[1;36m%s\033[0m\n", port)
	fmt.Printf("   ├─ Data Dir:    %s\n", dataDir)
	fmt.Printf("   ├─ Database:    %s\n", dbPath)
	fmt.Printf("   ├─ Architecture: Centralized Merkle CAS + Linear WAL Merge Queue\n")
	fmt.Printf("   └─ Target:      50,000+ merges/day | <2.5s agent checkouts\n\n")
}
