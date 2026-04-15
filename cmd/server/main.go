package main

import (
	"log/slog"
	"net/http"
	"os"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Info("cobalt-dingo starting", "port", port)

	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Error("server failed", "err", err)
		os.Exit(1)
	}
}
