package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo"
)

type mockUpdateBackend struct {
	mu          sync.Mutex
	enabled     bool
	prereleases bool
	checkCand   *updateCandidate
	checkErr    error
	installErr  error
	installFn   func(progress func(dl, total int64))
	relaunched  int
}

func (m *mockUpdateBackend) Enabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enabled
}

func (m *mockUpdateBackend) SetPrereleases(on bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prereleases = on
}

func (m *mockUpdateBackend) Check(context.Context) (*updateCandidate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.checkCand, m.checkErr
}

func (m *mockUpdateBackend) Install(_ context.Context, _ *updateCandidate, progress func(downloaded, total int64)) error {
	m.mu.Lock()
	fn := m.installFn
	err := m.installErr
	m.mu.Unlock()
	if fn != nil {
		fn(progress)
	}
	return err
}

func (m *mockUpdateBackend) Relaunch() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.relaunched++
}

func drainMainTasks(t *testing.T, ch <-chan func(), count int) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for i := 0; i < count; i++ {
		select {
		case fn := <-ch:
			fn()
		case <-deadline:
			t.Fatalf("timed out waiting for main-thread callback #%d", i+1)
		}
	}
}

func TestUpdaterHelpersAndFriendlyErrors(t *testing.T) {
	if got := formatVersion(" 0.1.2 "); got != "v0.1.2" {
		t.Fatalf("formatVersion(0.1.2) = %q, want v0.1.2", got)
	}
	if got := formatVersion("V0.2.0-beta.1"); got != "v0.2.0-beta.1" {
		t.Fatalf("formatVersion(V0.2.0-beta.1) = %q", got)
	}
	if got := formatBytes(512); got != "512 B" {
		t.Fatalf("formatBytes(512) = %q", got)
	}
	if got := formatBytes(2048); got != "2.0 KB" {
		t.Fatalf("formatBytes(2048) = %q", got)
	}
	if got := formatBytes(3 * 1024 * 1024); got != "3.0 MB" {
		t.Fatalf("formatBytes(3MB) = %q", got)
	}

	if msg := friendlyUpdateError(mygo.ErrUpdatesDisabled); !strings.Contains(msg, "未启用应用内签名更新") {
		t.Fatalf("unexpected disabled message: %q", msg)
	}
	if msg := friendlyUpdateError(errors.New("GET .../update-darwin-arm64.json: 404 Not Found")); !strings.Contains(msg, "update-*.json") {
		t.Fatalf("unexpected 404 message: %q", msg)
	}
	if msg := friendlyUpdateError(errors.New("GET https://api.github.com/...: 403 rate limit")); !strings.Contains(msg, "403") || !strings.Contains(msg, "接收测试预览版") {
		t.Fatalf("unexpected 403 message: %q", msg)
	}
	if msg := friendlyUpdateError(errors.New("the update is not signed with the app's key")); !strings.Contains(msg, "Ed25519") {
		t.Fatalf("unexpected signature message: %q", msg)
	}
}

func TestUpdateManagerFlowAndSkippedVersion(t *testing.T) {
	mainCh := make(chan func(), 16)
	opened := 0
	backend := &mockUpdateBackend{
		enabled: true,
		checkCand: &updateCandidate{
			Version: "0.2.0",
			Notes:   "### 更新内容\n- 免 403 签名更新",
			Size:    2048,
		},
		installFn: func(progress func(dl, total int64)) {
			progress(1024, 2048)
			progress(2048, 2048)
		},
	}

	a := &app{}
	a.cfg.IncludePrereleases = true
	a.upd = &updateManager{
		backend: backend,
		onMain:  func(fn func()) { mainCh <- fn },
		onOpen:  func() { opened++ },
	}

	// 1. Check finds update and notifies once.
	a.checkForUpdates(true)
	drainMainTasks(t, mainCh, 1)
	if a.upd.state != updAvailable || a.upd.release == nil || a.upd.release.Version != "0.2.0" {
		t.Fatalf("unexpected state after check: state=%v release=%+v", a.upd.state, a.upd.release)
	}
	if !backend.prereleases {
		t.Fatal("expected IncludePrereleases=true to propagate to updateBackend")
	}
	if opened != 2 {
		t.Fatalf("expected manual check open + update found open (2), got %d", opened)
	}

	// 2. Skipped version is suppressed in updIdle.
	a.cfg.SkippedVersion = "0.2.0"
	a.upd.state = updIdle
	if a.availableUpdate() != nil {
		t.Fatal("expected skipped version to be hidden while idle")
	}
	a.cfg.SkippedVersion = ""

	// 3. Download transitions through progress + verifying to ready, then Relaunch triggers backend.Relaunch().
	a.startUpdateDownload()
	drainMainTasks(t, mainCh, 3)
	if a.upd.state != updReady || a.upd.stagedVersion != "0.2.0" {
		t.Fatalf("expected updReady with stagedVersion 0.2.0, got state=%v ver=%q", a.upd.state, a.upd.stagedVersion)
	}
	a.relaunchWithUpdate()
	if backend.relaunched != 1 {
		t.Fatalf("expected Relaunch count 1, got %d", backend.relaunched)
	}

	// 4. Already up-to-date case.
	a.upd.state = updIdle
	a.upd.stagedVersion = ""
	backend.checkCand = nil
	a.checkForUpdates(false)
	drainMainTasks(t, mainCh, 1)
	if a.upd.state != updUpToDate {
		t.Fatalf("expected updUpToDate, got %v", a.upd.state)
	}

	// 5. Friendly error mapped when manual check fails.
	backend.checkErr = mygo.ErrUpdatesDisabled
	a.checkForUpdates(true)
	drainMainTasks(t, mainCh, 1)
	if a.upd.state != updError || !strings.Contains(a.upd.errMsg, "未启用应用内签名更新") {
		t.Fatalf("expected updError with friendly disabled hint, got state=%v err=%q", a.upd.state, a.upd.errMsg)
	}
}
