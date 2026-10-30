package database

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"

	"latihan-fiber/config"
)

func NewRedisClient(ctx context.Context) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     config.GetEnv("REDIS_ADDR", "127.0.0.1:6379"),
		Password: config.GetEnv("REDIS_PASSWORD", ""),
	})

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := client.Ping(pingCtx).Err(); err != nil {
		client.Close()
		return nil, err
	}

	return client, nil
}
