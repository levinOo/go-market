package config

import (
	"flag"
	"os"
)

type Config struct {
	Addr         string `env:"RUN_ADDRESS"`
	DataBaseAddr string `env:"DATABASE_URI"`
	SystemAddr   string `env:"ACCRUAL_SYSTEM_ADDRESS"`
	PepperKey    string `env:"KEY"`
	SecretKey    string `env:"SECRET_KEY"`
}

func GetConfig() Config {
	addrFlag := flag.String("a", "localhost:8080", "HTTP server addres")
	addrDB := flag.String("d", "", "Database uri")
	addrSystem := flag.String("r", "", "System addres")
	pepperKey := flag.String("k", "", "Hash key")
	secretKey := flag.String("s", "", "Secret key")

	flag.Parse()

	cfg := Config{
		Addr:         getString(*addrFlag, os.Getenv("RUN_ADDRESS")),
		DataBaseAddr: getString(*addrDB, os.Getenv("DATABASE_URI")),
		SystemAddr:   getString(*addrSystem, os.Getenv("ACCRUAL_SYSTEM_ADDRESS")),
		PepperKey:    getString(*pepperKey, os.Getenv("KEY")),
		SecretKey:    getString(*secretKey, os.Getenv("SECRET_KEY")),
	}

	return cfg
}

func getString(flagValue string, envValue string) string {
	if flagValue != "" {
		return flagValue
	}
	return envValue

}
