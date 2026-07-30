package checker

import (
	"context"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/lib/pq"
)

// 各プロトコルが受け付ける ssl_mode。README の表と一致していなければならない。
var sslModeSupport = map[string][]string{
	"mysql":         {SSLDisable, SSLRequire, SSLSkipVerify, SSLVerifyCA, SSLVerifyFull},
	"postgres":      {SSLDisable, SSLRequire, SSLSkipVerify, SSLVerifyCA, SSLVerifyFull},
	"mongodb":       {SSLDisable, SSLRequire, SSLSkipVerify, SSLVerifyCA, SSLVerifyFull},
	"redis":         {SSLDisable, SSLRequire, SSLSkipVerify, SSLVerifyCA, SSLVerifyFull},
	"elasticsearch": {SSLDisable, SSLRequire, SSLSkipVerify, SSLVerifyCA, SSLVerifyFull},
	// verify-ca は go-mssqldb の DSN パラメータで表現できないため非対応。
	"sqlserver": {SSLDisable, SSLRequire, SSLSkipVerify, SSLVerifyFull},
}

// tlsEnabled は各プロトコルの構築結果から「TLS が有効か」を判定する。
func tlsEnabled(t *testing.T, protocol, sslMode string) (enabled bool, err error) {
	t.Helper()

	req := CheckRequest{Host: "h", SSLMode: sslMode}

	switch protocol {
	case "mysql":
		dsn, err := mysqlDSN(req)
		if err != nil {
			return false, err
		}
		cfg, perr := mysql.ParseDSN(dsn)
		if perr != nil {
			t.Fatalf("the driver could not parse its own DSN %q: %v", dsn, perr)
		}

		return cfg.TLSConfig != "false" && cfg.TLSConfig != "", nil

	case "postgres":
		dsn, err := pgDSN(req)
		if err != nil {
			return false, err
		}
		cfg, perr := pq.NewConfig(dsn)
		if perr != nil {
			t.Fatalf("lib/pq could not parse its own DSN %q: %v", dsn, perr)
		}

		return cfg.SSLMode != pq.SSLModeDisable, nil

	case "mongodb":
		opts, err := mongoOptions(req)
		if err != nil {
			return false, err
		}

		return opts.TLSConfig != nil, nil

	case "redis":
		opts, err := redisOptions(req)
		if err != nil {
			return false, err
		}

		return opts.TLSConfig != nil, nil

	case "sqlserver":
		dsn, err := sqlServerDSN(req)
		if err != nil {
			return false, err
		}

		return !strings.Contains(dsn, "encrypt=disable"), nil

	case "elasticsearch":
		mode, err := resolveSSLMode(sslMode, sslModeSupport["elasticsearch"]...)
		if err != nil {
			return false, err
		}

		return esScheme(mode) == "https", nil
	}

	t.Fatalf("unknown protocol %q", protocol)

	return false, nil
}

// 検証を要求する ssl_mode が、黙って TLS 無効に落ちてはならない。
//
// 修正前は各チェッカーが switch の default で未知の値を「TLS 無効」に扱っていたため、
// verify-full や verify-ca を指定すると平文で接続していた。CLI と README は
// verify-full を共通の値として案内していたため、指定したのに暗号化されないという
// 最も危険な失敗の仕方をしていた。
func TestVerifyingSSLModesNeverSilentlyDisableTLS(t *testing.T) {
	for protocol, supported := range sslModeSupport {
		for _, mode := range []string{SSLRequire, SSLVerifyCA, SSLVerifyFull} {
			t.Run(protocol+"/"+mode, func(t *testing.T) {
				enabled, err := tlsEnabled(t, protocol, mode)

				if !containsMode(supported, mode) {
					// 非対応ならエラーで知らせること。黙って無効化は許さない。
					if err == nil {
						t.Fatalf("ssl_mode %q is not supported by %s but no error was returned (TLS enabled: %v)",
							mode, protocol, enabled)
					}
					if !strings.Contains(err.Error(), mode) {
						t.Errorf("error %q should name the rejected ssl_mode", err.Error())
					}

					return
				}

				if err != nil {
					t.Fatalf("ssl_mode %q is supported by %s but returned an error: %v", mode, protocol, err)
				}
				if !enabled {
					t.Errorf("ssl_mode %q silently produced a plaintext connection for %s", mode, protocol)
				}
			})
		}
	}
}

// skip-verify は「暗号化するが検証しない」であり、TLS を使わないことではない。
// PostgreSQL では以前 disable に寄せていたため平文で接続していた。
func TestSkipVerifyStillUsesTLS(t *testing.T) {
	for protocol, supported := range sslModeSupport {
		if !containsMode(supported, SSLSkipVerify) {
			continue
		}

		t.Run(protocol, func(t *testing.T) {
			enabled, err := tlsEnabled(t, protocol, SSLSkipVerify)
			if err != nil {
				t.Fatalf("error = %v, want nil", err)
			}
			if !enabled {
				t.Errorf("skip-verify produced a plaintext connection for %s", protocol)
			}
		})
	}
}

func TestDisableDoesNotUseTLS(t *testing.T) {
	for protocol := range sslModeSupport {
		for _, mode := range []string{"", SSLDisable} {
			t.Run(protocol+"/"+mode, func(t *testing.T) {
				enabled, err := tlsEnabled(t, protocol, mode)
				if err != nil {
					t.Fatalf("error = %v, want nil", err)
				}
				if enabled {
					t.Errorf("ssl_mode %q enabled TLS for %s, want plaintext", mode, protocol)
				}
			})
		}
	}
}

// タイプミスなど未知の値は必ずエラーにする。以前は黙って TLS 無効になっていた。
func TestUnknownSSLModeIsRejected(t *testing.T) {
	for protocol := range sslModeSupport {
		for _, bogus := range []string{"bogus", "verify_full", "VERIFY-FULL", "true", "yes"} {
			t.Run(protocol+"/"+bogus, func(t *testing.T) {
				enabled, err := tlsEnabled(t, protocol, bogus)
				if err == nil {
					t.Fatalf("ssl_mode %q was accepted by %s without error (TLS enabled: %v)",
						bogus, protocol, enabled)
				}
				if !strings.Contains(err.Error(), bogus) {
					t.Errorf("error %q should name the offending value", err.Error())
				}
			})
		}
	}
}

func TestResolveSSLMode(t *testing.T) {
	all := []string{SSLDisable, SSLRequire, SSLSkipVerify, SSLVerifyCA, SSLVerifyFull}

	t.Run("空文字は disable", func(t *testing.T) {
		got, err := resolveSSLMode("", all...)
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if got != SSLDisable {
			t.Errorf("resolveSSLMode(\"\") = %q, want %q", got, SSLDisable)
		}
	})

	t.Run("対応値はそのまま返る", func(t *testing.T) {
		for _, mode := range all {
			got, err := resolveSSLMode(mode, all...)
			if err != nil || got != mode {
				t.Errorf("resolveSSLMode(%q) = %q, %v; want %q, nil", mode, got, err, mode)
			}
		}
	})

	t.Run("既知だが非対応の値はエラーで対応値を示す", func(t *testing.T) {
		_, err := resolveSSLMode(SSLVerifyCA, SSLDisable, SSLRequire)
		if err == nil {
			t.Fatal("error = nil, want an error")
		}
		for _, want := range []string{SSLVerifyCA, "not supported", SSLRequire} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q should contain %q", err.Error(), want)
			}
		}
	})

	t.Run("未知の値はエラー", func(t *testing.T) {
		_, err := resolveSSLMode("nonsense", all...)
		if err == nil {
			t.Fatal("error = nil, want an error")
		}
		if !strings.Contains(err.Error(), "unknown") {
			t.Errorf("error %q should say the value is unknown", err.Error())
		}
	})
}

func containsMode(modes []string, mode string) bool {
	for _, m := range modes {
		if m == mode {
			return true
		}
	}

	return false
}

// ---------- uri 経路 ----------

// uri を渡すチェッカーの一覧と、そのプロトコルで妥当な uri。
var uriCapableCheckers = map[string]string{
	"mysql":         "app:pw@tcp(h:3306)/db",
	"postgres":      "host='h' port='5432' sslmode='disable'",
	"mongodb":       "mongodb://h:27017",
	"redis":         "redis://h:6379",
	"sqlserver":     "sqlserver://app:pw@h:1433",
	"elasticsearch": "http://h:9200",
}

// checkWithURI は uri を指定して Check を呼び、返ってきたエラー文字列を返す。
func checkWithURI(t *testing.T, protocol, uri, sslMode string) string {
	t.Helper()

	res, err := Dispatch(context.Background(), CheckRequest{
		Type:       protocol,
		URI:        uri,
		SSLMode:    sslMode,
		TimeoutSec: 1,
	})
	if err != nil {
		t.Fatalf("Dispatch() error = %v, want nil", err)
	}

	return res.Error
}

// uri を指定しても ssl_mode の値そのものは検証されなければならない。
// 修正前は uri があると検証を通らず、タイプミスが黙って無視されていた。
func TestUnknownSSLModeIsRejectedEvenWithURI(t *testing.T) {
	for protocol, uri := range uriCapableCheckers {
		for _, bogus := range []string{"verify_full", "bogus", "VERIFY-FULL"} {
			t.Run(protocol+"/"+bogus, func(t *testing.T) {
				got := checkWithURI(t, protocol, uri, bogus)

				if !strings.Contains(got, bogus) {
					t.Errorf("error = %q, want it to reject the ssl_mode %q instead of proceeding to connect",
						got, bogus)
				}
				if !strings.Contains(got, "ssl_mode") {
					t.Errorf("error = %q, want it to mention ssl_mode", got)
				}
			})
		}
	}
}

// TLS を要求するモードと uri の併用は、黙って平文接続になる代わりにエラーになる。
// 修正前は uri 側が平文なら平文で接続していた（redis:// + verify-full など）。
func TestTLSRequestingSSLModeWithURIIsRejected(t *testing.T) {
	for protocol, uri := range uriCapableCheckers {
		for _, mode := range []string{SSLRequire, SSLVerifyFull} {
			if !containsMode(sslModeSupport[protocol], mode) {
				continue
			}

			t.Run(protocol+"/"+mode, func(t *testing.T) {
				got := checkWithURI(t, protocol, uri, mode)

				if got == "" {
					t.Fatalf("no error; ssl_mode %q with a plaintext uri must not silently connect", mode)
				}
				if !strings.Contains(got, "ssl_mode") || !strings.Contains(got, mode) {
					t.Errorf("error = %q, want it to explain the ssl_mode/uri conflict", got)
				}
			})
		}
	}
}

// uri と disable の併用は許す。uri が TLS の有無を決めるため矛盾しない。
func TestDisableWithURIIsAllowed(t *testing.T) {
	for protocol, uri := range uriCapableCheckers {
		t.Run(protocol, func(t *testing.T) {
			for _, mode := range []string{"", SSLDisable} {
				got := checkWithURI(t, protocol, uri, mode)

				// 到達できないので接続エラーにはなるが、ssl_mode の拒否ではないこと。
				if strings.Contains(got, "ssl_mode") {
					t.Errorf("ssl_mode %q with a uri was rejected (%q); the URI should govern TLS", mode, got)
				}
			}
		})
	}
}

// Elasticsearch は uri がスキームを、ssl_mode が検証方式を与えるため
// https の uri との併用には意味がある。
func TestElasticsearchHTTPSURIAcceptsVerificationMode(t *testing.T) {
	for _, mode := range []string{SSLSkipVerify, SSLVerifyCA, SSLVerifyFull} {
		t.Run(mode, func(t *testing.T) {
			got := checkWithURI(t, "elasticsearch", "https://h:9200", mode)

			if strings.Contains(got, "ssl_mode") {
				t.Errorf("ssl_mode %q with an https uri was rejected (%q); it should supply the verification policy",
					mode, got)
			}
		})
	}
}
