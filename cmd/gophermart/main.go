package main

import (
	"log"

	"github.com/levinOo/go-market/internal/config"
	"github.com/levinOo/go-market/internal/service"
)

func main() {
	err := run()
	if err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg := config.GetConfig()

	return service.Serve(cfg)
}
