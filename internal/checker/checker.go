package checker

import (
	"context"
	"time"
)

// CheckResult holds the result of a single connection check.
type CheckResult struct {
	Success   bool      `json:"success"`
	Type      string    `json:"type"`
	Host      string    `json:"host,omitempty"`
	Port      int       `json:"port,omitempty"`
	LatencyMs int64     `json:"latency_ms"`
	Detail    string    `json:"detail,omitempty"`
	Error     string    `json:"error,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
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

// Timeout returns the effective timeout duration for a request.
func (r CheckRequest) Timeout() time.Duration {
	if r.TimeoutSec <= 0 {
		return 5 * time.Second
	}
	return time.Duration(r.TimeoutSec) * time.Second
}

// Run executes a check and records timing.
func Run(ctx context.Context, req CheckRequest, fn func(ctx context.Context) (string, error)) CheckResult {
	start := time.Now()
	detail, err := fn(ctx)
	latency := time.Since(start).Milliseconds()

	result := CheckResult{
		Type:      req.Type,
		Host:      req.Host,
		Port:      req.Port,
		LatencyMs: latency,
		CheckedAt: time.Now(),
	}
	if err != nil {
		result.Success = false
		result.Error = err.Error()
	} else {
		result.Success = true
		result.Detail = detail
	}
	return result
}
