package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

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
		*dbPath = filepath.Join(*dataDir, "aether.db")
	}

	if err := os.MkdirAll(*dataDir, 0755); err != nil {
		log.Fatalf("Failed to create data directory %s: %v", *dataDir, err)
	}

	// Print high-impact server banner
	printBanner(*port, *dataDir, *dbPath)

	// Initialize CAS store
	casDir := filepath.Join(*dataDir, "cas")
	store, err := cas.NewStore(casDir)
	if err != nil {
		log.Fatalf("Failed to initialize CAS store: %v", err)
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
	log.Printf("[AETHER] Control plane listening on http://0.0.0.0%s\n", addr)
	if err := http.ListenAndServe(addr, server); err != nil {
		log.Fatalf("Server exited with error: %v", err)
	}
}

func printBanner(port, dataDir, dbPath string) {
	fmt.Println("\033[38;5;39m")
	fmt.Println("  █████╗ ███████╗████████╗██╗  ██╗███████╗██████╗ ")
	fmt.Println(" ██╔══██╗██╔════╝╚══██╔══╝██║  ██║██╔════╝██╔══██╗")
	fmt.Println(" ███████║█████╗     ██║   ███████║█████╗  ██████╔╝")
	fmt.Println(" ██╔══██║██╔══╝     ██║   ██╔══██║██╔══╝  ██╔══██╗")
	fmt.Println(" ██║  ██║███████╗   ██║   ██║  ██║███████╗██║  ██║")
	fmt.Println(" ╚═╝  ╚═╝╚══════╝   ╚═╝   ╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝")
	fmt.Println("   Agent-Scale Server-Centric Version Control   ")
	fmt.Println("\033[0m")
	fmt.Printf("\033[1;32m ✔ Control Plane Ready\033[0m\n")
	fmt.Printf("   ├─ Port:        \033[1;36m%s\033[0m\n", port)
	fmt.Printf("   ├─ Data Dir:    %s\n", dataDir)
	fmt.Printf("   ├─ Database:    %s\n", dbPath)
	fmt.Printf("   ├─ Architecture: Centralized Merkle CAS + Linear WAL Merge Queue\n")
	fmt.Printf("   └─ Target:      50,000+ merges/day | <2.5s agent checkouts\n\n")
}
