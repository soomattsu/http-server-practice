package repository

import (
	"context"
	"fmt"
	"log"

	"github.com/redis/go-redis/v9"
)

func InitRedis(cfg *Config) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("localhost:%v", cfg.RedisPort),
		Password: cfg.RedisPassword,
	})
	// go-redisも、sql.DB同様コネクションプールを内部に持ち、lazy connection（pingするまでconnを張らない）
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("failed to ping Redis: %v", err)
	}
	log.Println("Success to open Redis connection!")
	return client, nil
}
