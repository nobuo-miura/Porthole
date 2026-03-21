package checker

import (
	"context"
	"fmt"
	"net"
	"net/smtp"
	"time"
)

type SMTPChecker struct{}

func (c *SMTPChecker) Check(ctx context.Context, req CheckRequest) CheckResult {
	return Run(ctx, req, func(ctx context.Context) (string, error) {
		host := req.Host
		port := req.Port
		if port == 0 {
			port = 25
		}
		addr := fmt.Sprintf("%s:%d", host, port)

		// Use DialContext for proper context cancellation
		deadline, ok := ctx.Deadline()
		if !ok {
			deadline = time.Now().Add(5 * time.Second)
		}

		conn, err := (&net.Dialer{Deadline: deadline}).DialContext(ctx, "tcp", addr)
		if err != nil {
			return "", err
		}

		client, err := smtp.NewClient(conn, host)
		if err != nil {
			conn.Close()
			return "", err
		}
		defer client.Close()

		// Perform EHLO handshake
		if err := client.Hello("porthole"); err != nil {
			return "", err
		}

		return fmt.Sprintf("SMTP server at %s responded", addr), nil
	})
}
