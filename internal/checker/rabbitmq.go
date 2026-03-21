package checker

import (
	"context"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

type RabbitMQChecker struct{}

func (c *RabbitMQChecker) Check(ctx context.Context, req CheckRequest) CheckResult {
	return Run(ctx, req, func(ctx context.Context) (string, error) {
		uri := req.URI
		if uri == "" {
			host := req.Host
			port := req.Port
			if port == 0 {
				port = 5672
			}
			user := req.Username
			if user == "" {
				user = "guest"
			}
			pass := req.Password
			if pass == "" {
				pass = "guest"
			}
			uri = fmt.Sprintf("amqp://%s:%s@%s:%d/", user, pass, host, port)
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
