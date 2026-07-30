package checker

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

// documentedTypes は README の "Supported type values" と一致していなければならない。
// README とレジストリの乖離を防ぐためのガード。
var documentedTypes = []string{
	"tcp", "udp",
	"mysql", "mariadb",
	"postgres", "postgresql",
	"mongodb", "redis",
	"elasticsearch", "rabbitmq",
	"smtp", "sqlserver", "mssql",
}

func TestRegistryCoversDocumentedTypes(t *testing.T) {
	for _, name := range documentedTypes {
		if _, ok := registry[name]; !ok {
			t.Errorf("type %q is documented in the README but not registered", name)
		}
	}

	if len(registry) != len(documentedTypes) {
		t.Errorf("registry has %d entries but %d are documented; update the README or documentedTypes",
			len(registry), len(documentedTypes))
	}
}

func TestRegistryAliases(t *testing.T) {
	aliases := map[string]string{
		"mariadb":    "mysql",
		"postgresql": "postgres",
		"mssql":      "sqlserver",
	}

	for alias, canonical := range aliases {
		a, b := registry[alias], registry[canonical]
		if a == nil || b == nil {
			t.Fatalf("alias %q or canonical %q missing from registry", alias, canonical)
		}
		if reflect.TypeOf(a) != reflect.TypeOf(b) {
			t.Errorf("alias %q resolves to %T, want same type as %q (%T)", alias, a, canonical, b)
		}
	}
}

func TestDispatchUnknownType(t *testing.T) {
	_, err := Dispatch(context.Background(), CheckRequest{Type: "nosuchdb"})
	if err == nil {
		t.Fatal("Dispatch() error = nil, want an error for an unknown type")
	}
	if !strings.Contains(err.Error(), "nosuchdb") {
		t.Errorf("error %q should name the offending type", err.Error())
	}
}

// fakeChecker はチェッカーに渡された context を記録する。
type fakeChecker struct {
	gotDeadline bool
	remaining   time.Duration
}

func (f *fakeChecker) Check(ctx context.Context, req CheckRequest) CheckResult {
	deadline, ok := ctx.Deadline()
	f.gotDeadline = ok
	if ok {
		f.remaining = time.Until(deadline)
	}
	return CheckResult{Success: true, Type: req.Type}
}

// registerForTest はテスト中だけレジストリにチェッカーを差し込む。
func registerForTest(t *testing.T, name string, c Checker) {
	t.Helper()
	if _, exists := registry[name]; exists {
		t.Fatalf("refusing to shadow existing registry entry %q", name)
	}
	registry[name] = c
	t.Cleanup(func() { delete(registry, name) })
}

func TestDispatchAppliesTimeout(t *testing.T) {
	fake := &fakeChecker{}
	registerForTest(t, "faketype", fake)

	_, err := Dispatch(context.Background(), CheckRequest{Type: "faketype", TimeoutSec: 3})
	if err != nil {
		t.Fatalf("Dispatch() error = %v, want nil", err)
	}

	if !fake.gotDeadline {
		t.Fatal("checker received a context without a deadline; Dispatch must apply the request timeout")
	}
	// 3秒指定なので、残りは 3 秒以下かつ 2 秒より大きいはず。
	if fake.remaining > 3*time.Second || fake.remaining < 2*time.Second {
		t.Errorf("deadline remaining = %v, want ~3s", fake.remaining)
	}
}

func TestDispatchAppliesDefaultTimeout(t *testing.T) {
	fake := &fakeChecker{}
	registerForTest(t, "faketype-default", fake)

	// TimeoutSec 未指定なら CheckRequest.Timeout() のデフォルト5秒が使われる。
	if _, err := Dispatch(context.Background(), CheckRequest{Type: "faketype-default"}); err != nil {
		t.Fatalf("Dispatch() error = %v, want nil", err)
	}

	if fake.remaining > 5*time.Second || fake.remaining < 4*time.Second {
		t.Errorf("deadline remaining = %v, want ~5s", fake.remaining)
	}
}
