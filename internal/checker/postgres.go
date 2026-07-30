package checker

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	_ "github.com/lib/pq"
)

type PostgresChecker struct{}

func (c *PostgresChecker) Check(ctx context.Context, req CheckRequest) CheckResult {
	return Run(ctx, req, func(ctx context.Context) (string, error) {
		// uri 経路でも ssl_mode を検証する（旧実装は uri があると迂回していた）。
		if err := checkSSLModeAgainstURI(req, postgresSSLModes...); err != nil {
			return "", err
		}

		dsn := req.URI
		if dsn == "" {
			var err error
			if dsn, err = pgDSN(req); err != nil {
				return "", err
			}
		}

		db, err := sql.Open("postgres", dsn)
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
		_ = db.QueryRowContext(ctx, "SELECT version()").Scan(&version)
		_ = db.QueryRowContext(ctx, "SELECT current_user").Scan(&currentUser)

		detail := "PostgreSQL"
		if version != "" {
			detail = pgShortVersion(version)
		}
		if currentUser != "" {
			detail += fmt.Sprintf(" | authenticated as %s", currentUser)
		}

		return detail, nil
	})
}

// pgDSN は libpq のキーワード/値形式の接続文字列を組み立てる。
//
// 値は必ずシングルクォートで囲む。libpq のパーサは "=" の後ろの空白を読み飛ばすため、
// クォートしていない空の値は次のキーごと値として取り込んでしまう。
//
//	"dbname= sslmode=disable"  ->  dbname="sslmode=disable", sslmode=""
//	"password= dbname=mydb"    ->  password="dbname=mydb"
//
// 前者では sslmode が未設定になり、lib/pq の既定（SSLを使う）で接続してしまうため、
// 利用者が disable を選んでいても "SSL is not enabled on the server" になっていた。
// 後者ではパスワードが黙って別物にすり替わる。クォートすれば空値もスペースを含む値も
// 正しく扱える。
func pgDSN(req CheckRequest) (string, error) {
	port := req.Port
	if port == 0 {
		port = 5432
	}

	// PostgreSQL の sslmode は lib/pq がネイティブに解釈する。
	//
	// skip-verify は libpq に無い値だが、以前はこれを disable に寄せていたため
	// 「暗号化はするが検証しない」を要求したのに平文で接続していた。libpq の
	// require がまさにその意味なので、そちらへ対応付ける。
	sslmode, err := resolveSSLMode(req.SSLMode, postgresSSLModes...)
	if err != nil {
		return "", err
	}
	if sslmode == SSLSkipVerify {
		sslmode = SSLRequire
	}

	params := [][2]string{
		{"host", req.Host},
		{"port", strconv.Itoa(port)},
		{"sslmode", sslmode},
		// TimeoutSec をそのまま使うと未指定時に 0 = 無制限になる。
		{"connect_timeout", strconv.Itoa(int(req.Timeout().Seconds()))},
	}

	// 空の任意項目は送らない。libpq 側の既定に委ねたほうが挙動が素直になる。
	for _, kv := range [][2]string{
		{"user", req.Username},
		{"password", req.Password},
		{"dbname", req.Database},
	} {
		if kv[1] != "" {
			params = append(params, kv)
		}
	}

	pairs := make([]string, 0, len(params))
	for _, kv := range params {
		pairs = append(pairs, kv[0]+"="+pgQuote(kv[1]))
	}

	return strings.Join(pairs, " "), nil
}

// pgQuote は libpq の接続文字列用に値をシングルクォートで囲み、
// 値の中のバックスラッシュとシングルクォートをエスケープする。
func pgQuote(value string) string {
	var b strings.Builder

	b.Grow(len(value) + 2)
	b.WriteByte('\'')
	for _, r := range value {
		if r == '\\' || r == '\'' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('\'')

	return b.String()
}

// pgShortVersion は version() の長い文字列を先頭の見出し部分だけに縮める。
// "PostgreSQL 17.10 on x86_64-pc-linux-musl, compiled by ..." のような文字列を
// 単純に20文字で切ると "PostgreSQL 17.10 on " と中途半端に終わるため、
// カンマまで、なければ " on " の手前で切る。
func pgShortVersion(version string) string {
	if i := strings.Index(version, ","); i > 0 {
		version = version[:i]
	}
	if i := strings.Index(version, " on "); i > 0 {
		version = version[:i]
	}

	return strings.TrimSpace(version)
}
