package config

import (
	"flag"
	"os"
	"strconv"
)

type Config struct {
	Addr            string `env:"RUN_ADDRESS"`
	DataBaseAddr    string `env:"DATABASE_URI"`
	SystemAddr      string `env:"ACCRUAL_SYSTEM_ADDRESS"`
	PepperKey       string `env:"KEY"`
	SecretKey       string `env:"SECRET_KEY"`
	AccrualRetryNum int    `env:"ACCRUAL_RETRY_NUM"`
}

func GetConfig() Config {
	addrServer := flag.String("a", "localhost:8080", "HTTP server address")
	addrDB := flag.String("d", "", "Database uri")
	addrSystem := flag.String("r", "", "System address")
	pepperKey := flag.String("k", "", "Hash key")
	secretKey := flag.String("s", "", "Secret key")
	accrualRetryNum := flag.Int("n", 3, "Accrual retry connection number")

	flag.Parse()

	cfg := Config{
		Addr:            getValue(*addrServer, os.Getenv("RUN_ADDRESS")),
		DataBaseAddr:    getValue(*addrDB, os.Getenv("DATABASE_URI")),
		SystemAddr:      getValue(*addrSystem, os.Getenv("ACCRUAL_SYSTEM_ADDRESS")),
		PepperKey:       getValue(*pepperKey, os.Getenv("KEY")),
		SecretKey:       getValue(*secretKey, os.Getenv("SECRET_KEY")),
		AccrualRetryNum: getIntValue(*accrualRetryNum, os.Getenv("ACCRUAL_RETRY_NUM")),
	}

	return cfg
}

func getValue(flagValue, envValue string) string {
	if flagValue != "" {
		return flagValue
	}
	return envValue
}

func getIntValue(flagValue int, envValue string) int {
	if flagValue != 0 {
		return flagValue
	}

	if envValue != "" {
		if v, err := strconv.Atoi(envValue); err == nil {
			return v
		}
	}

	return 0
}
