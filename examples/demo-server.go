package main

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

func main() {
	fmt.Println("Demo server for bankshot")
	fmt.Println("Waiting 2 seconds before binding to port...")
	time.Sleep(2 * time.Second)

	fmt.Println("Starting HTTP server on :9090")

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Hello from bankshot demo server!\n")
		fmt.Fprintf(w, "Time: %s\n", time.Now().Format(time.RFC3339))
		log.Printf("Request from %s", r.RemoteAddr)
	})

	log.Fatal(http.ListenAndServe(":9090", nil))
}
