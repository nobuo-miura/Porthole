package api

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/nobuo-miura/porthole/internal/checker"
)

func result(detail string) checker.CheckResult {
	return checker.CheckResult{Success: true, Type: "tcp", Detail: detail}
}

func details(rs []checker.CheckResult) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Detail
	}

	return out
}

func TestHistoryKeepsNewestUpToMax(t *testing.T) {
	h := NewHistory(3)
	for _, d := range []string{"a", "b", "c", "d", "e"} {
		h.Add(result(d))
	}

	got := details(h.All())
	want := []string{"c", "d", "e"}

	if len(got) != len(want) {
		t.Fatalf("history length = %d (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("history = %v, want %v (oldest first, newest last)", got, want)
		}
	}
}

func TestHistoryUnderMax(t *testing.T) {
	h := NewHistory(10)
	h.Add(result("a"))
	h.Add(result("b"))

	if got := details(h.All()); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("history = %v, want [a b]", got)
	}
}

// max が 0 以下だと以前はパニックしていた（slice bounds out of range [1:0]）。
// HISTORY_SIZE=0 は docker-compose から指定できるため、無効化として扱う。
func TestHistoryDisabledDoesNotPanic(t *testing.T) {
	for _, max := range []int{0, -1} {
		t.Run(fmt.Sprintf("max=%d", max), func(t *testing.T) {
			h := NewHistory(max)
			h.Add(result("a"))
			h.Add(result("b"))

			if got := h.All(); len(got) != 0 {
				t.Errorf("history = %v, want empty when disabled", details(got))
			}
		})
	}
}

func TestHistoryAllReturnsCopy(t *testing.T) {
	h := NewHistory(2)
	h.Add(result("original"))

	snapshot := h.All()
	snapshot[0].Detail = "mutated"

	if got := h.All()[0].Detail; got != "original" {
		t.Errorf("internal state = %q, want %q; All() must return a copy", got, "original")
	}
}

// -race 付きで実行するとロックの欠落を検出できる。
func TestHistoryConcurrentAccess(t *testing.T) {
	h := NewHistory(50)

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			h.Add(result(strconv.Itoa(i)))
		}(i)
		go func() {
			defer wg.Done()
			_ = h.All()
		}()
	}
	wg.Wait()

	if got := len(h.All()); got != 50 {
		t.Errorf("history length = %d, want 50", got)
	}
}

func TestHealthReportsVersion(t *testing.T) {
	h := New(NewHistory(5), "v1.2.3")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode %q: %v", rec.Body.String(), err)
	}
	if body["status"] != "ok" {
		t.Errorf("status field = %q, want %q", body["status"], "ok")
	}
	if body["version"] != "v1.2.3" {
		t.Errorf("version field = %q, want %q", body["version"], "v1.2.3")
	}
}

func TestNewFallsBackToDevVersion(t *testing.T) {
	h := New(NewHistory(5), "")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode %q: %v", rec.Body.String(), err)
	}
	if body["version"] != "dev" {
		t.Errorf("version field = %q, want %q", body["version"], "dev")
	}
}

func TestCheckRejectsMalformedJSON(t *testing.T) {
	h := New(NewHistory(5), "test")

	req := httptest.NewRequest(http.MethodPost, "/api/check", strings.NewReader("{not json"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "error") {
		t.Errorf("body = %q, want an error field", rec.Body.String())
	}
}

func TestCheckRejectsUnknownType(t *testing.T) {
	h := New(NewHistory(5), "test")

	req := httptest.NewRequest(http.MethodPost, "/api/check", strings.NewReader(`{"type":"nosuchdb"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	// 不明な種別はチェックが走っていないので履歴に残してはならない。
	if got := len(h.history.All()); got != 0 {
		t.Errorf("history length = %d, want 0 for a rejected request", got)
	}
}

func TestCheckWrongMethod(t *testing.T) {
	h := New(NewHistory(5), "test")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/check", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

// listenLocal は待ち受け中のローカルポートを返す。
func listenLocal(t *testing.T) int {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	return ln.Addr().(*net.TCPAddr).Port
}

// closedLocalPort は誰も待ち受けていないローカルポートを返す。
func closedLocalPort(t *testing.T) int {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatalf("failed to close listener: %v", err)
	}

	return port
}

func TestCheckSuccessIsRecorded(t *testing.T) {
	h := New(NewHistory(5), "test")
	port := listenLocal(t)

	body := fmt.Sprintf(`{"type":"tcp","host":"127.0.0.1","port":%d}`, port)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/check", strings.NewReader(body)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got checker.CheckResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode %q: %v", rec.Body.String(), err)
	}
	if !got.Success {
		t.Errorf("Success = false, want true (error: %s)", got.Error)
	}

	if n := len(h.history.All()); n != 1 {
		t.Errorf("history length = %d, want 1", n)
	}
}

func TestHistoryEndpointReturnsRecordedChecks(t *testing.T) {
	h := New(NewHistory(5), "test")
	h.history.Add(result("first"))
	h.history.Add(result("second"))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/history", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body struct {
		Checks []checker.CheckResult `json:"checks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode %q: %v", rec.Body.String(), err)
	}
	if got := details(body.Checks); len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Errorf("checks = %v, want [first second]", got)
	}
}

func TestBatchPreservesRequestOrder(t *testing.T) {
	h := New(NewHistory(10), "test")
	open := listenLocal(t)
	closed := closedLocalPort(t)

	// 1件目は成功、2件目は失敗するはず。並行実行でも入力順で返ること。
	body := fmt.Sprintf(
		`{"checks":[{"type":"tcp","host":"127.0.0.1","port":%d},{"type":"tcp","host":"127.0.0.1","port":%d}]}`,
		open, closed)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/check/batch", strings.NewReader(body)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got struct {
		Results []checker.CheckResult `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode %q: %v", rec.Body.String(), err)
	}
	if len(got.Results) != 2 {
		t.Fatalf("results length = %d, want 2", len(got.Results))
	}
	if !got.Results[0].Success {
		t.Errorf("results[0].Success = false, want true (open port %d, error: %s)", open, got.Results[0].Error)
	}
	if got.Results[1].Success {
		t.Errorf("results[1].Success = true, want false (closed port %d)", closed)
	}
	if got.Results[0].Port != open || got.Results[1].Port != closed {
		t.Errorf("ports = [%d %d], want [%d %d]; results must stay index-aligned with the request",
			got.Results[0].Port, got.Results[1].Port, open, closed)
	}

	if n := len(h.history.All()); n != 2 {
		t.Errorf("history length = %d, want 2", n)
	}
}

func TestBatchRejectsMalformedJSON(t *testing.T) {
	h := New(NewHistory(5), "test")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/check/batch", strings.NewReader("[")))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestBatchWithEmptyListSucceeds(t *testing.T) {
	h := New(NewHistory(5), "test")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/check/batch", strings.NewReader(`{"checks":[]}`)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if n := len(h.history.All()); n != 0 {
		t.Errorf("history length = %d, want 0", n)
	}
}
