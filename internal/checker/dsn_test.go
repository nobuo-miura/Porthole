package checker

import (
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// 接続情報を含む値の代表例。区切り文字として解釈されうる文字を並べている。
var nastyCredentials = []struct {
	name  string
	value string
}{
	{"アットマーク", "p@ssw0rd"},
	{"スラッシュ", "pa/ss"},
	{"コロン", "pa:ss"},
	{"疑問符とアンパサンド", "pa?ss&more=1"},
	{"ホスト偽装を狙う文字列", "x@evil.example:1234/"},
	{"スペース", "my secret"},
	{"パーセント", "100%pass"},
	{"日本語", "パスワード"},
}

// mustMySQLDSN / mustMongoOptions はエラーを起こさない入力を前提にしたテスト用ヘルパ。
func mustMySQLDSN(t *testing.T, req CheckRequest) string {
	t.Helper()

	dsn, err := mysqlDSN(req)
	if err != nil {
		t.Fatalf("mysqlDSN() error = %v, want nil", err)
	}

	return dsn
}

func mustMongoOptions(t *testing.T, req CheckRequest) *options.ClientOptions {
	t.Helper()

	opts, err := mongoOptions(req)
	if err != nil {
		t.Fatalf("mongoOptions() error = %v, want nil", err)
	}

	return opts
}

// ---------- MySQL ----------

// パスワードに "@" や "/" が含まれても、接続先やユーザ名がすり替わらないこと。
// 修正前は fmt.Sprintf での連結だったため区切りと誤認されていた。
func TestMySQLDSNEscapesCredentials(t *testing.T) {
	for _, tt := range nastyCredentials {
		t.Run(tt.name, func(t *testing.T) {
			dsn := mustMySQLDSN(t, CheckRequest{
				Host:     "db.internal",
				Port:     3306,
				Username: "app",
				Password: tt.value,
				Database: "mydb",
			})

			cfg, err := mysql.ParseDSN(dsn)
			if err != nil {
				t.Fatalf("the driver could not parse its own DSN %q: %v", dsn, err)
			}

			if cfg.Passwd != tt.value {
				t.Errorf("Passwd = %q, want %q (dsn: %s)", cfg.Passwd, tt.value, dsn)
			}
			if cfg.User != "app" {
				t.Errorf("User = %q, want %q (dsn: %s)", cfg.User, "app", dsn)
			}
			if cfg.Addr != "db.internal:3306" {
				t.Errorf("Addr = %q, want %q; a crafted password must not redirect the connection (dsn: %s)",
					cfg.Addr, "db.internal:3306", dsn)
			}
			if cfg.DBName != "mydb" {
				t.Errorf("DBName = %q, want %q (dsn: %s)", cfg.DBName, "mydb", dsn)
			}
		})
	}
}

func TestMySQLDSNUsernameAndDatabaseEscaping(t *testing.T) {
	dsn := mustMySQLDSN(t, CheckRequest{
		Host:     "db.internal",
		Username: "user@corp",
		Password: "pw",
		Database: "my db/name",
	})

	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("the driver could not parse its own DSN %q: %v", dsn, err)
	}
	if cfg.User != "user@corp" {
		t.Errorf("User = %q, want %q", cfg.User, "user@corp")
	}
	if cfg.DBName != "my db/name" {
		t.Errorf("DBName = %q, want %q", cfg.DBName, "my db/name")
	}
}

func TestMySQLDSNTLSAndTimeout(t *testing.T) {
	tests := map[string]string{
		"":            "false",
		"disable":     "false",
		"require":     "true",
		"skip-verify": "skip-verify",
	}

	for sslMode, wantTLS := range tests {
		t.Run("ssl_mode="+sslMode, func(t *testing.T) {
			cfg, err := mysql.ParseDSN(mustMySQLDSN(t, CheckRequest{Host: "h", SSLMode: sslMode}))
			if err != nil {
				t.Fatalf("parse failed: %v", err)
			}
			if cfg.TLSConfig != wantTLS {
				t.Errorf("TLSConfig = %q, want %q", cfg.TLSConfig, wantTLS)
			}
			// 未指定の TimeoutSec が 0（無制限）にならないこと。
			if cfg.Timeout != 5*time.Second {
				t.Errorf("Timeout = %v, want 5s", cfg.Timeout)
			}
		})
	}
}

func TestMySQLDSNIPv6(t *testing.T) {
	cfg, err := mysql.ParseDSN(mustMySQLDSN(t, CheckRequest{Host: "::1", Port: 3306}))
	if err != nil {
		t.Fatalf("the driver could not parse its own DSN: %v", err)
	}
	if cfg.Addr != "[::1]:3306" {
		t.Errorf("Addr = %q, want %q", cfg.Addr, "[::1]:3306")
	}
}

// ---------- MongoDB ----------

// 認証情報を URI に埋め込まず SetAuth で渡すので、特殊文字が入っても
// 接続先がすり替わらないこと。
func TestMongoOptionsEscapesCredentials(t *testing.T) {
	for _, tt := range nastyCredentials {
		t.Run(tt.name, func(t *testing.T) {
			opts := mustMongoOptions(t, CheckRequest{
				Host:     "mongo.internal",
				Port:     27017,
				Username: "app",
				Password: tt.value,
			})

			if len(opts.Hosts) != 1 || opts.Hosts[0] != "mongo.internal:27017" {
				t.Errorf("Hosts = %v, want [mongo.internal:27017]; a crafted password must not redirect the connection",
					opts.Hosts)
			}
			if opts.Auth == nil {
				t.Fatal("Auth is nil, want credentials to be set")
			}
			if opts.Auth.Password != tt.value {
				t.Errorf("Auth.Password = %q, want %q", opts.Auth.Password, tt.value)
			}
			if opts.Auth.Username != "app" {
				t.Errorf("Auth.Username = %q, want %q", opts.Auth.Username, "app")
			}
		})
	}
}

// username だけ指定してパスワードが空のとき、以前は認証情報が丸ごと無視され
// 匿名接続になっていた（&& での判定だったため）。
func TestMongoOptionsAuthWithEmptyPassword(t *testing.T) {
	opts := mustMongoOptions(t, CheckRequest{Host: "h", Username: "app"})

	if opts.Auth == nil {
		t.Fatal("Auth is nil; a username alone must still attempt authentication")
	}
	if opts.Auth.Username != "app" {
		t.Errorf("Auth.Username = %q, want %q", opts.Auth.Username, "app")
	}
	if !opts.Auth.PasswordSet {
		t.Error("PasswordSet = false; an empty password must be sent as an empty password")
	}
}

func TestMongoOptionsNoAuthWhenNoUsername(t *testing.T) {
	opts := mustMongoOptions(t, CheckRequest{Host: "h", Password: "orphan"})

	if opts.Auth != nil {
		t.Errorf("Auth = %+v, want nil when no username is given", opts.Auth)
	}
}

func TestMongoOptionsIPv6AndDefaultPort(t *testing.T) {
	opts := mustMongoOptions(t, CheckRequest{Host: "::1"})

	if len(opts.Hosts) != 1 || opts.Hosts[0] != "[::1]:27017" {
		t.Errorf("Hosts = %v, want [[::1]:27017]", opts.Hosts)
	}
}

func TestMongoOptionsURITakesPrecedence(t *testing.T) {
	opts := mustMongoOptions(t, CheckRequest{
		URI:  "mongodb://explicit.example:27018",
		Host: "ignored.example",
	})

	if len(opts.Hosts) != 1 || opts.Hosts[0] != "explicit.example:27018" {
		t.Errorf("Hosts = %v, want the URI to win", opts.Hosts)
	}
}

// ---------- RabbitMQ ----------

func TestAMQPURIEscapesCredentials(t *testing.T) {
	for _, tt := range nastyCredentials {
		t.Run(tt.name, func(t *testing.T) {
			uri := amqpURI(CheckRequest{
				Host:     "mq.internal",
				Port:     5672,
				Username: "app",
				Password: tt.value,
			})

			parsed, err := amqp.ParseURI(uri)
			if err != nil {
				t.Fatalf("the driver could not parse its own URI %q: %v", uri, err)
			}

			if parsed.Password != tt.value {
				t.Errorf("Password = %q, want %q (uri: %s)", parsed.Password, tt.value, uri)
			}
			if parsed.Username != "app" {
				t.Errorf("Username = %q, want %q (uri: %s)", parsed.Username, "app", uri)
			}
			if parsed.Host != "mq.internal" || parsed.Port != 5672 {
				t.Errorf("target = %s:%d, want mq.internal:5672; a crafted password must not redirect the connection (uri: %s)",
					parsed.Host, parsed.Port, uri)
			}
		})
	}
}

func TestAMQPURIDefaultsToGuest(t *testing.T) {
	parsed, err := amqp.ParseURI(amqpURI(CheckRequest{Host: "mq.internal"}))
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	if parsed.Username != "guest" || parsed.Password != "guest" {
		t.Errorf("credentials = %s/%s, want guest/guest", parsed.Username, parsed.Password)
	}
	if parsed.Port != 5672 {
		t.Errorf("Port = %d, want the default 5672", parsed.Port)
	}
}

func TestAMQPURIIPv6(t *testing.T) {
	uri := amqpURI(CheckRequest{Host: "::1", Port: 5672})

	parsed, err := amqp.ParseURI(uri)
	if err != nil {
		t.Fatalf("the driver could not parse its own URI %q: %v", uri, err)
	}
	if parsed.Host != "::1" {
		t.Errorf("Host = %q, want %q (uri: %s)", parsed.Host, "::1", uri)
	}
}

// ---------- 共通のアドレス組み立て ----------

func TestCheckRequestAddr(t *testing.T) {
	tests := []struct {
		name        string
		host        string
		port        int
		defaultPort int
		want        string
	}{
		{"IPv4", "10.0.0.1", 5432, 5432, "10.0.0.1:5432"},
		{"ホスト名", "db.internal", 3306, 3306, "db.internal:3306"},
		{"IPv6は角括弧で囲む", "::1", 6379, 6379, "[::1]:6379"},
		{"IPv6の完全表記", "2001:db8::1", 27017, 27017, "[2001:db8::1]:27017"},
		{"ポート未指定は既定値", "h", 0, 9200, "h:9200"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CheckRequest{Host: tt.host, Port: tt.port}.Addr(tt.defaultPort)
			if got != tt.want {
				t.Errorf("Addr(%d) = %q, want %q", tt.defaultPort, got, tt.want)
			}
		})
	}
}

func TestCheckRequestPortOr(t *testing.T) {
	if got := (CheckRequest{Port: 1234}).PortOr(5432); got != 1234 {
		t.Errorf("PortOr = %d, want 1234", got)
	}
	if got := (CheckRequest{}).PortOr(5432); got != 5432 {
		t.Errorf("PortOr = %d, want the default 5432", got)
	}
}
