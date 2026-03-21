package checker

import (
	"context"
	"fmt"
)

var registry = map[string]Checker{
	"tcp":           &TCPChecker{},
	"udp":           &UDPChecker{},
	"mysql":         &MySQLChecker{},
	"mariadb":       &MySQLChecker{},
	"postgres":      &PostgresChecker{},
	"postgresql":    &PostgresChecker{},
	"mongodb":       &MongoDBChecker{},
	"redis":         &RedisChecker{},
	"elasticsearch": &ElasticsearchChecker{},
	"rabbitmq":      &RabbitMQChecker{},
	"smtp":          &SMTPChecker{},
	"sqlserver":     &SQLServerChecker{},
	"mssql":         &SQLServerChecker{},
}

// Dispatch runs the appropriate checker for the given request type.
func Dispatch(ctx context.Context, req CheckRequest) (CheckResult, error) {
	c, ok := registry[req.Type]
	if !ok {
		return CheckResult{}, fmt.Errorf("unknown checker type: %q", req.Type)
	}
	ctx, cancel := context.WithTimeout(ctx, req.Timeout())
	defer cancel()
	return c.Check(ctx, req), nil
}

// SupportedTypes returns all registered checker type names.
func SupportedTypes() []string {
	types := make([]string, 0, len(registry))
	for t := range registry {
		types = append(types, t)
	}
	return types
}
