package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Port, PostgresDSN, TDengineDSN, TDengineDatabase string
	HistoryRetentionDays, CollectorWorkers           int
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func intEnv(k string, d int) int {
	v, e := strconv.Atoi(env(k, strconv.Itoa(d)))
	if e != nil {
		return d
	}
	return v
}
func Load() Config {
	ph, pp, pu, pw, pd := env("POSTGRES_HOST", "localhost"), env("POSTGRES_PORT", "5432"), env("POSTGRES_USER", "postgres"), env("POSTGRES_PASSWORD", ""), env("POSTGRES_DATABASE", "aquacontrolai")
	th, tp, tu, tw, td := env("TDENGINE_HOST", "localhost"), env("TDENGINE_PORT", "6041"), env("TDENGINE_USER", "root"), env("TDENGINE_PASSWORD", ""), env("TDENGINE_DATABASE", "aquacontrolai")
	return Config{Port: env("APP_PORT", "8080"), PostgresDSN: fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s", pu, pw, ph, pp, pd, env("POSTGRES_SSLMODE", "disable")), TDengineDSN: fmt.Sprintf("%s:%s@http(%s:%s)/%s", tu, tw, th, tp, td), TDengineDatabase: td, HistoryRetentionDays: intEnv("HISTORY_RETENTION_DAYS", 365), CollectorWorkers: intEnv("COLLECTOR_WORKERS", 8)}
}
