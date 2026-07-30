package checker

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strconv"

	_ "github.com/microsoft/go-mssqldb"
)

type SQLServerChecker struct{}

func (c *SQLServerChecker) Check(ctx context.Context, req CheckRequest) CheckResult {
	return Run(ctx, req, func(ctx context.Context) (string, error) {
		// uri 経路でも ssl_mode を検証する（旧実装は uri があると迂回していた）。
		if err := checkSSLModeAgainstURI(req, sqlServerSSLModes...); err != nil {
			return "", err
		}

		dsn := req.URI
		if dsn == "" {
			var err error
			if dsn, err = sqlServerDSN(req); err != nil {
				return "", err
			}
		}

		db, err := sql.Open("sqlserver", dsn)
		if err != nil {
			return "", err
		}
		defer db.Close()

		if err := db.PingContext(ctx); err != nil {
			return "", err
		}

		// Ping は成功しているので接続確認は済み。以下はベストエフォートの付加情報で、
		// 権限不足などで取得できなくてもチェック自体は成功扱いにする。
		var version, currentUser string
		_ = db.QueryRowContext(ctx, "SELECT @@VERSION").Scan(&version)
		_ = db.QueryRowContext(ctx, "SELECT SYSTEM_USER").Scan(&currentUser)

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

// sqlServerDSN は接続文字列を組み立てる。
//
// 以前は switch の default で未知の ssl_mode を "encrypt=disable" に落としていたため、
// verify-full を指定しても、タイプミスでも、黙って平文で接続していた。
//
// verify-ca（チェーンのみ検証）は go-mssqldb の DSN パラメータでは表現できないため
// 対応していない。エラーにして利用者に知らせる。対応するには sql.Open ではなく
// コネクタAPIに移行して独自の tls.Config を渡す必要がある。
func sqlServerDSN(req CheckRequest) (string, error) {
	mode, err := resolveSSLMode(req.SSLMode, sqlServerSSLModes...)
	if err != nil {
		return "", err
	}

	q := url.Values{}
	// TimeoutSec をそのまま使うと未指定時に 0 = 無制限になる。
	q.Set("connection timeout", strconv.Itoa(int(req.Timeout().Seconds())))
	if req.Database != "" {
		q.Set("database", req.Database)
	}

	switch mode {
	case SSLDisable:
		q.Set("encrypt", "disable")

	case SSLSkipVerify:
		q.Set("encrypt", "true")
		q.Set("TrustServerCertificate", "true")

	case SSLRequire, SSLVerifyFull:
		// encrypt=true は証明書を検証する。
		q.Set("encrypt", "true")

	default:
		return "", fmt.Errorf("unhandled ssl_mode %q", mode)
	}

	u := &url.URL{
		Scheme:   "sqlserver",
		User:     url.UserPassword(req.Username, req.Password),
		Host:     req.Addr(1433),
		RawQuery: q.Encode(),
	}

	return u.String(), nil
}
