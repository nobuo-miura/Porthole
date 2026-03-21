package checker

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
)

type PostgresChecker struct{}

func (c *PostgresChecker) Check(ctx context.Context, req CheckRequest) CheckResult {
	return Run(ctx, req, func(ctx context.Context) (string, error) {
		dsn := req.URI
		if dsn == "" {
			host := req.Host
			port := req.Port
			if port == 0 {
				port = 5432
			}
			// PostgreSQL sslmode: disable | require | verify-ca | verify-full
			sslmode := req.SSLMode
			if sslmode == "" || sslmode == "skip-verify" {
				sslmode = "disable"
			}
			dsn = fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s connect_timeout=%d",
				host, port, req.Username, req.Password, req.Database, sslmode, req.TimeoutSec)
		}

		db, err := sql.Open("postgres", dsn)
		if err != nil {
			return "", err
		}
		defer db.Close()

		if err := db.PingContext(ctx); err != nil {
			return "", err
		}

		var version, currentUser string
		db.QueryRowContext(ctx, "SELECT version()").Scan(&version)
		db.QueryRowContext(ctx, "SELECT current_user").Scan(&currentUser)

		if len(version) > 20 {
			version = version[:20]
		}
		detail := "PostgreSQL"
		if version != "" {
			detail = version
		}
		if currentUser != "" {
			detail += fmt.Sprintf(" | authenticated as %s", currentUser)
		}
		return detail, nil
	})
}
