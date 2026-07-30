package checker

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

type RedisChecker struct{}

func (c *RedisChecker) Check(ctx context.Context, req CheckRequest) CheckResult {
	return Run(ctx, req, func(ctx context.Context) (string, error) {
		// uri 経路でも ssl_mode を検証する（旧実装は uri があると迂回していた）。
		if err := checkSSLModeAgainstURI(req, redisSSLModes...); err != nil {
			return "", err
		}

		var rdb *redis.Client

		if req.URI != "" {
			opt, err := redis.ParseURL(req.URI)
			if err != nil {
				return "", fmt.Errorf("invalid Redis URI: %w", err)
			}
			rdb = redis.NewClient(opt)
		} else {
			clientOpts, err := redisOptions(req)
			if err != nil {
				return "", err
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

// redisOptions は接続オプションを組み立てる。
//
// 以前は switch の default で未知の ssl_mode を無視していたため、verify-full や
// verify-ca を指定しても、タイプミスでも、黙って平文で接続していた。
func redisOptions(req CheckRequest) (*redis.Options, error) {
	mode, err := resolveSSLMode(req.SSLMode, redisSSLModes...)
	if err != nil {
		return nil, err
	}

	tlsConfig, err := tlsConfigForMode(mode, nil)
	if err != nil {
		return nil, err
	}

	return &redis.Options{
		Addr:      req.Addr(6379),
		Password:  req.Password,
		DB:        0,
		TLSConfig: tlsConfig, // nil なら TLS を使わない
	}, nil
}
