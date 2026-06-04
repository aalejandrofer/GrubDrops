package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	HTTPAddr        string
	DBPath          string
	MasterKey       string
	SeedFakeAccount bool
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:        getenv("MINER_HTTP_ADDR", "0.0.0.0:8080"),
		DBPath:          getenv("MINER_DB_PATH", "/data/miner.db"),
		MasterKey:       os.Getenv("MINER_MASTER_KEY"),
		SeedFakeAccount: parseBool(os.Getenv("MINER_SEED_FAKE_ACCOUNT")),
	}
	if strings.TrimSpace(cfg.MasterKey) == "" {
		return Config{}, fmt.Errorf("MINER_MASTER_KEY is required")
	}
	return cfg, nil
}

func getenv(k, d string) string {
	if v, ok := os.LookupEnv(k); ok && v != "" {
		return v
	}
	return d
}

func parseBool(s string) bool {
	if s == "" {
		return false
	}
	b, _ := strconv.ParseBool(s)
	return b
}
