package checker

import (
	"context"
	"fmt"
	"net/url"

	amqp "github.com/rabbitmq/amqp091-go"
)

// amqpURI は AMQP の接続URIを組み立てる。
//
// 以前は fmt.Sprintf("amqp://%s:%s@%s:%d/", user, pass, host, port) で連結していたため、
// パスワードに "@" や "/" や ":" が含まれると URI の区切りと誤認され、接続先が
// すり替わっていた。IPv6 アドレスも壊れていた。
// url.URL 経由なら必要な部分だけがパーセントエンコードされる。
func amqpURI(req CheckRequest) string {
	// RabbitMQ の既定資格情報。未指定時の挙動は従来どおり。
	user := req.Username
	if user == "" {
		user = "guest"
	}
	pass := req.Password
	if pass == "" {
		pass = "guest"
	}

	u := &url.URL{
		Scheme: "amqp",
		User:   url.UserPassword(user, pass),
		Host:   req.Addr(5672),
		Path:   "/",
	}

	return u.String()
}

type RabbitMQChecker struct{}

func (c *RabbitMQChecker) Check(ctx context.Context, req CheckRequest) CheckResult {
	return Run(ctx, req, func(ctx context.Context) (string, error) {
		uri := req.URI
		if uri == "" {
			uri = amqpURI(req)
		}

		// amqp091-go doesn't support context natively on Dial,
		// so we do it via a goroutine + select.
		type result struct {
			conn *amqp.Connection
			err  error
		}
		ch := make(chan result, 1)
		go func() {
			conn, err := amqp.Dial(uri)
			ch <- result{conn, err}
		}()

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case r := <-ch:
			if r.err != nil {
				return "", r.err
			}
			defer r.conn.Close()
			props := r.conn.Properties
			version, _ := props["version"].(string)
			product, _ := props["product"].(string)
			if product == "" {
				product = "RabbitMQ"
			}
			if version != "" {
				return fmt.Sprintf("%s %s", product, version), nil
			}
			return product + " connected", nil
		}
	})
}
