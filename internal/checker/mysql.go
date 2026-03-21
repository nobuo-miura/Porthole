package checker

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/go-sql-driver/mysql"
)

type MySQLChecker struct{}

func (c *MySQLChecker) Check(ctx context.Context, req CheckRequest) CheckResult {
	return Run(ctx, req, func(ctx context.Context) (string, error) {
		dsn := req.URI
		if dsn == "" {
			host := req.Host
			port := req.Port
			if port == 0 {
				port = 3306
			}
			// tls param: false=disable, true=require+verify, skip-verify=require without verify
			tlsParam := "false"
			switch req.SSLMode {
			case "require":
				tlsParam = "true"
			case "skip-verify":
				tlsParam = "skip-verify"
			}
			dsn = fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?timeout=%ds&tls=%s",
				req.Username, req.Password, host, port, req.Database, req.TimeoutSec, tlsParam)
		}

		db, err := sql.Open("mysql", dsn)
		if err != nil {
			return "", err
		}
		defer db.Close()

		if err := db.PingContext(ctx); err != nil {
			return "", err
		}

		var version, currentUser string
		db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version)
		db.QueryRowContext(ctx, "SELECT CURRENT_USER()").Scan(&currentUser)

		detail := "MySQL"
		if version != "" {
			detail = fmt.Sprintf("MySQL %s", version)
		}
		if currentUser != "" {
			detail += fmt.Sprintf(" | authenticated as %s", currentUser)
		}
		return detail, nil
	})
}
