package updater

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/netproxy"
)

const (
	// DefaultRepo 是 SnipGo 官方 GitHub 发布仓库。
	DefaultRepo = "Lumiaqian/snipgo"
	// DefaultAPIBase 是 GitHub REST API 根地址。
	DefaultAPIBase = "https://api.github.com"
	// DefaultBundleID 是 SnipGo.app 的 CFBundleIdentifier，用于校验更新包身份。
	DefaultBundleID = "app.snipgo"

	maxJSONSize  = 2 * 1024 * 1024   // 2 MiB
	maxSumsSize  = 256 * 1024        // 256 KiB
	maxAssetSize = 256 * 1024 * 1024 // 256 MiB
	stallTimeout = 60 * time.Second
)

// Release 描述从 GitHub Releases 解析得到的可用新版本元数据。
type Release struct {
	Version     string
	TagName     string
	Name        string
	Notes       string
	PublishedAt time.Time
	Prerelease  bool
	HTMLURL     string
	AssetName   string
	AssetURL    string
	AssetSize   int64
	AssetSHA256 string
	DMGURL      string
}

// StagePhase 标识下载并暂存新版本应用包所处的阶段。
type StagePhase int

const (
	PhaseDownloading StagePhase = iota
	PhaseVerifying
	PhaseExtracting
	PhaseStaging
)

// ProgressFunc 接收下载与校验进度回调。
type ProgressFunc func(phase StagePhase, downloaded, total int64)

// Client 负责检查更新、校验签名哈希及在 macOS 下暂存原子替换。
type Client struct {
	Repo            string
	APIBase         string
	HTTPClient      *http.Client
	Arch            string
	OS              string
	BundleID        string
	AppPID          int
	AllowUnverified bool
}

// NewClient 创建默认配置的 GitHub Releases 更新客户端（自动跟随 macOS 系统代理）。
func NewClient() *Client {
	tr := netproxy.Transport().Clone()
	tr.ResponseHeaderTimeout = 20 * time.Second
	return &Client{
		Repo:       DefaultRepo,
		APIBase:    DefaultAPIBase,
		HTTPClient: &http.Client{Transport: tr},
		Arch:       runtime.GOARCH,
		OS:         runtime.GOOS,
		BundleID:   DefaultBundleID,
		AppPID:     os.Getpid(),
	}
}

// StagedUpdate 表示已经通过 SHA-256 校验与身份核对并就绪等待应用退出后替换的新版本。
type StagedUpdate struct {
	Version      string
	TargetBundle string
	WorkDir      string
	NewBundle    string

	helper         *exec.Cmd
	relaunchMarked atomic.Bool
	cancelOnce     sync.Once
}

// MarkRelaunch 通知后台替换脚本在完成 .app 替换后自动重新打开应用。
func (s *StagedUpdate) MarkRelaunch() error {
	if s == nil {
		return errors.New("staged update is nil")
	}
	if err := os.WriteFile(filepath.Join(s.WorkDir, "relaunch"), []byte("1\n"), 0o644); err != nil {
		return err
	}
	s.relaunchMarked.Store(true)
	return nil
}

// Cancel 终止尚未执行的替换守护进程并清理暂存目录。若已标记重启且即将退出则不取消。
func (s *StagedUpdate) Cancel() {
	if s == nil || s.relaunchMarked.Load() {
		return
	}
	s.cancelOnce.Do(func() {
		if s.helper != nil && s.helper.Process != nil {
			_ = s.helper.Process.Kill()
			_ = s.helper.Wait()
		}
		if s.WorkDir != "" {
			_ = os.RemoveAll(s.WorkDir)
		}
	})
}

type ghRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
	Assets      []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
	Digest             string `json:"digest"`
	State              string `json:"state"`
}

func (c *Client) effectiveRepo() string {
	if strings.TrimSpace(c.Repo) != "" {
		return strings.TrimSpace(c.Repo)
	}
	return DefaultRepo
}

func (c *Client) effectiveAPIBase() string {
	b := strings.TrimRight(strings.TrimSpace(c.APIBase), "/")
	if b != "" {
		return b
	}
	return DefaultAPIBase
}

func (c *Client) effectiveArch() string {
	if strings.TrimSpace(c.Arch) != "" {
		return strings.TrimSpace(c.Arch)
	}
	return runtime.GOARCH
}

func (c *Client) effectiveOS() string {
	if strings.TrimSpace(c.OS) != "" {
		return strings.TrimSpace(c.OS)
	}
	return runtime.GOOS
}

func (c *Client) effectiveBundleID() string {
	if strings.TrimSpace(c.BundleID) != "" {
		return strings.TrimSpace(c.BundleID)
	}
	return DefaultBundleID
}

func (c *Client) effectivePID() int {
	if c.AppPID > 0 {
		return c.AppPID
	}
	return os.Getpid()
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	tr := netproxy.Transport().Clone()
	tr.ResponseHeaderTimeout = 20 * time.Second
	return &http.Client{Transport: tr}
}

// Check 向 GitHub Releases 查询是否有高于 currentVersion 的新版本；若已是最新则返回 nil, nil。
func (c *Client) Check(ctx context.Context, currentVersion string, includePrereleases bool) (*Release, error) {
	if _, err := ParseVersion(currentVersion); err != nil {
		return nil, fmt.Errorf("无效的当前版本号 %q: %w", currentVersion, err)
	}
	base := c.effectiveAPIBase()
	repo := c.effectiveRepo()

	var picked *ghRelease
	if !includePrereleases {
		u := fmt.Sprintf("%s/repos/%s/releases/latest", base, repo)
		data, err := c.fetchBytes(ctx, u, "application/vnd.github+json", maxJSONSize)
		if err != nil {
			return nil, err
		}
		var r ghRelease
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("解析 GitHub Release 响应失败: %w", err)
		}
		if !r.Draft && !r.Prerelease {
			picked = &r
		}
	} else {
		u := fmt.Sprintf("%s/repos/%s/releases?per_page=15", base, repo)
		data, err := c.fetchBytes(ctx, u, "application/vnd.github+json", maxJSONSize)
		if err != nil {
			return nil, err
		}
		var list []ghRelease
		if err := json.Unmarshal(data, &list); err != nil {
			return nil, fmt.Errorf("解析 GitHub Releases 列表失败: %w", err)
		}
		var bestVer string
		for i := range list {
			item := &list[i]
			if item.Draft {
				continue
			}
			if _, err := ParseVersion(item.TagName); err != nil {
				continue
			}
			if picked == nil || CompareVersion(item.TagName, bestVer) > 0 {
				picked = item
				bestVer = item.TagName
			}
		}
	}

	if picked == nil {
		return nil, nil
	}
	parsed, err := ParseVersion(picked.TagName)
	if err != nil || !IsNewer(picked.TagName, currentVersion) {
		return nil, nil
	}

	rel := c.buildRelease(picked, parsed)
	if rel.AssetURL != "" && rel.AssetSHA256 == "" {
		if sumsAsset := findAssetCI(picked.Assets, "SHA256SUMS.txt"); sumsAsset != nil {
			if rawSums, err := c.fetchBytes(ctx, sumsAsset.BrowserDownloadURL, "text/plain", maxSumsSize); err == nil {
				sums := ParseSHA256Sums(string(rawSums))
				if h := sums[rel.AssetName]; isHex64(h) {
					rel.AssetSHA256 = h
				}
			}
		}
	}
	return rel, nil
}

func (c *Client) buildRelease(r *ghRelease, v Version) *Release {
	verStr := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if len(v.Prerelease) > 0 {
		verStr += "-" + strings.Join(v.Prerelease, ".")
	}
	title := strings.TrimSpace(r.Name)
	if title == "" {
		title = FormatVersion(verStr)
	}
	arch := c.effectiveArch()
	zipAsset, dmgAsset := pickPlatformAssets(r.Assets, arch)

	dmgURL := r.HTMLURL
	if dmgAsset != nil && dmgAsset.BrowserDownloadURL != "" {
		dmgURL = dmgAsset.BrowserDownloadURL
	}

	rel := &Release{
		Version:     verStr,
		TagName:     r.TagName,
		Name:        title,
		Notes:       strings.TrimSpace(r.Body),
		PublishedAt: r.PublishedAt,
		Prerelease:  r.Prerelease,
		HTMLURL:     r.HTMLURL,
		DMGURL:      dmgURL,
	}
	if zipAsset != nil {
		rel.AssetName = zipAsset.Name
		rel.AssetURL = zipAsset.BrowserDownloadURL
		rel.AssetSize = zipAsset.Size
		rel.AssetSHA256 = parseGitHubDigest(zipAsset.Digest)
	}
	return rel
}

func pickPlatformAssets(assets []ghAsset, arch string) (zipAsset, dmgAsset *ghAsset) {
	var archZipSuffix, univZipSuffix string
	var archDMGSuffix, univDMGSuffix string
	switch arch {
	case "amd64":
		archZipSuffix = "-amd64.app.zip"
		univZipSuffix = "-universal.app.zip"
		archDMGSuffix = "-amd64.dmg"
		univDMGSuffix = "-universal.dmg"
	default:
		archZipSuffix = "-arm64.app.zip"
		univZipSuffix = "-universal.app.zip"
		archDMGSuffix = "-arm64.dmg"
		univDMGSuffix = "-universal.dmg"
	}

	var fallbackZip, fallbackDMG *ghAsset
	for i := range assets {
		a := &assets[i]
		if a.State != "" && a.State != "uploaded" {
			continue
		}
		low := strings.ToLower(a.Name)
		switch {
		case strings.HasSuffix(low, archZipSuffix) && zipAsset == nil:
			zipAsset = a
		case strings.HasSuffix(low, univZipSuffix) && fallbackZip == nil:
			fallbackZip = a
		case strings.HasSuffix(low, archDMGSuffix) && dmgAsset == nil:
			dmgAsset = a
		case strings.HasSuffix(low, univDMGSuffix) && fallbackDMG == nil:
			fallbackDMG = a
		}
	}
	if zipAsset == nil {
		zipAsset = fallbackZip
	}
	if dmgAsset == nil {
		dmgAsset = fallbackDMG
	}
	return zipAsset, dmgAsset
}

func findAssetCI(assets []ghAsset, targetName string) *ghAsset {
	for i := range assets {
		if strings.EqualFold(assets[i].Name, targetName) {
			return &assets[i]
		}
	}
	return nil
}

func parseGitHubDigest(d string) string {
	d = strings.TrimSpace(strings.ToLower(d))
	if !strings.HasPrefix(d, "sha256:") {
		return ""
	}
	hexPart := strings.TrimPrefix(d, "sha256:")
	if !isHex64(hexPart) {
		return ""
	}
	return hexPart
}

// DownloadAndStage 下载新版本安装包、执行 SHA-256 与 Bundle ID 校验、解压并通过后台替换守护就绪。
func (c *Client) DownloadAndStage(ctx context.Context, rel *Release, targetBundle string, onProgress ProgressFunc) (*StagedUpdate, error) {
	if rel == nil {
		return nil, errors.New("未提供目标版本信息")
	}
	if strings.TrimSpace(rel.AssetURL) == "" {
		return nil, errors.New("该版本未提供自动更新压缩包，请前往发布页手动下载 DMG 安装")
	}
	if !c.AllowUnverified && !isHex64(rel.AssetSHA256) {
		return nil, errors.New("未找到发布包的 SHA-256 校验摘要，为确保安全已中止自动更新")
	}
	if err := validateFetchURL(rel.AssetURL); err != nil {
		return nil, err
	}
	cleanTarget := filepath.Clean(strings.TrimSpace(targetBundle))
	if err := CheckWritableBundle(cleanTarget); err != nil {
		return nil, err
	}

	workDir, err := os.MkdirTemp(filepath.Dir(cleanTarget), ".snipgo-update-*")
	if err != nil {
		return nil, fmt.Errorf("创建临时更新目录失败: %w", err)
	}
	keepWork := false
	defer func() {
		if !keepWork {
			_ = os.RemoveAll(workDir)
		}
	}()

	zipPath := filepath.Join(workDir, "update.zip")
	if onProgress != nil {
		onProgress(PhaseDownloading, 0, rel.AssetSize)
	}
	gotHash, err := c.downloadAndHash(ctx, rel.AssetURL, zipPath, rel.AssetSize, onProgress)
	if err != nil {
		return nil, err
	}

	if onProgress != nil {
		onProgress(PhaseVerifying, rel.AssetSize, rel.AssetSize)
	}
	if !c.AllowUnverified {
		expectedHash := strings.ToLower(rel.AssetSHA256)
		if subtle.ConstantTimeCompare([]byte(gotHash), []byte(expectedHash)) != 1 {
			return nil, fmt.Errorf("安装包 SHA-256 校验失败（期望 %s，实际 %s）", expectedHash[:12], gotHash[:12])
		}
	}

	if onProgress != nil {
		onProgress(PhaseExtracting, rel.AssetSize, rel.AssetSize)
	}
	extractDir := filepath.Join(workDir, "extracted")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return nil, err
	}
	if err := c.extractArchive(ctx, zipPath, extractDir); err != nil {
		return nil, fmt.Errorf("解压更新包失败: %w", err)
	}

	newBundle, err := findAndVerifyAppBundle(extractDir, c.effectiveBundleID())
	if err != nil {
		return nil, err
	}

	if c.effectiveOS() == "darwin" {
		if _, err := os.Stat("/usr/bin/xattr"); err == nil {
			_ = exec.CommandContext(ctx, "/usr/bin/xattr", "-dr", "com.apple.quarantine", newBundle).Run()
		}
	}

	if onProgress != nil {
		onProgress(PhaseStaging, rel.AssetSize, rel.AssetSize)
	}
	cmd := exec.Command("/bin/sh", "-c", installAfterExitScript, "snipgo-updater",
		strconv.Itoa(c.effectivePID()), cleanTarget, newBundle, workDir)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动原子替换进程失败: %w", err)
	}
	keepWork = true
	return &StagedUpdate{
		Version:      rel.Version,
		TargetBundle: cleanTarget,
		WorkDir:      workDir,
		NewBundle:    newBundle,
		helper:       cmd,
	}, nil
}

const installAfterExitScript = `pid=$1 target=$2 new=$3 work=$4
trap '' HUP
while kill -0 "$pid" 2>/dev/null; do sleep 0.05; done
if mv "$target" "$work/old"; then
	mv "$new" "$target" || mv "$work/old" "$target"
fi
relaunch=
if [ -e "$work/relaunch" ]; then relaunch=1; fi
rm -rf "$work"
if [ -n "$relaunch" ]; then exec /usr/bin/open "$target"; fi
`

func (c *Client) fetchBytes(ctx context.Context, rawURL, accept string, maxBytes int64) ([]byte, error) {
	body, _, err := c.openGET(ctx, rawURL, accept)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("响应体超过大小上限 (%s)", FormatBytes(maxBytes))
	}
	return data, nil
}

func (c *Client) downloadAndHash(ctx context.Context, rawURL, dstPath string, expectedSize int64, onProgress ProgressFunc) (string, error) {
	body, contentLen, err := c.openGET(ctx, rawURL, "application/octet-stream")
	if err != nil {
		return "", err
	}
	defer body.Close()

	total := expectedSize
	if total <= 0 && contentLen > 0 {
		total = contentLen
	}

	out, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	defer out.Close()

	hasher := sha256.New()
	mw := io.MultiWriter(out, hasher)
	buf := make([]byte, 32*1024)
	var downloaded int64
	lastEmit := time.Now()
	for {
		n, readErr := body.Read(buf)
		if n > 0 {
			downloaded += int64(n)
			if downloaded > maxAssetSize {
				return "", fmt.Errorf("更新包大小超过上限 (%s)", FormatBytes(maxAssetSize))
			}
			if _, writeErr := mw.Write(buf[:n]); writeErr != nil {
				return "", writeErr
			}
			if onProgress != nil && time.Since(lastEmit) >= 60*time.Millisecond {
				onProgress(PhaseDownloading, downloaded, total)
				lastEmit = time.Now()
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return "", readErr
		}
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	if onProgress != nil {
		onProgress(PhaseDownloading, downloaded, max(total, downloaded))
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func (c *Client) openGET(ctx context.Context, rawURL, accept string) (io.ReadCloser, int64, error) {
	if err := validateFetchURL(rawURL); err != nil {
		return nil, 0, err
	}
	client := c.httpClient()
	retries := [2]time.Duration{400 * time.Millisecond, 1200 * time.Millisecond}

	for try := 0; ; try++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("User-Agent", "SnipGo-Updater")
		if accept != "" {
			req.Header.Set("Accept", accept)
		}

		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			return newStalledBody(resp.Body, stallTimeout), resp.ContentLength, nil
		}

		var transient bool
		var retErr error
		if err != nil {
			retErr = err
			transient = ctx.Err() == nil
		} else {
			retErr = fmt.Errorf("HTTP %s (%s)", resp.Status, rawURL)
			transient = resp.StatusCode == http.StatusTooManyRequests || (resp.StatusCode >= 500 && resp.StatusCode <= 599)
			_ = resp.Body.Close()
		}

		if !transient || try >= len(retries) || ctx.Err() != nil {
			return nil, 0, retErr
		}
		select {
		case <-ctx.Done():
			return nil, 0, ctx.Err()
		case <-time.After(retries[try]):
		}
	}
}

type stalledBody struct {
	rc      io.ReadCloser
	timeout time.Duration
	timer   *time.Timer
	timed   atomic.Bool
}

func newStalledBody(rc io.ReadCloser, d time.Duration) io.ReadCloser {
	sb := &stalledBody{rc: rc, timeout: d}
	sb.timer = time.AfterFunc(d, func() {
		sb.timed.Store(true)
		_ = rc.Close()
	})
	return sb
}

func (s *stalledBody) Read(p []byte) (int, error) {
	s.timer.Reset(s.timeout)
	n, err := s.rc.Read(p)
	if err != nil && s.timed.Load() {
		return n, fmt.Errorf("下载超时（超过 %v 未收到数据）", s.timeout)
	}
	return n, err
}

func (s *stalledBody) Close() error {
	s.timer.Stop()
	return s.rc.Close()
}

func validateFetchURL(rawURL string) error {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("无效下载地址 %q: %w", rawURL, err)
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" && isLoopbackHost(u.Hostname()) {
		return nil
	}
	return fmt.Errorf("安全策略要求使用 HTTPS 下载地址: %q", rawURL)
}

func isLoopbackHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func (c *Client) extractArchive(ctx context.Context, zipPath, extractDir string) error {
	if c.effectiveOS() == "darwin" {
		if _, err := os.Stat("/usr/bin/ditto"); err == nil {
			cmd := exec.CommandContext(ctx, "/usr/bin/ditto", "-x", "-k", zipPath, extractDir)
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
			}
			return nil
		}
	}
	return extractZipSafe(zipPath, extractDir)
}

func extractZipSafe(zipPath, dstDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	cleanDst := filepath.Clean(dstDir) + string(os.PathSeparator)
	for _, f := range r.File {
		target := filepath.Clean(filepath.Join(dstDir, f.Name))
		if !strings.HasPrefix(target, cleanDst) && target != filepath.Clean(dstDir) {
			return fmt.Errorf("非法的压缩包路径 %q", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		mode := f.Mode()
		if mode.Perm() == 0 {
			mode = 0o644
		}
		if mode&os.ModeSymlink != 0 {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			linkBytes, err := io.ReadAll(io.LimitReader(rc, 4096))
			_ = rc.Close()
			if err != nil {
				return err
			}
			_ = os.Remove(target)
			if err := os.Symlink(string(linkBytes), target); err != nil {
				return err
			}
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())
		if err != nil {
			_ = rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, io.LimitReader(rc, maxAssetSize))
		closeErr := out.Close()
		_ = rc.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func findAndVerifyAppBundle(extractDir, expectedBundleID string) (string, error) {
	entries, err := os.ReadDir(extractDir)
	if err != nil {
		return "", err
	}
	var apps []string
	for _, e := range entries {
		if e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".app") {
			apps = append(apps, filepath.Join(extractDir, e.Name()))
		}
	}
	if len(apps) != 1 {
		return "", fmt.Errorf("更新包中未找到唯一的 .app 应用包（共发现 %d 个）", len(apps))
	}
	appDir := apps[0]

	plistPath := filepath.Join(appDir, "Contents", "Info.plist")
	plistData, err := os.ReadFile(plistPath)
	if err != nil {
		return "", fmt.Errorf("更新包缺少 Contents/Info.plist: %w", err)
	}
	if expectedBundleID != "" && !strings.Contains(string(plistData), "<string>"+expectedBundleID+"</string>") {
		return "", fmt.Errorf("更新包 Bundle ID 与当前应用不一致（期望 %s）", expectedBundleID)
	}

	macOSDir := filepath.Join(appDir, "Contents", "MacOS")
	execEntries, err := os.ReadDir(macOSDir)
	if err != nil || len(execEntries) == 0 {
		return "", errors.New("更新包缺少 Contents/MacOS 可执行文件")
	}
	return appDir, nil
}
