package checker

import (
	"testing"

	"github.com/lib/pq"
)

// parseDSN は lib/pq 自身のパーサで DSN を解釈する。
// 文字列の形を目視で確かめるのではなく、実際にドライバがどう読むかで検証する。
func parseDSN(t *testing.T, dsn string) pq.Config {
	t.Helper()

	cfg, err := pq.NewConfig(dsn)
	if err != nil {
		t.Fatalf("lib/pq failed to parse %q: %v", dsn, err)
	}

	return cfg
}

// mustPGDSN はエラーを起こさない入力を前提にしたテスト用ヘルパ。
func mustPGDSN(t *testing.T, req CheckRequest) string {
	t.Helper()

	dsn, err := pgDSN(req)
	if err != nil {
		t.Fatalf("pgDSN() error = %v, want nil", err)
	}

	return dsn
}

// Database 欄が空のとき、sslmode が失われてはならない。
//
// 修正前は "dbname= sslmode=disable" となり、libpq のパーサが "=" の後ろの空白を
// 読み飛ばして dbname に "sslmode=disable" を取り込んでいた。その結果 sslmode が
// 未設定になり、lib/pq の既定（SSLを使う）で接続するため、利用者が disable を
// 選んでいても "pq: SSL is not enabled on the server" になっていた。
func TestPostgresDSNKeepsSSLModeWhenDatabaseIsEmpty(t *testing.T) {
	dsn := mustPGDSN(t, CheckRequest{
		Host:     "192.168.11.3",
		Port:     5432,
		Username: "postgres",
		Password: "secret",
		Database: "", // 空
		SSLMode:  "disable",
	})

	cfg := parseDSN(t, dsn)

	if cfg.SSLMode != "disable" {
		t.Errorf("SSLMode = %q, want %q (dsn: %s)", cfg.SSLMode, "disable", dsn)
	}
	if cfg.Database != "" {
		t.Errorf("Database = %q, want empty; it must not swallow the next parameter (dsn: %s)",
			cfg.Database, dsn)
	}
}

// Password が空のとき、後続の dbname を飲み込んではならない。
func TestPostgresDSNKeepsPasswordSeparateWhenEmpty(t *testing.T) {
	dsn := mustPGDSN(t, CheckRequest{
		Host:     "192.168.11.3",
		Port:     5432,
		Username: "postgres",
		Password: "", // 空
		Database: "mydb",
		SSLMode:  "disable",
	})

	cfg := parseDSN(t, dsn)

	if cfg.Password != "" {
		t.Errorf("Password = %q, want empty (dsn: %s)", cfg.Password, dsn)
	}
	if cfg.Database != "mydb" {
		t.Errorf("Database = %q, want %q (dsn: %s)", cfg.Database, "mydb", dsn)
	}
	if cfg.SSLMode != "disable" {
		t.Errorf("SSLMode = %q, want %q (dsn: %s)", cfg.SSLMode, "disable", dsn)
	}
}

// すべての項目が空でも DSN が壊れないこと。
func TestPostgresDSNWithOnlyHost(t *testing.T) {
	dsn := mustPGDSN(t, CheckRequest{Host: "192.168.11.3", SSLMode: "disable"})

	cfg := parseDSN(t, dsn)

	if cfg.SSLMode != "disable" {
		t.Errorf("SSLMode = %q, want %q (dsn: %s)", cfg.SSLMode, "disable", dsn)
	}
	if cfg.Port != 5432 {
		t.Errorf("Port = %d, want the default 5432 (dsn: %s)", cfg.Port, dsn)
	}
}

// 資格情報に特殊文字が含まれても、値が壊れたり別のキーとして解釈されたりしない。
func TestPostgresDSNEscapesSpecialCharacters(t *testing.T) {
	tests := []struct {
		name     string
		password string
	}{
		{"スペース", "my secret"},
		{"シングルクォート", "pa'ss"},
		{"バックスラッシュ", `pa\ss`},
		{"key=value に見える文字列", "sslmode=require"},
		{"空白と等号の組み合わせ", "a = b"},
		{"日本語", "パスワード"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsn := mustPGDSN(t, CheckRequest{
				Host:     "192.168.11.3",
				Port:     5432,
				Username: "postgres",
				Password: tt.password,
				Database: "mydb",
				SSLMode:  "disable",
			})

			cfg := parseDSN(t, dsn)

			if cfg.Password != tt.password {
				t.Errorf("Password = %q, want %q (dsn: %s)", cfg.Password, tt.password, dsn)
			}
			if cfg.SSLMode != "disable" {
				t.Errorf("SSLMode = %q, want %q; a crafted password must not change it (dsn: %s)",
					cfg.SSLMode, "disable", dsn)
			}
			if cfg.Database != "mydb" {
				t.Errorf("Database = %q, want %q (dsn: %s)", cfg.Database, "mydb", dsn)
			}
		})
	}
}

func TestPostgresDSNSSLModeMapping(t *testing.T) {
	tests := map[string]string{
		"":            "disable",
		"disable":     "disable",
		"skip-verify": "require", // libpq に無い値だが require が同じ意味（暗号化のみ）
		"require":     "require",
		"verify-ca":   "verify-ca",
		"verify-full": "verify-full",
	}

	for input, want := range tests {
		t.Run("ssl_mode="+input, func(t *testing.T) {
			dsn := mustPGDSN(t, CheckRequest{Host: "h", Database: "d", SSLMode: input})
			if got := parseDSN(t, dsn).SSLMode; string(got) != want {
				t.Errorf("SSLMode = %q, want %q (dsn: %s)", got, want, dsn)
			}
		})
	}
}

// connect_timeout に生の TimeoutSec を使うと未指定時に 0（無制限）になる。
func TestPostgresDSNUsesEffectiveTimeout(t *testing.T) {
	tests := map[int]string{
		0:  "5s", // 未指定はデフォルト5秒
		-1: "5s",
		3:  "3s",
	}

	for timeoutSec, want := range tests {
		dsn := mustPGDSN(t, CheckRequest{Host: "h", TimeoutSec: timeoutSec})
		got := parseDSN(t, dsn).ConnectTimeout
		if got.String() != want {
			t.Errorf("TimeoutSec %d -> connect_timeout %v, want %s (dsn: %s)",
				timeoutSec, got, want, dsn)
		}
	}
}

func TestPGQuote(t *testing.T) {
	tests := map[string]string{
		"":            `''`,
		"plain":       `'plain'`,
		"with space":  `'with space'`,
		`quo'te`:      `'quo\'te'`,
		`back\slash`:  `'back\\slash'`,
		`both'\mixed`: `'both\'\\mixed'`,
	}

	for input, want := range tests {
		if got := pgQuote(input); got != want {
			t.Errorf("pgQuote(%q) = %s, want %s", input, got, want)
		}
	}
}

func TestPGShortVersion(t *testing.T) {
	tests := map[string]string{
		"PostgreSQL 17.10 on x86_64-pc-linux-musl, compiled by gcc (Alpine 14.2.0) 14.2.0, 64-bit": "PostgreSQL 17.10",
		"PostgreSQL 16.2 on x86_64": "PostgreSQL 16.2",
		"PostgreSQL 15.1":           "PostgreSQL 15.1",
		"":                          "",
	}

	for input, want := range tests {
		if got := pgShortVersion(input); got != want {
			t.Errorf("pgShortVersion(%q) = %q, want %q", input, got, want)
		}
	}
}
