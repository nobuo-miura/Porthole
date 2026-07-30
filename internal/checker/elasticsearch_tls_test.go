package checker

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// tlsServerWithHostnameMismatch は自己署名TLSサーバを立て、
// 「証明書のSANに含まれないホスト名」でのアクセスURLと、その証明書を信頼するプールを返す。
//
// httptest の証明書は SAN に example.com と 127.0.0.1 を持つが localhost を持たない。
// そのため 127.0.0.1 に解決される localhost 経由でアクセスすると、
// チェーンは辿れるがホスト名検証だけが失敗する状況を作れる。
func tlsServerWithHostnameMismatch(t *testing.T) (url string, trusted *x509.CertPool) {
	t.Helper()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"cluster_name":"tls","status":"green","number_of_nodes":1}`))
	}))
	t.Cleanup(srv.Close)

	_, port, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "https://"))
	if err != nil {
		t.Fatalf("failed to parse %q: %v", srv.URL, err)
	}

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())

	// 証明書は自己署名なので、リーフをルートとして信頼すればチェーンは検証できる。
	return "https://localhost:" + port, pool
}

// tlsConfigForMode2 はエラーを起こさない入力を前提にしたテスト用ヘルパ。
func tlsConfigForMode2(t *testing.T, mode string, roots *x509.CertPool) *tls.Config {
	t.Helper()

	cfg, err := tlsConfigForMode(mode, roots)
	if err != nil {
		t.Fatalf("tlsConfigForMode(%q) error = %v, want nil", mode, err)
	}

	return cfg
}

// get は指定の ssl_mode と信頼ストアで実際にTLS接続を試みる。
func get(t *testing.T, url, sslMode string, roots *x509.CertPool) error {
	t.Helper()

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfigForMode2(t, sslMode, roots)}}
	defer client.CloseIdleConnections()

	resp, err := client.Get(url) //nolint:noctx // テスト内の単発リクエスト
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

// 4つのモードが実際に異なる挙動になることを固定する。
// 修正前は skip-verify 以外がすべて同一（チェーン + ホスト名検証）だった。
func TestESTLSModesDifferInBehaviour(t *testing.T) {
	url, trusted := tlsServerWithHostnameMismatch(t)

	tests := []struct {
		sslMode   string
		roots     *x509.CertPool
		wantError bool
		why       string
	}{
		{
			sslMode: "skip-verify", roots: nil, wantError: false,
			why: "検証しないので信頼していない証明書でも通る",
		},
		{
			sslMode: "verify-ca", roots: trusted, wantError: false,
			why: "チェーンは辿れる。ホスト名不一致は verify-ca では無視する",
		},
		{
			sslMode: "verify-ca", roots: nil, wantError: true,
			why: "ホスト名は見ないが、チェーンが信頼できないので失敗する",
		},
		{
			sslMode: "verify-full", roots: trusted, wantError: true,
			why: "チェーンは辿れるがホスト名が一致しないので失敗する",
		},
		{
			sslMode: "require", roots: trusted, wantError: true,
			why: "require は verify-full と同じ扱い",
		},
	}

	for _, tt := range tests {
		name := tt.sslMode + "/システムルート"
		if tt.roots != nil {
			name = tt.sslMode + "/テストCAを信頼"
		}

		t.Run(name, func(t *testing.T) {
			err := get(t, url, tt.sslMode, tt.roots)

			if tt.wantError && err == nil {
				t.Errorf("error = nil, want an error: %s", tt.why)
			}
			if !tt.wantError && err != nil {
				t.Errorf("error = %v, want nil: %s", err, tt.why)
			}
		})
	}
}

// verify-ca と verify-full が同一挙動でないことを直接対比する。
// この2つが同じ結果になったら、モードの実装が退行している。
func TestESVerifyCADiffersFromVerifyFull(t *testing.T) {
	url, trusted := tlsServerWithHostnameMismatch(t)

	caErr := get(t, url, "verify-ca", trusted)
	fullErr := get(t, url, "verify-full", trusted)

	if caErr != nil {
		t.Errorf("verify-ca failed (%v); it must ignore the hostname mismatch", caErr)
	}
	if fullErr == nil {
		t.Error("verify-full succeeded; it must reject a hostname mismatch")
	}
}

func TestTLSConfigForModeMinVersion(t *testing.T) {
	for _, mode := range []string{"", "disable", "require", "skip-verify", "verify-ca", "verify-full"} {
		if mode == "" || mode == SSLDisable {
			continue // disable は nil を返す
		}
		cfg := tlsConfigForMode2(t, mode, nil)
		if cfg.MinVersion != tls.VersionTLS12 {
			t.Errorf("tlsConfigForMode(%q).MinVersion = %#x, want TLS 1.2", mode, cfg.MinVersion)
		}
	}
}

func TestTLSConfigForModeSkipsVerificationOnlyWhenAsked(t *testing.T) {
	tests := map[string]bool{
		"require":     false,
		"verify-full": false,
		"skip-verify": true,
		// verify-ca は Go 標準の検証を無効化して自前で検証するため true になる。
		// 検証していないわけではないので、VerifyPeerCertificate の有無で担保する。
		"verify-ca": true,
	}

	for mode, wantSkip := range tests {
		cfg := tlsConfigForMode2(t, mode, nil)
		if cfg.InsecureSkipVerify != wantSkip {
			t.Errorf("tlsConfigForMode(%q).InsecureSkipVerify = %v, want %v", mode, cfg.InsecureSkipVerify, wantSkip)
		}

		hasCallback := cfg.VerifyPeerCertificate != nil
		if mode == SSLVerifyCA && !hasCallback {
			t.Error(`tlsConfigForMode("verify-ca") must install a chain-verifying callback`)
		}
		if mode == SSLSkipVerify && hasCallback {
			t.Error(`tlsConfigForMode("skip-verify") must not verify anything`)
		}
	}
}
