package main

import (
	"log"
	"net/http"
	"os"

	"github.com/fhak/pelagicsociety/internal/server"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv, err := server.New()
	if err != nil {
		log.Fatalf("init server: %v", err)
	}

	log.Printf("pelagicsociety listening on :%s", port)
	if err := http.ListenAndServe(":"+port, srv); err != nil {
		log.Fatal(err)
	}
}
