package updater

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSemVerCompareAndIsNewer(t *testing.T) {
	cases := []struct {
		cand  string
		cur   string
		newer bool
	}{
		{"v0.2.0", "0.1.0", true},
		{"0.2.0", "v0.1.0", true},
		{"v0.1.0", "0.1.0", false},
		{"v0.1.0", "0.2.0", false},
		{"0.2.0-beta.2", "0.2.0-beta.1", true},
		{"0.2.0-beta.10", "0.2.0-beta.2", true},
		{"0.2.0", "0.2.0-beta.10", true},
		{"0.2.0-beta.1", "0.2.0", false},
		{"1.0", "0.9.9", true},
		{"invalid", "0.1.0", false},
	}
	for _, tc := range cases {
		if got := IsNewer(tc.cand, tc.cur); got != tc.newer {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", tc.cand, tc.cur, got, tc.newer)
		}
	}
	if FormatVersion("0.2.0") != "v0.2.0" || FormatVersion("V0.2.0") != "v0.2.0" {
		t.Fatalf("unexpected FormatVersion output: %q / %q", FormatVersion("0.2.0"), FormatVersion("V0.2.0"))
	}
}

func TestParseSHA256SumsAndFormatBytes(t *testing.T) {
	txt := "# comment\n" +
		"80DD825FC2A15DE39EEFB8741EF056E6452AC20BECDACC9935F66C1EF70C1B51  SnipGo-0.2.0-arm64.app.zip\n" +
		"28dc43f533f40fd1879578b200c672dcfcc4cae4a3e4a46ed4ff7f2fd81ff2ab *./dist/SnipGo-0.2.0-universal.app.zip\n" +
		"badline\n"
	m := ParseSHA256Sums(txt)
	if len(m) != 2 {
		t.Fatalf("expected 2 entries, got %d (%v)", len(m), m)
	}
	if m["SnipGo-0.2.0-arm64.app.zip"] != "80dd825fc2a15de39eefb8741ef056e6452ac20becdacc9935f66c1ef70c1b51" {
		t.Fatalf("unexpected hash for arm64 zip: %q", m["SnipGo-0.2.0-arm64.app.zip"])
	}
	if m["SnipGo-0.2.0-universal.app.zip"] != "28dc43f533f40fd1879578b200c672dcfcc4cae4a3e4a46ed4ff7f2fd81ff2ab" {
		t.Fatalf("unexpected hash for universal zip: %q", m["SnipGo-0.2.0-universal.app.zip"])
	}
	if FormatBytes(512) != "512 B" || FormatBytes(2048) != "2.0 KB" || FormatBytes(5*1024*1024) != "5.0 MB" {
		t.Fatalf("FormatBytes mismatch: %q %q %q", FormatBytes(512), FormatBytes(2048), FormatBytes(5*1024*1024))
	}
}

func TestDetectAppBundleAndWritableCheck(t *testing.T) {
	if _, err := DetectAppBundle("/usr/local/bin/snipgo"); err == nil {
		t.Fatal("expected error for non-.app binary")
	}

	tmp := t.TempDir()
	appDir := filepath.Join(tmp, "SnipGo.app")
	exePath := filepath.Join(appDir, "Contents", "MacOS", "SnipGo")
	if err := os.MkdirAll(filepath.Dir(exePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exePath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := DetectAppBundle(exePath)
	if err != nil {
		t.Fatalf("DetectAppBundle failed: %v", err)
	}
	wantReal, _ := filepath.EvalSymlinks(appDir)
	if wantReal == "" {
		wantReal = appDir
	}
	if got != filepath.Clean(wantReal) {
		t.Fatalf("DetectAppBundle = %q, want %q", got, wantReal)
	}
	if err := CheckWritableBundle(appDir); err != nil {
		t.Fatalf("CheckWritableBundle valid app failed: %v", err)
	}
	if err := CheckWritableBundle(filepath.Join(tmp, "PlainDir")); err == nil {
		t.Fatal("expected error for non-.app suffix")
	}
	if err := CheckWritableBundle("/private/var/folders/x/AppTranslocation/abc/d/SnipGo.app"); err == nil {
		t.Fatal("expected error for AppTranslocation path")
	}
	if err := CheckWritableBundle("/Volumes/SnipGo/SnipGo.app"); err == nil {
		t.Fatal("expected error for /Volumes mount")
	}
}

func makeMockAppZip(t *testing.T, bundleID, version string) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	files := map[string]struct {
		body string
		mode os.FileMode
	}{
		"SnipGo.app/Contents/Info.plist": {
			body: fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>%s</string>
<key>CFBundleShortVersionString</key><string>%s</string>
</dict></plist>`, bundleID, version),
			mode: 0o644,
		},
		"SnipGo.app/Contents/MacOS/SnipGo": {
			body: "#!/bin/sh\necho " + version + "\n",
			mode: 0o755,
		},
	}

	for name, f := range files {
		hdr := &zip.FileHeader{Name: name, Method: zip.Deflate}
		hdr.SetMode(f.mode)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatalf("CreateHeader %s: %v", name, err)
		}
		if _, err := w.Write([]byte(f.body)); err != nil {
			t.Fatalf("Write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:])
}

func TestCheckWithLatestAndPrereleaseAndSumsFallback(t *testing.T) {
	zipBytes, zipHash := makeMockAppZip(t, DefaultBundleID, "0.2.0")
	_ = zipBytes

	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/Lumiaqian/snipgo/releases/latest":
			_ = json.NewEncoder(w).Encode(ghRelease{
				TagName:    "v0.2.0",
				Name:       "SnipGo 0.2.0",
				Body:       "### 新特性\n- 自动更新",
				Prerelease: false,
				HTMLURL:    srvURL + "/rel/v0.2.0",
				Assets: []ghAsset{
					{
						Name:               "SnipGo-0.2.0-arm64.app.zip",
						BrowserDownloadURL: srvURL + "/dl/SnipGo-0.2.0-arm64.app.zip",
						Size:               int64(len(zipBytes)),
						State:              "uploaded",
						// 故意省略 Digest，测试 SHA256SUMS.txt 回退路径
					},
					{
						Name:               "SnipGo-0.2.0-universal.app.zip",
						BrowserDownloadURL: srvURL + "/dl/SnipGo-0.2.0-universal.app.zip",
						Size:               int64(len(zipBytes)),
						Digest:             "sha256:" + zipHash,
						State:              "uploaded",
					},
					{
						Name:               "SHA256SUMS.txt",
						BrowserDownloadURL: srvURL + "/dl/SHA256SUMS.txt",
						State:              "uploaded",
					},
				},
			})
		case "/dl/SHA256SUMS.txt":
			_, _ = fmt.Fprintf(w, "%s  SnipGo-0.2.0-arm64.app.zip\n", zipHash)
		case "/repos/Lumiaqian/snipgo/releases":
			_ = json.NewEncoder(w).Encode([]ghRelease{
				{
					TagName:    "v0.3.0-beta.2",
					Name:       "SnipGo 0.3.0 Beta 2",
					Body:       "Beta notes",
					Prerelease: true,
					HTMLURL:    srvURL + "/rel/v0.3.0-beta.2",
					Assets: []ghAsset{
						{
							Name:               "SnipGo-0.3.0-beta.2-universal.app.zip",
							BrowserDownloadURL: srvURL + "/dl/beta.zip",
							Size:               1234,
							Digest:             "sha256:" + zipHash,
							State:              "uploaded",
						},
					},
				},
				{TagName: "v0.2.0", Prerelease: false},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	srvURL = srv.URL

	c := NewClient()
	c.APIBase = srv.URL
	c.Arch = "arm64"

	// 1. 0.1.0 -> 发现 v0.2.0 且通过 SHA256SUMS.txt 解析出哈希
	rel, err := c.Check(context.Background(), "0.1.0", false)
	if err != nil {
		t.Fatalf("Check stable: %v", err)
	}
	if rel == nil || rel.Version != "0.2.0" || rel.AssetName != "SnipGo-0.2.0-arm64.app.zip" || rel.AssetSHA256 != zipHash {
		t.Fatalf("unexpected release: %+v", rel)
	}

	// 2. amd64 无专属包时自动回退 universal 包，并直接采用 GitHub digest 字段
	c.Arch = "amd64"
	relAmd, err := c.Check(context.Background(), "0.1.0", false)
	if err != nil {
		t.Fatalf("Check amd64 fallback: %v", err)
	}
	if relAmd == nil || relAmd.AssetName != "SnipGo-0.2.0-universal.app.zip" || relAmd.AssetSHA256 != zipHash {
		t.Fatalf("unexpected amd64 fallback release: %+v", relAmd)
	}

	// 3. 当前已是 0.2.0 且关闭测试通道 -> nil
	upToDate, err := c.Check(context.Background(), "0.2.0", false)
	if err != nil || upToDate != nil {
		t.Fatalf("expected up-to-date nil, got rel=%+v err=%v", upToDate, err)
	}

	// 4. 开启测试通道 -> 命中 v0.3.0-beta.2
	beta, err := c.Check(context.Background(), "0.2.0", true)
	if err != nil {
		t.Fatalf("Check prereleases: %v", err)
	}
	if beta == nil || beta.Version != "0.3.0-beta.2" || !beta.Prerelease {
		t.Fatalf("expected beta v0.3.0-beta.2, got %+v", beta)
	}
}

func TestCheckServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer srv.Close()

	c := NewClient()
	c.APIBase = srv.URL
	c.HTTPClient = srv.Client()
	if _, err := c.Check(context.Background(), "0.1.0", false); err == nil {
		t.Fatal("expected HTTP 502 error")
	}
}

func TestDownloadAndStageVerifyAndAtomicSwap(t *testing.T) {
	goodZip, goodHash := makeMockAppZip(t, DefaultBundleID, "0.2.0")
	wrongIDZip, wrongIDHash := makeMockAppZip(t, "com.evil.other", "0.2.0")

	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/good.zip":
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(goodZip)))
			_, _ = w.Write(goodZip)
		case "/wrong-id.zip":
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(wrongIDZip)))
			_, _ = w.Write(wrongIDZip)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	srvURL = srv.URL

	// 1. 哈希不匹配必须拒绝并清理临时目录
	tmp := t.TempDir()
	targetApp := filepath.Join(tmp, "SnipGo.app")
	createMockTargetApp(t, targetApp, "0.1.0")

	c := NewClient()
	c.AppPID = 999999 // 仅测试拒绝分支，不实际触发替换
	badRel := &Release{
		Version:     "0.2.0",
		AssetName:   "SnipGo-0.2.0-arm64.app.zip",
		AssetURL:    srvURL + "/good.zip",
		AssetSize:   int64(len(goodZip)),
		AssetSHA256: strings.Repeat("0", 64),
	}
	if _, err := c.DownloadAndStage(context.Background(), badRel, targetApp, nil); err == nil {
		t.Fatal("expected hash mismatch error")
	}
	assertNoLeftoverWorkDirs(t, tmp)

	// 2. Bundle ID 不一致必须拒绝并清理临时目录
	wrongRel := &Release{
		Version:     "0.2.0",
		AssetName:   "SnipGo-0.2.0-arm64.app.zip",
		AssetURL:    srvURL + "/wrong-id.zip",
		AssetSize:   int64(len(wrongIDZip)),
		AssetSHA256: wrongIDHash,
	}
	if _, err := c.DownloadAndStage(context.Background(), wrongRel, targetApp, nil); err == nil {
		t.Fatal("expected bundle ID mismatch error")
	}
	assertNoLeftoverWorkDirs(t, tmp)

	// 3. 启动真实短生命周期子进程充当旧进程，验证：
	//    进度回调 -> SHA-256 通过 -> 进程退出后原子替换 .app -> 临时目录彻底清理
	sleeper := exec.Command("/bin/sh", "-c", "sleep 0.25")
	if err := sleeper.Start(); err != nil {
		t.Fatalf("start sleeper: %v", err)
	}
	defer func() {
		if sleeper.Process != nil {
			_ = sleeper.Process.Kill()
			_ = sleeper.Wait()
		}
	}()

	c.AppPID = sleeper.Process.Pid
	var sawDownloading, sawVerifying, sawExtracting, sawStaging bool
	goodRel := &Release{
		Version:     "0.2.0",
		AssetName:   "SnipGo-0.2.0-arm64.app.zip",
		AssetURL:    srvURL + "/good.zip",
		AssetSize:   int64(len(goodZip)),
		AssetSHA256: goodHash,
	}
	staged, err := c.DownloadAndStage(context.Background(), goodRel, targetApp, func(phase StagePhase, dl, total int64) {
		switch phase {
		case PhaseDownloading:
			sawDownloading = true
		case PhaseVerifying:
			sawVerifying = true
		case PhaseExtracting:
			sawExtracting = true
		case PhaseStaging:
			sawStaging = true
		}
	})
	if err != nil {
		t.Fatalf("DownloadAndStage failed: %v", err)
	}
	if !sawDownloading || !sawVerifying || !sawExtracting || !sawStaging {
		t.Fatalf("missing progress phases: dl=%v ver=%v ext=%v stg=%v", sawDownloading, sawVerifying, sawExtracting, sawStaging)
	}
	if err := sleeper.Wait(); err != nil {
		t.Fatalf("sleeper wait: %v", err)
	}

	// 等待后台脚本完成 mv 替换与 workDir 清理（不写入 relaunch 标记以免触发 /usr/bin/open）
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		plist, err := os.ReadFile(filepath.Join(targetApp, "Contents", "Info.plist"))
		_, workErr := os.Stat(staged.WorkDir)
		if err == nil && strings.Contains(string(plist), "<string>0.2.0</string>") && os.IsNotExist(workErr) {
			return
		}
		time.Sleep(40 * time.Millisecond)
	}
	plist, _ := os.ReadFile(filepath.Join(targetApp, "Contents", "Info.plist"))
	t.Fatalf("atomic swap did not complete in time; target Info.plist:\n%s", string(plist))
}

func createMockTargetApp(t *testing.T, appDir, version string) {
	t.Helper()
	macOS := filepath.Join(appDir, "Contents", "MacOS")
	if err := os.MkdirAll(macOS, 0o755); err != nil {
		t.Fatal(err)
	}
	plist := fmt.Sprintf(`<plist><dict><key>CFBundleIdentifier</key><string>%s</string><key>CFBundleShortVersionString</key><string>%s</string></dict></plist>`, DefaultBundleID, version)
	if err := os.WriteFile(filepath.Join(appDir, "Contents", "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(macOS, "SnipGo"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func assertNoLeftoverWorkDirs(t *testing.T, parentDir string) {
	t.Helper()
	entries, err := os.ReadDir(parentDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".snipgo-update-") {
			t.Fatalf("leftover temp dir found: %s", e.Name())
		}
	}
}
