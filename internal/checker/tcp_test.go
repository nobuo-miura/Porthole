package checker

import (
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
)

// listenLocal は待ち受け中のローカルポートを返す。
func listenLocal(t *testing.T) (host string, port int) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	return splitHostPort(t, ln.Addr().String())
}

// closedLocalPort は誰も待ち受けていないローカルポートを返す。
// 一度 listen して番号を得た直後に閉じることで、確実に接続拒否される番号を得る。
func closedLocalPort(t *testing.T) (host string, port int) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	host, port = splitHostPort(t, ln.Addr().String())
	if err := ln.Close(); err != nil {
		t.Fatalf("failed to close listener: %v", err)
	}

	return host, port
}

func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()

	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("failed to split %q: %v", addr, err)
	}
	port, err := strconv.Atoi(p)
	if err != nil {
		t.Fatalf("failed to parse port %q: %v", p, err)
	}

	return h, port
}

func TestTCPCheckerSuccess(t *testing.T) {
	host, port := listenLocal(t)

	got := (&TCPChecker{}).Check(context.Background(), CheckRequest{Type: "tcp", Host: host, Port: port})

	if !got.Success {
		t.Fatalf("Success = false, want true (error: %s)", got.Error)
	}
	if got.Detail == "" {
		t.Error("Detail is empty, want a description of the connection")
	}
	if got.Host != host || got.Port != port {
		t.Errorf("got host=%s port=%d, want host=%s port=%d", got.Host, got.Port, host, port)
	}
}

func TestTCPCheckerRefused(t *testing.T) {
	host, port := closedLocalPort(t)

	got := (&TCPChecker{}).Check(context.Background(), CheckRequest{Type: "tcp", Host: host, Port: port})

	if got.Success {
		t.Fatal("Success = true, want false for a closed port")
	}
	if got.Error == "" {
		t.Error("Error is empty, want the dial failure reason")
	}
	if got.Detail != "" {
		t.Errorf("Detail = %q, want empty on failure", got.Detail)
	}
}

func TestTCPCheckerRespectsCancelledContext(t *testing.T) {
	host, port := listenLocal(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got := (&TCPChecker{}).Check(ctx, CheckRequest{Type: "tcp", Host: host, Port: port})

	if got.Success {
		t.Error("Success = true, want false for a cancelled context")
	}
}

// listenUDP は UDP ソケットを bind し、reply が true なら受信内容に応答する。
// reply が false のソケットは「開いているが黙っている」ポートを再現する。
func listenUDP(t *testing.T, reply bool) (host string, port int) {
	t.Helper()

	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("failed to listen on udp: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	go func() {
		buf := make([]byte, 512)
		for {
			n, peer, err := conn.ReadFromUDP(buf)
			if err != nil {
				return // Cleanup で閉じられた
			}
			if reply {
				_, _ = conn.WriteToUDP(buf[:n], peer)
			}
		}
	}()

	addr := conn.LocalAddr().(*net.UDPAddr)

	return addr.IP.String(), addr.Port
}

func TestUDPCheckerRepliedIsOK(t *testing.T) {
	host, port := listenUDP(t, true)

	got := (&UDPChecker{}).Check(context.Background(),
		CheckRequest{Type: "udp", Host: host, Port: port, TimeoutSec: 2})

	if got.Outcome != OutcomeOK {
		t.Fatalf("Outcome = %q, want %q (error: %s, detail: %s)", got.Outcome, OutcomeOK, got.Error, got.Detail)
	}
	if !got.Success {
		t.Error("Success = false, want true when the peer replied")
	}
}

// 閉じたポートは ICMP port unreachable により確定的な失敗になる。
func TestUDPCheckerClosedPortIsFailed(t *testing.T) {
	host, port := closedLocalPort(t)

	got := (&UDPChecker{}).Check(context.Background(),
		CheckRequest{Type: "udp", Host: host, Port: port, TimeoutSec: 2})

	if got.Outcome != OutcomeFailed {
		t.Fatalf("Outcome = %q, want %q (detail: %s)", got.Outcome, OutcomeFailed, got.Detail)
	}
	if got.Success {
		t.Error("Success = true, want false for a closed UDP port")
	}
	if !strings.Contains(got.Error, "closed") {
		t.Errorf("Error = %q, should say the port is closed", got.Error)
	}
}

// 開いていて黙っているポートは「判定不能」であり、成功でも失敗でもない。
// 以前は DialContext の成功だけで OK を返していた箇所。
func TestUDPCheckerSilentPortIsIndeterminate(t *testing.T) {
	host, port := listenUDP(t, false)

	got := (&UDPChecker{}).Check(context.Background(),
		CheckRequest{Type: "udp", Host: host, Port: port, TimeoutSec: 1})

	if got.Outcome != OutcomeIndeterminate {
		t.Fatalf("Outcome = %q, want %q (error: %s, detail: %s)",
			got.Outcome, OutcomeIndeterminate, got.Error, got.Detail)
	}
	if got.Success {
		t.Error("Success = true, want false: a silent UDP port is not evidence of reachability")
	}
	if got.Error != "" {
		t.Errorf("Error = %q, want empty: indeterminate is not a failure", got.Error)
	}
	if got.Detail == "" {
		t.Error("Detail is empty, want an explanation of why the result is inconclusive")
	}
}

func TestUDPCheckerUnresolvableHostIsFailed(t *testing.T) {
	got := (&UDPChecker{}).Check(context.Background(),
		CheckRequest{Type: "udp", Host: "no-such-host.invalid", Port: 9999, TimeoutSec: 2})

	if got.Outcome != OutcomeFailed {
		t.Errorf("Outcome = %q, want %q for an unresolvable host", got.Outcome, OutcomeFailed)
	}
}
