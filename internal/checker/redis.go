package checker

import (
	"context"
	"crypto/tls"
	"fmt"

	"github.com/redis/go-redis/v9"
)

type RedisChecker struct{}

func (c *RedisChecker) Check(ctx context.Context, req CheckRequest) CheckResult {
	return Run(ctx, req, func(ctx context.Context) (string, error) {
		var rdb *redis.Client

		if req.URI != "" {
			opt, err := redis.ParseURL(req.URI)
			if err != nil {
				return "", fmt.Errorf("invalid Redis URI: %w", err)
			}
			rdb = redis.NewClient(opt)
		} else {
			host := req.Host
			port := req.Port
			if port == 0 {
				port = 6379
			}
			clientOpts := &redis.Options{
				Addr:     fmt.Sprintf("%s:%d", host, port),
				Password: req.Password,
				DB:       0,
			}
			switch req.SSLMode {
			case "require":
				clientOpts.TLSConfig = &tls.Config{}
			case "skip-verify":
				clientOpts.TLSConfig = &tls.Config{InsecureSkipVerify: true}
			}
			rdb = redis.NewClient(clientOpts)
		}
		defer rdb.Close()

		result, err := rdb.Ping(ctx).Result()
		if err != nil {
			return "", err
		}

		detail := fmt.Sprintf("Redis PING: %s", result)
		if req.Password != "" || req.URI != "" {
			detail += " | authentication successful"
		}
		return detail, nil
	})
}
