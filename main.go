package main

import (
	"log"
	"os"
	"time"

	"auth-pddikti/internal/pddikti"
	"auth-pddikti/internal/server"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8090"
	}

	client := pddikti.NewClient(pddikti.WithTimeout(10 * time.Second))
	router := server.NewRouter(client)

	log.Printf("[+] auth-pddikti service running on http://localhost:%s", port)
	if err := router.Run(":" + port); err != nil {
		log.Fatalf("[-] Failed to run server: %v", err)
	}
}
