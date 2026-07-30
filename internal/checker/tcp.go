package checker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"time"
)

type TCPChecker struct{}

func (c *TCPChecker) Check(ctx context.Context, req CheckRequest) CheckResult {
	return Run(ctx, req, func(ctx context.Context) (string, error) {
		addr := req.Addr(0)
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
		if err != nil {
			return "", err
		}
		conn.Close()
		return fmt.Sprintf("TCP connection to %s successful", addr), nil
	})
}

type UDPChecker struct{}

// Check は UDP ポートを三値で判定する。
//
// UDP にはハンドシェイクが無いため、DialContext が成功してもパケットが1つも
// 送られておらず到達性の証拠にはならない（名前解決が通ったことしか分からない）。
// そこで空のデータグラムを1つ送り、次の3つに分類する。
//
//	OutcomeOK            相手が応答を返した
//	OutcomeFailed        ICMP port unreachable を受けた（確定的に閉）
//	OutcomeIndeterminate 無応答（開いていて黙っているか、ドロップされたか判別不能）
//
// AWS の Security Group のように不許可トラフィックを reject せず drop する
// 経路では、閉じたポートも開いたポートも indeterminate になる点に注意。
func (c *UDPChecker) Check(ctx context.Context, req CheckRequest) CheckResult {
	return RunProbe(ctx, req, func(ctx context.Context) (Probe, error) {
		addr := req.Addr(0)

		conn, err := (&net.Dialer{}).DialContext(ctx, "udp", addr)
		if err != nil {
			// 名前解決やソケット作成の失敗は確定的な失敗。
			return Probe{}, err
		}
		defer conn.Close()

		// 内容を解釈されにくいよう1バイトのゼロを送る。ポートが閉じていれば
		// ICMP port unreachable が返り、後続の Read/Write が ECONNREFUSED になる。
		if _, err := conn.Write([]byte{0}); err != nil {
			if errors.Is(err, syscall.ECONNREFUSED) {
				return Probe{}, fmt.Errorf("UDP port %s is closed (ICMP port unreachable)", addr)
			}
			return Probe{}, err
		}

		// 読み取りデッドラインは必須。これが無いと無応答のポートで Read が
		// 永久にブロックする。ctx に期限が無い場合はリクエストの timeout を使う。
		deadline, ok := ctx.Deadline()
		if !ok {
			deadline = time.Now().Add(req.Timeout())
		}
		if err := conn.SetReadDeadline(deadline); err != nil {
			return Probe{}, err
		}

		buf := make([]byte, 512)
		n, err := conn.Read(buf)
		switch {
		case err == nil:
			return Probe{
				Outcome: OutcomeOK,
				Detail:  fmt.Sprintf("UDP %s replied with %d byte(s)", addr, n),
			}, nil

		case errors.Is(err, syscall.ECONNREFUSED):
			return Probe{}, fmt.Errorf("UDP port %s is closed (ICMP port unreachable)", addr)

		case isTimeout(err):
			return Probe{
				Outcome: OutcomeIndeterminate,
				Detail: fmt.Sprintf(
					"sent a datagram to %s but got no reply within %s. UDP is connectionless, "+
						"so this is not evidence of reachability: the port may be open but silent, "+
						"or the traffic may have been dropped. Firewalls that drop rather than "+
						"reject (including AWS security groups) look identical to an open port here.",
					addr, req.Timeout()),
			}, nil

		default:
			return Probe{}, err
		}
	})
}

// isTimeout は読み取りデッドライン超過かどうかを判定する。
func isTimeout(err error) bool {
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var netErr net.Error

	return errors.As(err, &netErr) && netErr.Timeout()
}
