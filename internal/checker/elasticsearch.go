package checker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// esMaxBodyBytes は読み取るレスポンスボディの上限。
// 診断に必要なのは先頭だけなので、巨大なレスポンスで詰まらないよう制限する。
const esMaxBodyBytes = 64 * 1024

type ElasticsearchChecker struct{}

// Check は /_cluster/health を叩いて Elasticsearch の到達性を確認する。
//
// 以前はステータスコードを一切見ておらず、401 でも成功として報告していた。
// ES の 401 ボディは status が数値なので構造体（string）へのデコードが失敗し、
// フォールバックの "HTTP 401" が err=nil で返っていたのが原因。
func (c *ElasticsearchChecker) Check(ctx context.Context, req CheckRequest) CheckResult {
	return RunProbe(ctx, req, func(ctx context.Context) (Probe, error) {
		mode, err := resolveSSLMode(req.SSLMode, elasticsearchSSLModes...)
		if err != nil {
			return Probe{}, err
		}

		endpoint := req.URI
		if endpoint == "" {
			endpoint = esScheme(mode) + "://" + req.Addr(9200)
		}

		// Elasticsearch では uri がスキームを、ssl_mode が検証方式を与えるため
		// https の uri と ssl_mode の併用には意味がある。ただし http の uri に
		// TLS を要求するモードを重ねるのは矛盾なので、黙って平文にせずエラーにする。
		if req.URI != "" && mode != SSLDisable && strings.HasPrefix(endpoint, "http://") {
			return Probe{}, fmt.Errorf(
				"ssl_mode %q requires TLS but the uri is plain http; use an https:// uri "+
					"(or set ssl_mode to disable)", mode)
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
			strings.TrimSuffix(endpoint, "/")+"/_cluster/health", nil)
		if err != nil {
			return Probe{}, err
		}
		if req.Username != "" {
			httpReq.SetBasicAuth(req.Username, req.Password)
		}

		client, err := esClient(mode)
		if err != nil {
			return Probe{}, err
		}
		defer client.CloseIdleConnections()

		resp, err := client.Do(httpReq)
		if err != nil {
			return Probe{}, err
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(io.LimitReader(resp.Body, esMaxBodyBytes))
		if err != nil {
			return Probe{}, err
		}

		switch {
		case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
			return Probe{}, fmt.Errorf("authentication failed (HTTP %d)%s", resp.StatusCode, esReason(body))

		case resp.StatusCode < 200 || resp.StatusCode > 299:
			return Probe{}, fmt.Errorf("unexpected response (HTTP %d)%s", resp.StatusCode, esReason(body))
		}

		// 2xx だけでは Elasticsearch に到達した証拠にならない。プロキシの既定ページ、
		// 認証ポータル、別サービスなども 200 を返す。しかも構造体への
		// デコードは「JSONではあるが ES ではない」ボディでも成功してしまい、
		// ゼロ値のまま "Cluster: , Status: , Nodes: 0" を成功として報告していた。
		// クラスタヘルスとして辻褄が合うことを確認できない限り判定不能とする。
		health, ok := parseESHealth(body)
		if !ok {
			return Probe{
				Outcome: OutcomeIndeterminate,
				Detail: fmt.Sprintf(
					"HTTP %d, but the response is not an Elasticsearch cluster health document. "+
						"Something answered on this port (a proxy, an auth portal or another service) "+
						"and it is not possible to tell whether Elasticsearch itself was reached.",
					resp.StatusCode),
			}, nil
		}

		return Probe{
			Outcome: OutcomeOK,
			Detail: fmt.Sprintf("Cluster: %s, Status: %s, Nodes: %d",
				health.ClusterName, health.Status, health.NumberOfNodes),
		}, nil
	})
}

// esHealth は /_cluster/health のレスポンス。
type esHealth struct {
	Status        string
	ClusterName   string
	NumberOfNodes int
}

// parseESHealth はボディがクラスタヘルスとして妥当なら解析結果と true を返す。
//
// 必須項目の有無だけでなく status の値も検証する。ES のクラスタヘルスの status は
// 必ず green / yellow / red のいずれかなので、他サービスの 200 レスポンスと
// 区別する識別子として使える。
func parseESHealth(body []byte) (esHealth, bool) {
	var parsed struct {
		Status        *string `json:"status"`
		ClusterName   *string `json:"cluster_name"`
		NumberOfNodes *int    `json:"number_of_nodes"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return esHealth{}, false
	}
	if parsed.ClusterName == nil || *parsed.ClusterName == "" || parsed.Status == nil {
		return esHealth{}, false
	}

	switch *parsed.Status {
	case "green", "yellow", "red":
	default:
		return esHealth{}, false
	}

	health := esHealth{Status: *parsed.Status, ClusterName: *parsed.ClusterName}
	if parsed.NumberOfNodes != nil {
		health.NumberOfNodes = *parsed.NumberOfNodes
	}

	return health, true
}

// esScheme は ssl_mode に対応するスキームを返す。
// 通常は resolveSSLMode で正規化した値を渡すが、空文字も disable として扱う。
// 以前は ssl_mode を無視して常に http:// を組み立てていた。
func esScheme(mode string) string {
	if mode == "" || mode == SSLDisable {
		return "http"
	}

	return "https"
}

// esClient は ssl_mode に応じた HTTP クライアントを返す。
// リダイレクトは追わない。追うと Basic 認証ヘッダを別ホストへ送ってしまう恐れがあり、
// 診断としてもリダイレクトされた事実を報告したほうが正確。
func esClient(mode string) (*http.Client, error) {
	// roots は nil でシステムの信頼ストアを使う。
	tlsConfig, err := tlsConfigForMode(mode, nil)
	if err != nil {
		return nil, err
	}

	return &http.Client{
		CheckRedirect: func(r *http.Request, _ []*http.Request) error {
			return fmt.Errorf("redirected to %s (not followed)", r.URL.Redacted())
		},
		Transport: &http.Transport{TLSClientConfig: tlsConfig},
	}, nil
}

// esReason はエラーボディから理由を1行で取り出す。取れなければ空文字を返す。
func esReason(body []byte) string {
	var parsed struct {
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || len(parsed.Error) == 0 {
		return ""
	}

	// error は文字列の場合もオブジェクトの場合もある。
	var asString string
	if err := json.Unmarshal(parsed.Error, &asString); err == nil {
		return ": " + asString
	}

	var asObject struct {
		Type   string `json:"type"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(parsed.Error, &asObject); err != nil {
		return ""
	}

	switch {
	case asObject.Reason != "" && asObject.Type != "":
		return fmt.Sprintf(": %s (%s)", asObject.Reason, asObject.Type)
	case asObject.Reason != "":
		return ": " + asObject.Reason
	case asObject.Type != "":
		return ": " + asObject.Type
	}

	return ""
}
