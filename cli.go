package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/nobuo-miura/porthole/internal/checker"
)

// CLI の終了コード。CI/CD から結果を分岐できるよう三値をそのまま反映する。
const (
	exitOK            = 0 // すべて到達性を確認できた
	exitFailed        = 1 // 1件以上が確定的に失敗した
	exitIndeterminate = 2 // 失敗は無いが判定不能が残った（UDP の無応答など）
	exitUsage         = 3 // 引数や入力が不正
)

const cliUsage = `Usage: porthole check [flags]

Run connection checks without starting the web server, and exit with a status
code that reflects the result. Intended for ECS Exec sessions and CI/CD.

Flags:
  --type string        Checker type (tcp, udp, mysql, postgres, redis, ...)
  --host string        Target host
  --port int           Target port
  --username string    Username
  --password string    Password (prefer PORTHOLE_PASSWORD; argv is visible to other processes)
  --database string    Database name
  --uri string         Full connection URI (overrides host/port/credentials)
  --ssl-mode string    disable | skip-verify | verify-ca | verify-full | require
                       Not every protocol supports every value; an unsupported or
                       misspelled value is rejected rather than silently downgraded.
                       verify-ca is unavailable for sqlserver. For postgres, require
                       means "encrypt without verifying" (libpq semantics).
  --timeout int        Timeout in seconds (default 5)
  --stdin              Read a CheckRequest object, or an array of them, from stdin
  --json               Emit results as JSON

Environment:
  PORTHOLE_PASSWORD    Used when --password is not given

Exit codes:
  0  ok             reachability confirmed
  1  failed         at least one check definitively failed
  2  indeterminate  no failures, but at least one result proves nothing
  3  usage          bad arguments or input

Examples:
  porthole check --type tcp --host db.internal --port 5432
  porthole check --type postgres --host db.internal --port 5432 --username app --database app
  echo '[{"type":"tcp","host":"a","port":80},{"type":"tcp","host":"b","port":443}]' \
    | porthole check --stdin --json
`

// runCLI は check サブコマンドを実行し、終了コードを返す。
// テストしやすいよう入出力を引数で受け取る。
func runCLI(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	// flag 自身の出力は捨て、成功時（--help）は stdout、失敗時は stderr へ
	// 出し分ける。flag に任せると --help も stderr になってしまう。
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}

	var (
		typ      = fs.String("type", "", "checker type")
		host     = fs.String("host", "", "target host")
		port     = fs.Int("port", 0, "target port")
		username = fs.String("username", "", "username")
		password = fs.String("password", "", "password")
		database = fs.String("database", "", "database name")
		uri      = fs.String("uri", "", "full connection URI")
		sslMode  = fs.String("ssl-mode", "", "SSL mode")
		timeout  = fs.Int("timeout", 0, "timeout in seconds")
		useStdin = fs.Bool("stdin", false, "read requests from stdin")
		asJSON   = fs.Bool("json", false, "emit JSON")
	)

	if err := fs.Parse(args); err != nil {
		// --help はエラーではなく正常な要求。使い方を stdout に出して 0 で終わる。
		if errors.Is(err, flag.ErrHelp) {
			_, _ = fmt.Fprint(stdout, cliUsage)

			return exitOK
		}

		_, _ = fmt.Fprintf(stderr, "porthole: %v\n\n", err)
		_, _ = fmt.Fprint(stderr, cliUsage)

		return exitUsage
	}

	requests, err := cliRequests(*useStdin, stdin, checker.CheckRequest{
		Type:       *typ,
		Host:       *host,
		Port:       *port,
		Username:   *username,
		Password:   *password,
		Database:   *database,
		URI:        *uri,
		SSLMode:    *sslMode,
		TimeoutSec: *timeout,
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "porthole: %v\n", err)
		return exitUsage
	}

	results := make([]checker.CheckResult, 0, len(requests))
	for _, req := range requests {
		res, err := checker.Dispatch(context.Background(), req)
		if err != nil {
			// 未知の種別などディスパッチ自体の失敗。
			res = checker.CheckResult{
				Type:      req.Type,
				Host:      req.Host,
				Port:      req.Port,
				Outcome:   checker.OutcomeFailed,
				Error:     err.Error(),
				CheckedAt: time.Now(),
			}
		}
		results = append(results, res)
	}

	if err := writeCLIResults(stdout, results, *asJSON); err != nil {
		_, _ = fmt.Fprintf(stderr, "porthole: failed to write results: %v\n", err)
		return exitFailed
	}

	return cliExitCode(results)
}

// cliRequests は stdin かフラグからチェック要求の一覧を組み立てる。
// 入力元に関わらず、全リクエストに同じ検証とパスワード補完を適用する。
func cliRequests(useStdin bool, stdin io.Reader, fromFlags checker.CheckRequest) ([]checker.CheckRequest, error) {
	requests := []checker.CheckRequest{fromFlags}
	if useStdin {
		var err error
		if requests, err = readStdinRequests(stdin); err != nil {
			return nil, err
		}
	}

	for i := range requests {
		// パスワードが空なら環境変数で補う。stdin の JSON をリポジトリに
		// 置いたまま、資格情報だけ環境から渡せるようにするため。
		requests[i].Password = cliPassword(requests[i].Password)

		if err := validateRequest(requests[i], i, useStdin); err != nil {
			return nil, err
		}
	}

	return requests, nil
}

// readStdinRequests は stdin の JSON を読む。配列と単一オブジェクトの両方を受け付ける。
func readStdinRequests(stdin io.Reader) ([]checker.CheckRequest, error) {
	raw, err := io.ReadAll(stdin)
	if err != nil {
		return nil, fmt.Errorf("failed to read stdin: %w", err)
	}
	if len(raw) == 0 {
		return nil, errors.New("stdin is empty")
	}

	var many []checker.CheckRequest
	if err := json.Unmarshal(raw, &many); err == nil {
		if len(many) == 0 {
			return nil, errors.New("stdin contained an empty array")
		}

		return many, nil
	}

	var one checker.CheckRequest
	if err := json.Unmarshal(raw, &one); err != nil {
		return nil, fmt.Errorf("stdin is not a CheckRequest object or array: %w", err)
	}

	return []checker.CheckRequest{one}, nil
}

// validateRequest は要求が実行可能な形かを確認する。
// これを通さないと、type や host の欠落が「実行してみたら失敗した」(exit 1) として
// 扱われ、入力ミス (exit 3) と区別できなくなる。
func validateRequest(req checker.CheckRequest, index int, fromStdin bool) error {
	// フラグ入力とJSON入力で指摘の書き方を変える。
	missing := func(flag, field string) error {
		if fromStdin {
			return fmt.Errorf("checks[%d]: %q is required", index, field)
		}

		return fmt.Errorf("%s is required", flag)
	}

	if req.Type == "" {
		if !fromStdin {
			return errors.New("--type is required (or use --stdin)")
		}

		return missing("--type", "type")
	}
	if req.Host == "" && req.URI == "" {
		if fromStdin {
			return fmt.Errorf("checks[%d]: %q or %q is required", index, "host", "uri")
		}

		return errors.New("--host or --uri is required")
	}

	return nil
}

// cliPassword は値が空なら環境変数を使う。
// argv は他プロセスから見えるため、環境変数のほうが安全。
func cliPassword(current string) string {
	if current != "" {
		return current
	}

	return os.Getenv("PORTHOLE_PASSWORD")
}

func writeCLIResults(w io.Writer, results []checker.CheckResult, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if len(results) == 1 {
			return enc.Encode(results[0])
		}

		return enc.Encode(results)
	}

	for _, r := range results {
		target := r.Host
		if r.Port != 0 {
			target = fmt.Sprintf("%s:%d", r.Host, r.Port)
		}

		message := r.Detail
		if r.Error != "" {
			message = r.Error
		}

		if _, err := fmt.Fprintf(w, "%-7s %-14s %-24s %6dms  %s\n",
			cliLabel(r.Outcome), r.Type, target, r.LatencyMs, message); err != nil {
			return err
		}
	}

	return nil
}

func cliLabel(outcome string) string {
	switch outcome {
	case checker.OutcomeOK:
		return "OK"
	case checker.OutcomeIndeterminate:
		return "UNKNOWN"
	default:
		return "FAIL"
	}
}

// cliExitCode は最も重い結果を終了コードに変換する。
// 確定的な失敗 > 判定不能 > 成功 の順で優先する。
func cliExitCode(results []checker.CheckResult) int {
	code := exitOK
	for _, r := range results {
		switch r.Outcome {
		case checker.OutcomeFailed:
			return exitFailed
		case checker.OutcomeIndeterminate:
			code = exitIndeterminate
		}
	}

	return code
}
