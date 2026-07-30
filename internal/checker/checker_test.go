package checker

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCheckRequestTimeout(t *testing.T) {
	tests := []struct {
		name       string
		timeoutSec int
		want       time.Duration
	}{
		{"未指定はデフォルト5秒", 0, 5 * time.Second},
		{"負値はデフォルト5秒", -3, 5 * time.Second},
		{"1秒", 1, time.Second},
		{"30秒", 30, 30 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CheckRequest{TimeoutSec: tt.timeoutSec}.Timeout()
			if got != tt.want {
				t.Errorf("Timeout() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunSuccess(t *testing.T) {
	req := CheckRequest{Type: "tcp", Host: "example.test", Port: 1234}

	got := Run(context.Background(), req, func(context.Context) (string, error) {
		return "all good", nil
	})

	if !got.Success {
		t.Errorf("Success = false, want true")
	}
	if got.Detail != "all good" {
		t.Errorf("Detail = %q, want %q", got.Detail, "all good")
	}
	if got.Error != "" {
		t.Errorf("Error = %q, want empty", got.Error)
	}
	if got.Type != req.Type || got.Host != req.Host || got.Port != req.Port {
		t.Errorf("request fields not copied: got %+v, want type=%s host=%s port=%d",
			got, req.Type, req.Host, req.Port)
	}
	if got.CheckedAt.IsZero() {
		t.Error("CheckedAt is zero, want a timestamp")
	}
}

func TestRunFailure(t *testing.T) {
	wantErr := errors.New("connection refused")

	got := Run(context.Background(), CheckRequest{Type: "tcp"}, func(context.Context) (string, error) {
		// 失敗時に detail を返しても採用されないことを確認する。
		return "ignored detail", wantErr
	})

	if got.Success {
		t.Error("Success = true, want false")
	}
	if got.Error != wantErr.Error() {
		t.Errorf("Error = %q, want %q", got.Error, wantErr.Error())
	}
	if got.Detail != "" {
		t.Errorf("Detail = %q, want empty on failure", got.Detail)
	}
}

func TestRunRecordsLatency(t *testing.T) {
	const sleep = 25 * time.Millisecond

	got := Run(context.Background(), CheckRequest{Type: "tcp"}, func(context.Context) (string, error) {
		time.Sleep(sleep)
		return "slow but fine", nil
	})

	// 計測の丸めを考慮して下限だけ検証する。
	if got.LatencyMs < sleep.Milliseconds()-5 {
		t.Errorf("LatencyMs = %d, want >= %d", got.LatencyMs, sleep.Milliseconds()-5)
	}
}

func TestRunPassesContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got := Run(ctx, CheckRequest{Type: "tcp"}, func(ctx context.Context) (string, error) {
		return "", ctx.Err()
	})

	if got.Success {
		t.Error("Success = true, want false for a cancelled context")
	}
	if got.Error != context.Canceled.Error() {
		t.Errorf("Error = %q, want %q", got.Error, context.Canceled.Error())
	}
}
