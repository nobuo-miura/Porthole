package checker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func esRequest(t *testing.T, srv *httptest.Server, sslMode string) CheckResult {
	t.Helper()

	return (&ElasticsearchChecker{}).Check(context.Background(), CheckRequest{
		Type:       "elasticsearch",
		URI:        srv.URL,
		SSLMode:    sslMode,
		TimeoutSec: 5,
	})
}

func TestElasticsearchHealthyClusterIsOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_cluster/health" {
			t.Errorf("requested path = %q, want /_cluster/health", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cluster_name":"prod","status":"green","number_of_nodes":3}`))
	}))
	defer srv.Close()

	got := esRequest(t, srv, "disable")

	if !got.Success {
		t.Fatalf("Success = false, want true (error: %s)", got.Error)
	}
	for _, want := range []string{"prod", "green", "3"} {
		if !strings.Contains(got.Detail, want) {
			t.Errorf("Detail = %q, want it to contain %q", got.Detail, want)
		}
	}
}

// 以前は 401 が成功として報告されていた（ES の status が数値のため
// デコードが失敗し、"HTTP 401" が err=nil で返っていた）。
func TestElasticsearchUnauthorizedIsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		// 実際の ES のエラー形状: status は数値。
		_, _ = w.Write([]byte(`{"error":{"type":"security_exception","reason":"missing authentication credentials"},"status":401}`))
	}))
	defer srv.Close()

	got := esRequest(t, srv, "disable")

	if got.Success {
		t.Fatalf("Success = true, want false for HTTP 401 (detail: %s)", got.Detail)
	}
	if got.Outcome != OutcomeFailed {
		t.Errorf("Outcome = %q, want %q", got.Outcome, OutcomeFailed)
	}
	if !strings.Contains(got.Error, "401") {
		t.Errorf("Error = %q, want it to mention the status code", got.Error)
	}
	if !strings.Contains(strings.ToLower(got.Error), "authentication") {
		t.Errorf("Error = %q, want it to identify this as an authentication failure", got.Error)
	}
	if !strings.Contains(got.Error, "missing authentication credentials") {
		t.Errorf("Error = %q, want the reason from the response body", got.Error)
	}
}

func TestElasticsearchForbiddenIsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	if got := esRequest(t, srv, "disable"); got.Success {
		t.Errorf("Success = true, want false for HTTP 403")
	}
}

func TestElasticsearchServerErrorIsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	got := esRequest(t, srv, "disable")

	if got.Success {
		t.Fatalf("Success = true, want false for HTTP 503")
	}
	if !strings.Contains(got.Error, "503") {
		t.Errorf("Error = %q, want it to mention the status code", got.Error)
	}
}

// プロキシが HTML のエラーページを返すケース。非 2xx なら失敗であるべき。
func TestElasticsearchNonJSONErrorPageIsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`<html><body>502 Bad Gateway</body></html>`))
	}))
	defer srv.Close()

	if got := esRequest(t, srv, "disable"); got.Success {
		t.Errorf("Success = true, want false for an HTML 502 page")
	}
}

// 2xx でも Elasticsearch とは限らない。プロキシの既定ページ、認証ポータル、
// 別サービスなども 200 を返すため、判定不能として扱う。
// 特に「JSONだが ES ではない」ケースは以前 unmarshal が成功してしまい、
// ゼロ値の "Cluster: , Status: , Nodes: 0" を成功として報告していた。
func TestElasticsearch2xxThatIsNotElasticsearchIsIndeterminate(t *testing.T) {
	bodies := map[string]string{
		"プロキシのHTML":       `<html><body>Welcome to nginx</body></html>`,
		"認証ポータルのJSON":     `{"login":"required","redirect":"/sso"}`,
		"別サービスのAPI":       `{"service":"payments","version":"3.1"}`,
		"status が不正な値":    `{"cluster_name":"x","status":"online","number_of_nodes":1}`,
		"cluster_name 欠落": `{"status":"green","number_of_nodes":1}`,
		"cluster_name が空": `{"cluster_name":"","status":"green"}`,
	}

	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()

			got := esRequest(t, srv, "disable")

			if got.Outcome != OutcomeIndeterminate {
				t.Fatalf("Outcome = %q, want %q (detail: %q)", got.Outcome, OutcomeIndeterminate, got.Detail)
			}
			if got.Success {
				t.Error("Success = true, want false: reaching Elasticsearch was not established")
			}
			if strings.Contains(got.Detail, "Cluster: ,") {
				t.Errorf("Detail = %q, must not present zero values as a real cluster", got.Detail)
			}
		})
	}
}

// number_of_nodes が無くても、cluster_name と妥当な status があれば ES と判定する。
func TestElasticsearchMinimalHealthDocumentIsOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"cluster_name":"prod","status":"yellow"}`))
	}))
	defer srv.Close()

	got := esRequest(t, srv, "disable")

	if got.Outcome != OutcomeOK {
		t.Fatalf("Outcome = %q, want %q (error: %s, detail: %s)",
			got.Outcome, OutcomeOK, got.Error, got.Detail)
	}
}

func TestParseESHealth(t *testing.T) {
	tests := []struct {
		name string
		body string
		ok   bool
	}{
		{"green", `{"cluster_name":"c","status":"green","number_of_nodes":3}`, true},
		{"yellow", `{"cluster_name":"c","status":"yellow"}`, true},
		{"red", `{"cluster_name":"c","status":"red"}`, true},
		{"unknown status value", `{"cluster_name":"c","status":"up"}`, false},
		{"status missing", `{"cluster_name":"c"}`, false},
		{"cluster_name missing", `{"status":"green"}`, false},
		{"empty object", `{}`, false},
		{"not json", `<html>`, false},
		{"json array", `[]`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := parseESHealth([]byte(tt.body))
			if ok != tt.ok {
				t.Errorf("parseESHealth(%s) ok = %v, want %v", tt.body, ok, tt.ok)
			}
		})
	}
}

// リダイレクトを追うと Basic 認証ヘッダが別ホストへ渡る恐れがあるため追わない。
func TestElasticsearchDoesNotFollowRedirects(t *testing.T) {
	var reachedElsewhere bool

	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reachedElsewhere = true
		_, _ = w.Write([]byte(`{"cluster_name":"attacker","status":"green","number_of_nodes":1}`))
	}))
	defer elsewhere.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/_cluster/health", http.StatusFound)
	}))
	defer srv.Close()

	got := esRequest(t, srv, "disable")

	if reachedElsewhere {
		t.Error("the checker followed a redirect to another host; credentials could leak")
	}
	if got.Success {
		t.Errorf("Success = true, want false when the server redirects (detail: %s)", got.Detail)
	}
}

// TLS 検証をスキップする指定のときだけ自己署名証明書を受け入れる。
func TestElasticsearchTLSVerification(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"cluster_name":"tls","status":"green","number_of_nodes":1}`))
	}))
	defer srv.Close()

	t.Run("skip-verify は自己署名を受け入れる", func(t *testing.T) {
		if got := esRequest(t, srv, "skip-verify"); !got.Success {
			t.Errorf("Success = false, want true with skip-verify (error: %s)", got.Error)
		}
	})

	t.Run("require は自己署名を拒否する", func(t *testing.T) {
		got := esRequest(t, srv, "require")
		if got.Success {
			t.Error("Success = true, want false: an untrusted certificate must fail when verification is required")
		}
	})
}

func TestESScheme(t *testing.T) {
	tests := map[string]string{
		"":            "http",
		"disable":     "http",
		"require":     "https",
		"skip-verify": "https",
		"verify-ca":   "https",
		"verify-full": "https",
	}

	for sslMode, want := range tests {
		if got := esScheme(sslMode); got != want {
			t.Errorf("esScheme(%q) = %q, want %q", sslMode, got, want)
		}
	}
}

func TestESReason(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"object form", `{"error":{"type":"security_exception","reason":"unauthenticated"},"status":401}`, ": unauthenticated (security_exception)"},
		{"string form", `{"error":"nope"}`, ": nope"},
		{"reason only", `{"error":{"reason":"just because"}}`, ": just because"},
		{"no error field", `{"status":401}`, ""},
		{"not json", `<html>`, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := esReason([]byte(tt.body)); got != tt.want {
				t.Errorf("esReason() = %q, want %q", got, tt.want)
			}
		})
	}
}
