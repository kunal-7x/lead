// freeswitch-bridge: ESL event listener + recording upload worker for FreeSWITCH.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
)

func main() {
	addr := os.Getenv("FREESWITCH_BRIDGE_ADDR")
	if addr == "" {
		addr = ":8109"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok"}`)
	})

	ctx := context.Background()
	_ = ctx

	fmt.Printf("freeswitch-bridge listening on %s\n", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		fmt.Fprintf(os.Stderr, "freeswitch-bridge: %v\n", err)
		os.Exit(1)
	}
}
