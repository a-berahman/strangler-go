// Command legacy runs the pretend old system.
package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"

	"github.com/a-berahman/strangler-go/internal/legacy"
)

func main() {
	addr := flag.String("addr", ":8081", "listen address")
	flag.Parse()
	slog.Info("legacy listening", "addr", *addr)
	if err := http.ListenAndServe(*addr, legacy.Handler()); err != nil {
		slog.Error("legacy stopped", "err", err)
		os.Exit(1)
	}
}
