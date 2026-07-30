package checker

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Outcome はチェック結果の三値分類。
//
// Success は真偽値しか表せないが、UDP のようにコネクションレスなプロトコルでは
// 「開いている」と「フィルタされている」を区別できない場合がある。そこを
// 成功として報告すると到達性の証拠が無いのに OK に見えてしまうため、
// 「判定不能」を独立した状態として持つ。
const (
	// OutcomeOK は到達性（必要なら認証まで）の肯定的な証拠が得られたことを示す。
	OutcomeOK = "ok"
	// OutcomeFailed は確定的な失敗を示す（接続拒否、認証失敗など）。
	OutcomeFailed = "failed"
	// OutcomeIndeterminate はどちらとも判断できないことを示す。
	// 例: UDP に送信したが応答もICMPエラーも返ってこなかった。
	OutcomeIndeterminate = "indeterminate"
)

// CheckResult holds the result of a single connection check.
type CheckResult struct {
	// Success は Outcome == OutcomeOK と同義。既存APIの互換のために残している。
	Success bool `json:"success"`
	// Outcome は ok / failed / indeterminate のいずれか。
	Outcome   string    `json:"outcome"`
	Type      string    `json:"type"`
	Host      string    `json:"host,omitempty"`
	Port      int       `json:"port,omitempty"`
	LatencyMs int64     `json:"latency_ms"`
	Detail    string    `json:"detail,omitempty"`
	Error     string    `json:"error,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

// Probe は1回のプロトコル試行の結果。Outcome が空なら OutcomeOK として扱う。
type Probe struct {
	Detail  string
	Outcome string
}

// CheckRequest is the input for a connection check.
type CheckRequest struct {
	Type       string `json:"type"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Username   string `json:"username,omitempty"`
	Password   string `json:"password,omitempty"`
	Database   string `json:"database,omitempty"`
	URI        string `json:"uri,omitempty"`
	SSLMode    string `json:"ssl_mode,omitempty"` // disable | require | verify-ca | verify-full | skip-verify
	TimeoutSec int    `json:"timeout_sec,omitempty"`
}

// Checker is the interface all protocol checkers implement.
type Checker interface {
	Check(ctx context.Context, req CheckRequest) CheckResult
}

// ssl_mode の取り得る値。
const (
	SSLDisable    = "disable"     // TLS を使わない
	SSLRequire    = "require"     // TLS を使い、チェーンとホスト名を検証する
	SSLSkipVerify = "skip-verify" // TLS を使うが検証しない
	SSLVerifyCA   = "verify-ca"   // チェーンは検証するがホスト名は検証しない
	SSLVerifyFull = "verify-full" // チェーンとホスト名を検証する
)

// 各プロトコルが受け付ける ssl_mode。README の対応表と一致していなければならない。
var (
	mySQLSSLModes         = []string{SSLDisable, SSLRequire, SSLSkipVerify, SSLVerifyCA, SSLVerifyFull}
	postgresSSLModes      = []string{SSLDisable, SSLRequire, SSLSkipVerify, SSLVerifyCA, SSLVerifyFull}
	mongoSSLModes         = []string{SSLDisable, SSLRequire, SSLSkipVerify, SSLVerifyCA, SSLVerifyFull}
	redisSSLModes         = []string{SSLDisable, SSLRequire, SSLSkipVerify, SSLVerifyCA, SSLVerifyFull}
	elasticsearchSSLModes = []string{SSLDisable, SSLRequire, SSLSkipVerify, SSLVerifyCA, SSLVerifyFull}
	// verify-ca（チェーンのみ検証）は go-mssqldb の DSN パラメータで表現できない。
	sqlServerSSLModes = []string{SSLDisable, SSLRequire, SSLSkipVerify, SSLVerifyFull}
)

// checkSSLModeAgainstURI は uri を使うチェッカー向けの ssl_mode 検証。
//
// これらのプロトコルでは uri が TLS の設定も内包するため、ssl_mode を後から
// 重ねる方法が無い。以前は uri が指定されていると ssl_mode の検証自体を
// 通らず、タイプミスが黙って無視され、さらに verify-full を指定しても
// uri 側が平文なら平文で接続していた。
//
// そこで値そのものは常に検証し、TLS を要求するモードと uri の併用は
// 矛盾した入力としてエラーにする。uri に TLS 設定を書くよう促す。
func checkSSLModeAgainstURI(req CheckRequest, supported ...string) error {
	mode, err := resolveSSLMode(req.SSLMode, supported...)
	if err != nil {
		return err
	}

	if req.URI != "" && mode != SSLDisable {
		return fmt.Errorf(
			"ssl_mode %q cannot be combined with uri: the URI carries its own TLS settings, "+
				"so put them in the URI instead (or clear the URI to use the individual fields)", mode)
	}

	return nil
}

// resolveSSLMode は ssl_mode を検証し、正規化した値を返す。
// 空文字は SSLDisable として扱う。supported に無い値はエラーにする。
//
// 以前は各チェッカーが switch の default で未知の値を「TLS 無効」に落としていた。
// そのため verify-full や verify-ca を指定しても、さらにはタイプミスでも、
// 黙って暗号化なしで接続していた。検証を要求したはずが平文になるのは
// 最も避けたい失敗の仕方なので、対応していない値は必ずエラーにする。
func resolveSSLMode(sslMode string, supported ...string) (string, error) {
	if sslMode == "" {
		sslMode = SSLDisable
	}

	if slices.Contains(supported, sslMode) {
		return sslMode, nil
	}

	if slices.Contains([]string{SSLDisable, SSLRequire, SSLSkipVerify, SSLVerifyCA, SSLVerifyFull}, sslMode) {
		return "", fmt.Errorf("ssl_mode %q is not supported for this protocol (supported: %s)",
			sslMode, strings.Join(supported, ", "))
	}

	return "", fmt.Errorf("unknown ssl_mode %q (supported: %s)", sslMode, strings.Join(supported, ", "))
}

// tlsConfigForMode は正規化済みの ssl_mode に対応する TLS 設定を返す。
// SSLDisable の場合は nil を返す（呼び出し側は TLS を使わない）。
//
// roots が nil ならシステムの信頼ストアを使う（テストから差し替えるための引数）。
func tlsConfigForMode(mode string, roots *x509.CertPool) (*tls.Config, error) {
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    roots,
	}

	switch mode {
	case SSLDisable:
		return nil, nil

	case SSLRequire, SSLVerifyFull:
		// Go 標準の検証（チェーン + ホスト名）。
		return cfg, nil

	case SSLSkipVerify:
		// 利用者が明示的に選択した場合のみ。
		cfg.InsecureSkipVerify = true //nolint:gosec // opt-in

		return cfg, nil

	case SSLVerifyCA:
		// Go 標準の検証はホスト名まで見るので無効化し、チェーンのみ自前で検証する。
		cfg.InsecureSkipVerify = true //nolint:gosec // 下の VerifyPeerCertificate で検証する
		cfg.VerifyPeerCertificate = verifyChainIgnoringHostname(roots)

		return cfg, nil

	default:
		return nil, fmt.Errorf("unknown ssl_mode %q", mode)
	}
}

// verifyChainIgnoringHostname はチェーンのみを検証するコールバックを返す。
// x509.VerifyOptions の DNSName を空にすることでホスト名検証を省く。
func verifyChainIgnoringHostname(roots *x509.CertPool) func([][]byte, [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		if len(rawCerts) == 0 {
			return errors.New("server presented no certificate")
		}

		certs := make([]*x509.Certificate, 0, len(rawCerts))
		for _, raw := range rawCerts {
			cert, err := x509.ParseCertificate(raw)
			if err != nil {
				return fmt.Errorf("failed to parse server certificate: %w", err)
			}
			certs = append(certs, cert)
		}

		opts := x509.VerifyOptions{
			Roots:         roots, // nil ならシステムの信頼ストア
			Intermediates: x509.NewCertPool(),
			// DNSName は意図的に空。これが verify-ca と verify-full の違い。
		}
		for _, cert := range certs[1:] {
			opts.Intermediates.AddCert(cert)
		}

		if _, err := certs[0].Verify(opts); err != nil {
			return fmt.Errorf("certificate chain verification failed: %w", err)
		}

		return nil
	}
}

// Timeout returns the effective timeout duration for a request.
func (r CheckRequest) Timeout() time.Duration {
	if r.TimeoutSec <= 0 {
		return 5 * time.Second
	}
	return time.Duration(r.TimeoutSec) * time.Second
}

// Addr はホストとポートを結合したアドレスを返す。defaultPort は Port が 0 のときに使う。
//
// fmt.Sprintf("%s:%d", ...) だと IPv6 アドレスで壊れる（"::1:6379" のようになり、
// どこまでがホストか判別できない）。net.JoinHostPort は必要に応じて角括弧で囲む。
func (r CheckRequest) Addr(defaultPort int) string {
	port := r.Port
	if port == 0 {
		port = defaultPort
	}

	return net.JoinHostPort(r.Host, strconv.Itoa(port))
}

// PortOr は Port が 0 のときに defaultPort を返す。
func (r CheckRequest) PortOr(defaultPort int) int {
	if r.Port == 0 {
		return defaultPort
	}

	return r.Port
}

// Run executes a check and records timing.
// 成功なら OutcomeOK、エラーなら OutcomeFailed になる。
// 判定不能を返し得るチェッカーは RunProbe を使うこと。
func Run(ctx context.Context, req CheckRequest, fn func(ctx context.Context) (string, error)) CheckResult {
	return RunProbe(ctx, req, func(ctx context.Context) (Probe, error) {
		detail, err := fn(ctx)
		return Probe{Detail: detail}, err
	})
}

// RunProbe executes a check that can report an indeterminate outcome, and records timing.
func RunProbe(ctx context.Context, req CheckRequest, fn func(ctx context.Context) (Probe, error)) CheckResult {
	start := time.Now()
	probe, err := fn(ctx)
	latency := time.Since(start).Milliseconds()

	result := CheckResult{
		Type:      req.Type,
		Host:      req.Host,
		Port:      req.Port,
		LatencyMs: latency,
		CheckedAt: time.Now(),
	}

	switch {
	case err != nil:
		result.Outcome = OutcomeFailed
		result.Error = err.Error()
	case probe.Outcome == "" || probe.Outcome == OutcomeOK:
		result.Outcome = OutcomeOK
		result.Detail = probe.Detail
	default:
		result.Outcome = probe.Outcome
		result.Detail = probe.Detail
	}

	// Success は「肯定的な証拠が得られた」ことのみを意味する。
	// 判定不能は false（証拠が無いものを OK とは報告しない）。
	result.Success = result.Outcome == OutcomeOK

	return result
}
