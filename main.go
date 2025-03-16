package main

import (
	"fmt"
	"log"
	"net/http"
)

func main() {
	newMux := http.NewServeMux()

	server := &http.Server{
		Addr:    ":8080",
		Handler: newMux,
	}

	fmt.Println("Server starting on :8080...")

	err := server.ListenAndServe()
	if err != nil {
		log.Fatal("Server error:", err)
	}

}
