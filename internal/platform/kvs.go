package platform

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

func InitRedis(cfg *Config) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%v:%v", cfg.RedisHost, cfg.RedisPort),
		Password: cfg.RedisPassword,
	})
	// Redis接続不能判定用のタイムアウト
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// go-redisも、sql.DB同様conn poolを内部に持ちlazy connection（pingするまでconnを張らない）する
	if err := client.Ping(ctx).Err(); err != nil {
		// 接続不能時はキャッシュを使わずにサーバを動かすので、leak防止のため明示的に閉じる
		client.Close()
		return nil, fmt.Errorf("failed to ping Redis: %v", err)
	}
	log.Println("Success to open Redis connection!")
	return client, nil
}
