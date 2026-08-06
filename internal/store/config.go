package store

import (
	"log"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	MySQLDatabase string `env:"MYSQL_DATABASE,notEmpty"`
	MySQLUser     string `env:"MYSQL_USER,notEmpty"`
	MySQLPassword string `env:"MYSQL_PASSWORD,notEmpty"`
	MySQLPort     string `env:"MYSQL_PORT,notEmpty"`
	RedisPort     string `env:"REDIS_PORT,notEmpty"`
	RedisPassword string `env:"REDIS_PASSWORD,notEmpty"`
}

func LoadCfg() *Config {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		log.Fatalf("Failed to set parse required env vars: %v", err)
	}
	return &cfg
}
