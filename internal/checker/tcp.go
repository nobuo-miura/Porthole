package checker

import (
	"context"
	"fmt"
	"net"
)

type TCPChecker struct{}

func (c *TCPChecker) Check(ctx context.Context, req CheckRequest) CheckResult {
	return Run(ctx, req, func(ctx context.Context) (string, error) {
		addr := fmt.Sprintf("%s:%d", req.Host, req.Port)
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
		if err != nil {
			return "", err
		}
		conn.Close()
		return fmt.Sprintf("TCP connection to %s successful", addr), nil
	})
}

type UDPChecker struct{}

func (c *UDPChecker) Check(ctx context.Context, req CheckRequest) CheckResult {
	return Run(ctx, req, func(ctx context.Context) (string, error) {
		addr := fmt.Sprintf("%s:%d", req.Host, req.Port)
		conn, err := (&net.Dialer{}).DialContext(ctx, "udp", addr)
		if err != nil {
			return "", err
		}
		conn.Close()
		return fmt.Sprintf("UDP connection to %s established", addr), nil
	})
}
