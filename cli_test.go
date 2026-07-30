package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/nobuo-miura/porthole/internal/checker"
)

func listenLocal(t *testing.T) int {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	return ln.Addr().(*net.TCPAddr).Port
}

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

func TestCLIExitCode(t *testing.T) {
	tests := []struct {
		name     string
		outcomes []string
		want     int
	}{
		{"no results", nil, exitOK},
		{"all ok", []string{checker.OutcomeOK, checker.OutcomeOK}, exitOK},
		{"one failure", []string{checker.OutcomeOK, checker.OutcomeFailed}, exitFailed},
		{"ok and indeterminate", []string{checker.OutcomeOK, checker.OutcomeIndeterminate}, exitIndeterminate},
		{"failure outranks indeterminate", []string{checker.OutcomeIndeterminate, checker.OutcomeFailed}, exitFailed},
		{"only indeterminate", []string{checker.OutcomeIndeterminate}, exitIndeterminate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := make([]checker.CheckResult, len(tt.outcomes))
			for i, o := range tt.outcomes {
				results[i] = checker.CheckResult{Outcome: o}
			}
			if got := cliExitCode(results); got != tt.want {
				t.Errorf("cliExitCode(%v) = %d, want %d", tt.outcomes, got, tt.want)
			}
		})
	}
}

func TestCLIRequestsFromFlags(t *testing.T) {
	t.Run("host なしはエラー", func(t *testing.T) {
		_, err := cliRequests(false, nil, checker.CheckRequest{Type: "tcp"})
		if err == nil {
			t.Fatal("error = nil, want an error when neither --host nor --uri is given")
		}
	})

	t.Run("type なしはエラー", func(t *testing.T) {
		_, err := cliRequests(false, nil, checker.CheckRequest{Host: "example.test"})
		if err == nil {
			t.Fatal("error = nil, want an error when --type is missing")
		}
	})

	t.Run("uri だけでも通る", func(t *testing.T) {
		got, err := cliRequests(false, nil, checker.CheckRequest{Type: "redis", URI: "redis://x"})
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if len(got) != 1 || got[0].URI != "redis://x" {
			t.Errorf("got %+v, want a single request carrying the URI", got)
		}
	})
}

func TestCLIRequestsFromStdin(t *testing.T) {
	t.Run("配列", func(t *testing.T) {
		in := strings.NewReader(`[{"type":"tcp","host":"a","port":1},{"type":"tcp","host":"b","port":2}]`)
		got, err := cliRequests(true, in, checker.CheckRequest{})
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if len(got) != 2 || got[0].Host != "a" || got[1].Host != "b" {
			t.Errorf("got %+v, want two requests in order", got)
		}
	})

	t.Run("単一オブジェクト", func(t *testing.T) {
		in := strings.NewReader(`{"type":"tcp","host":"a","port":1}`)
		got, err := cliRequests(true, in, checker.CheckRequest{})
		if err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
		if len(got) != 1 || got[0].Host != "a" {
			t.Errorf("got %+v, want one request", got)
		}
	})

	for _, tt := range []struct{ name, input string }{
		{"空", ``},
		{"空配列", `[]`},
		{"不正なJSON", `not json`},
		{"type 欠落", `{"host":"a"}`},
		{"host と uri の両方が欠落", `{"type":"tcp"}`},
		{"配列内の type 欠落", `[{"host":"a","port":1}]`},
		{"配列内の host 欠落", `[{"type":"tcp","port":1}]`},
	} {
		t.Run(tt.name+"はエラー", func(t *testing.T) {
			if _, err := cliRequests(true, strings.NewReader(tt.input), checker.CheckRequest{}); err == nil {
				t.Errorf("error = nil, want an error for input %q", tt.input)
			}
		})
	}
}

// 配列で不正な要素があった場合、何番目かを示す必要がある。
func TestCLIRequestsStdinErrorNamesTheIndex(t *testing.T) {
	in := strings.NewReader(`[{"type":"tcp","host":"a","port":1},{"type":"tcp","port":2}]`)

	_, err := cliRequests(true, in, checker.CheckRequest{})
	if err == nil {
		t.Fatal("error = nil, want an error for the second element")
	}
	if !strings.Contains(err.Error(), "checks[1]") {
		t.Errorf("error = %q, want it to identify the offending index", err.Error())
	}
}

// stdin 経由のリクエストにも PORTHOLE_PASSWORD が適用されること。
// 以前はフラグ入力にしか適用されていなかった。
func TestCLIRequestsStdinInheritsPasswordFromEnv(t *testing.T) {
	t.Setenv("PORTHOLE_PASSWORD", "from-env")

	in := strings.NewReader(`[{"type":"mysql","host":"a","port":3306},{"type":"mysql","host":"b","port":3306}]`)
	got, err := cliRequests(true, in, checker.CheckRequest{})
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}

	for i, req := range got {
		if req.Password != "from-env" {
			t.Errorf("requests[%d].Password = %q, want %q", i, req.Password, "from-env")
		}
	}
}

// JSON に明示されたパスワードを環境変数で上書きしてはいけない。
func TestCLIRequestsStdinPasswordInJSONWins(t *testing.T) {
	t.Setenv("PORTHOLE_PASSWORD", "from-env")

	in := strings.NewReader(`{"type":"mysql","host":"a","port":3306,"password":"in-json"}`)
	got, err := cliRequests(true, in, checker.CheckRequest{})
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if got[0].Password != "in-json" {
		t.Errorf("Password = %q, want %q", got[0].Password, "in-json")
	}
}

// 入力の不備は「実行して失敗した」(1) ではなく usage error (3) として返す。
func TestRunCLIInvalidStdinExitsThree(t *testing.T) {
	for _, input := range []string{
		`[{"host":"db.internal","port":5432}]`,
		`{"type":"tcp"}`,
	} {
		t.Run(input, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := runCLI([]string{"--stdin"}, strings.NewReader(input), &stdout, &stderr)

			if code != exitUsage {
				t.Errorf("exit code = %d, want %d (stderr: %s)", code, exitUsage, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty: nothing should have run", stdout.String())
			}
		})
	}
}

// パスワードは argv に置くと他プロセスから見えるため、環境変数を使えること。
func TestCLIPassword(t *testing.T) {
	t.Run("環境変数から読む", func(t *testing.T) {
		t.Setenv("PORTHOLE_PASSWORD", "from-env")
		if got := cliPassword(""); got != "from-env" {
			t.Errorf("cliPassword(\"\") = %q, want %q", got, "from-env")
		}
	})

	t.Run("フラグが環境変数より優先", func(t *testing.T) {
		t.Setenv("PORTHOLE_PASSWORD", "from-env")
		if got := cliPassword("from-flag"); got != "from-flag" {
			t.Errorf("cliPassword() = %q, want %q", got, "from-flag")
		}
	})

	t.Run("どちらも無ければ空", func(t *testing.T) {
		t.Setenv("PORTHOLE_PASSWORD", "")
		if got := cliPassword(""); got != "" {
			t.Errorf("cliPassword() = %q, want empty", got)
		}
	})
}

func TestRunCLIReachablePortExitsZero(t *testing.T) {
	port := listenLocal(t)

	var stdout, stderr bytes.Buffer
	code := runCLI([]string{"--type", "tcp", "--host", "127.0.0.1", "--port", fmt.Sprint(port)},
		nil, &stdout, &stderr)

	if code != exitOK {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitOK, stderr.String())
	}
	if !strings.Contains(stdout.String(), "OK") {
		t.Errorf("stdout = %q, want it to report OK", stdout.String())
	}
}

func TestRunCLIClosedPortExitsOne(t *testing.T) {
	port := closedLocalPort(t)

	var stdout, stderr bytes.Buffer
	code := runCLI([]string{"--type", "tcp", "--host", "127.0.0.1", "--port", fmt.Sprint(port)},
		nil, &stdout, &stderr)

	if code != exitFailed {
		t.Fatalf("exit code = %d, want %d", code, exitFailed)
	}
	if !strings.Contains(stdout.String(), "FAIL") {
		t.Errorf("stdout = %q, want it to report FAIL", stdout.String())
	}
}

func TestRunCLIUsageErrorExitsThree(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runCLI([]string{"--type", "tcp"}, nil, &stdout, &stderr)

	if code != exitUsage {
		t.Fatalf("exit code = %d, want %d", code, exitUsage)
	}
	if stderr.Len() == 0 {
		t.Error("stderr is empty, want an explanation of the usage error")
	}
}

func TestRunCLIUnknownFlagExitsThree(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runCLI([]string{"--nope"}, nil, &stdout, &stderr); code != exitUsage {
		t.Errorf("exit code = %d, want %d", code, exitUsage)
	}
}

func TestRunCLIUnknownTypeExitsOne(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runCLI([]string{"--type", "nosuchdb", "--host", "example.test"}, nil, &stdout, &stderr)

	if code != exitFailed {
		t.Fatalf("exit code = %d, want %d", code, exitFailed)
	}
	if !strings.Contains(stdout.String(), "nosuchdb") {
		t.Errorf("stdout = %q, want it to name the unknown type", stdout.String())
	}
}

func TestRunCLIJSONSingleResultIsAnObject(t *testing.T) {
	port := listenLocal(t)

	var stdout, stderr bytes.Buffer
	runCLI([]string{"--json", "--type", "tcp", "--host", "127.0.0.1", "--port", fmt.Sprint(port)},
		nil, &stdout, &stderr)

	var one checker.CheckResult
	if err := json.Unmarshal(stdout.Bytes(), &one); err != nil {
		t.Fatalf("a single check should emit a JSON object; got %q (%v)", stdout.String(), err)
	}
	if one.Outcome != checker.OutcomeOK {
		t.Errorf("outcome = %q, want %q", one.Outcome, checker.OutcomeOK)
	}
}

func TestRunCLIJSONMultipleResultsIsAnArray(t *testing.T) {
	open, closed := listenLocal(t), closedLocalPort(t)

	in := strings.NewReader(fmt.Sprintf(
		`[{"type":"tcp","host":"127.0.0.1","port":%d},{"type":"tcp","host":"127.0.0.1","port":%d}]`,
		open, closed))

	var stdout, stderr bytes.Buffer
	code := runCLI([]string{"--stdin", "--json"}, in, &stdout, &stderr)

	if code != exitFailed {
		t.Errorf("exit code = %d, want %d (one check failed)", code, exitFailed)
	}

	var many []checker.CheckResult
	if err := json.Unmarshal(stdout.Bytes(), &many); err != nil {
		t.Fatalf("multiple checks should emit a JSON array; got %q (%v)", stdout.String(), err)
	}
	if len(many) != 2 {
		t.Fatalf("got %d results, want 2", len(many))
	}
	if many[0].Outcome != checker.OutcomeOK || many[1].Outcome != checker.OutcomeFailed {
		t.Errorf("outcomes = [%q %q], want [ok failed]; results must stay in request order",
			many[0].Outcome, many[1].Outcome)
	}
}

func TestCLILabel(t *testing.T) {
	tests := map[string]string{
		checker.OutcomeOK:            "OK",
		checker.OutcomeFailed:        "FAIL",
		checker.OutcomeIndeterminate: "UNKNOWN",
		"":                           "FAIL",
	}

	for outcome, want := range tests {
		if got := cliLabel(outcome); got != want {
			t.Errorf("cliLabel(%q) = %q, want %q", outcome, got, want)
		}
	}
}

// --help は使い方の要求であってエラーではない。以前は終了コード 3 を返していた。
func TestRunCLIHelpExitsZero(t *testing.T) {
	for _, arg := range []string{"--help", "-h"} {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := runCLI([]string{arg}, nil, &stdout, &stderr)

			if code != exitOK {
				t.Errorf("exit code = %d, want %d", code, exitOK)
			}
			// 使い方は stdout に出す（パイプで受け取れるように）。
			if !strings.Contains(stdout.String(), "Usage: porthole check") {
				t.Errorf("stdout = %q, want the usage text", stdout.String())
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty for an explicit help request", stderr.String())
			}
		})
	}
}

// 不正なフラグは usage error のまま。使い方は stderr に出す。
func TestRunCLIUnknownFlagWritesToStderr(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runCLI([]string{"--nope"}, nil, &stdout, &stderr)

	if code != exitUsage {
		t.Errorf("exit code = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "nope") {
		t.Errorf("stderr = %q, want it to name the unknown flag", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty on a usage error", stdout.String())
	}
}
