// Command newsvc runs the new customers service.
package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"

	"github.com/a-berahman/strangler-go/internal/customers"
)

func main() {
	addr := flag.String("addr", ":8082", "listen address")
	flag.Parse()
	slog.Info("newsvc listening", "addr", *addr)
	if err := http.ListenAndServe(*addr, customers.Handler(customers.NewStore())); err != nil {
		slog.Error("newsvc stopped", "err", err)
		os.Exit(1)
	}
}
