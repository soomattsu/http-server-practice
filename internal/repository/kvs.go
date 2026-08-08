package repository

import (
	"context"
	"fmt"
	"log"

	"github.com/redis/go-redis/v9"
)

func TryRedis(cfg *Config) {
	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("localhost:%v", cfg.RedisPort),
		Password: cfg.RedisPassword,
	})
	defer client.Close()
	pong, err := client.Ping(context.Background()).Result()
	if err != nil {
		log.Fatalf("Failed to health-check of Redis: %v", err)
	}
	log.Printf("Success to login Redis, %v!", pong)
}
