package checker

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"

	_ "github.com/microsoft/go-mssqldb"
)

type SQLServerChecker struct{}

func (c *SQLServerChecker) Check(ctx context.Context, req CheckRequest) CheckResult {
	return Run(ctx, req, func(ctx context.Context) (string, error) {
		dsn := req.URI
		if dsn == "" {
			host := req.Host
			port := req.Port
			if port == 0 {
				port = 1433
			}
			q := url.Values{}
			q.Set("connection timeout", fmt.Sprintf("%d", req.TimeoutSec))
			if req.Database != "" {
				q.Set("database", req.Database)
			}
			// encrypt: disable | false (no verify) | true (require+verify)
			switch req.SSLMode {
			case "require":
				q.Set("encrypt", "true")
			case "skip-verify":
				q.Set("encrypt", "true")
				q.Set("TrustServerCertificate", "true")
			default: // disable
				q.Set("encrypt", "disable")
			}
			u := &url.URL{
				Scheme:   "sqlserver",
				User:     url.UserPassword(req.Username, req.Password),
				Host:     fmt.Sprintf("%s:%d", host, port),
				RawQuery: q.Encode(),
			}
			dsn = u.String()
		}

		db, err := sql.Open("sqlserver", dsn)
		if err != nil {
			return "", err
		}
		defer db.Close()

		if err := db.PingContext(ctx); err != nil {
			return "", err
		}

		var version, currentUser string
		db.QueryRowContext(ctx, "SELECT @@VERSION").Scan(&version)
		db.QueryRowContext(ctx, "SELECT SYSTEM_USER").Scan(&currentUser)

		// Shorten long version string
		if len(version) > 40 {
			version = version[:40]
		}
		detail := "SQL Server"
		if version != "" {
			detail = version
		}
		if currentUser != "" {
			detail += fmt.Sprintf(" | authenticated as %s", currentUser)
		}
		return detail, nil
	})
}
