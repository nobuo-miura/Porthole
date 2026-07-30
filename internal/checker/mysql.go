package checker

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	"github.com/go-sql-driver/mysql"
)

type MySQLChecker struct{}

func (c *MySQLChecker) Check(ctx context.Context, req CheckRequest) CheckResult {
	return Run(ctx, req, func(ctx context.Context) (string, error) {
		// uri 経路でも ssl_mode を検証する（旧実装は uri があると迂回していた）。
		if err := checkSSLModeAgainstURI(req, mySQLSSLModes...); err != nil {
			return "", err
		}

		dsn := req.URI
		if dsn == "" {
			var err error
			if dsn, err = mysqlDSN(req); err != nil {
				return "", err
			}
		}

		db, err := sql.Open("mysql", dsn)
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
		_ = db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version)
		_ = db.QueryRowContext(ctx, "SELECT CURRENT_USER()").Scan(&currentUser)

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

// mysqlDSN はドライバ自身の Config を使って DSN を組み立てる。
//
// 以前は fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?...") で連結していたため、
// パスワードに "@" や "/" が含まれると区切り文字と誤認されて接続先やユーザ名が
// すり替わっていた。IPv6 アドレスも "tcp(::1:3306)" となり壊れていた。
// FormatDSN はこれらを正しくエスケープする。
func mysqlDSN(req CheckRequest) (string, error) {
	cfg := mysql.NewConfig() // AllowNativePasswords などの既定値を含む
	cfg.Net = "tcp"
	cfg.Addr = req.Addr(3306)
	cfg.User = req.Username
	cfg.Passwd = req.Password
	cfg.DBName = req.Database
	// TimeoutSec をそのまま使うと未指定時に 0 = 無制限になる。
	cfg.Timeout = req.Timeout()

	tlsParam, err := mysqlTLSParam(req.SSLMode)
	if err != nil {
		return "", err
	}
	cfg.TLSConfig = tlsParam

	return cfg.FormatDSN(), nil
}

// mysqlVerifyCAConfig は verify-ca 用に登録する TLS 設定の名前。
// go-sql-driver の DSN はチェーンのみの検証を直接表現できないため、
// RegisterTLSConfig で名前付きの設定を登録して参照する。
const mysqlVerifyCAConfig = "porthole-verify-ca"

var registerMySQLVerifyCA sync.Once

// mysqlTLSParam は ssl_mode を go-sql-driver の tls パラメータに変換する。
//
// 以前は switch の default で未知の値を "false"（TLS 無効）に落としていたため、
// verify-full や verify-ca を指定しても、タイプミスでも、黙って平文で接続していた。
func mysqlTLSParam(sslMode string) (string, error) {
	mode, err := resolveSSLMode(sslMode, mySQLSSLModes...)
	if err != nil {
		return "", err
	}

	switch mode {
	case SSLDisable:
		return "false", nil

	case SSLSkipVerify:
		return "skip-verify", nil

	case SSLRequire, SSLVerifyFull:
		// ドライバの "true" はチェーンとホスト名の両方を検証する。
		return "true", nil

	case SSLVerifyCA:
		var regErr error
		registerMySQLVerifyCA.Do(func() {
			tlsConfig, err := tlsConfigForMode(SSLVerifyCA, nil)
			if err != nil {
				regErr = err

				return
			}
			regErr = mysql.RegisterTLSConfig(mysqlVerifyCAConfig, tlsConfig)
		})
		if regErr != nil {
			return "", fmt.Errorf("failed to register the verify-ca TLS config: %w", regErr)
		}

		return mysqlVerifyCAConfig, nil

	default:
		return "", fmt.Errorf("unhandled ssl_mode %q", mode)
	}
}
