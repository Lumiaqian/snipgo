package updater

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DetectAppBundle 根据当前可执行文件路径解析所属的 macOS .app Bundle 根目录。
// 当运行在 go run / 测试二进制等非 .app 环境中时返回描述性错误。
func DetectAppBundle(exePath string) (string, error) {
	s := strings.TrimSpace(exePath)
	if s == "" {
		return "", fmt.Errorf("无法确定当前可执行文件路径")
	}
	if resolved, err := filepath.EvalSymlinks(s); err == nil {
		s = resolved
	}
	s = filepath.Clean(s)

	macOSDir := filepath.Dir(s)
	contentsDir := filepath.Dir(macOSDir)
	bundleDir := filepath.Dir(contentsDir)

	if filepath.Base(macOSDir) != "MacOS" ||
		filepath.Base(contentsDir) != "Contents" ||
		!strings.HasSuffix(strings.ToLower(filepath.Base(bundleDir)), ".app") {
		return "", fmt.Errorf("当前并非从独立 .app 应用包运行（开发调试模式下请下载安装包更新）")
	}
	return bundleDir, nil
}

// CheckWritableBundle 检查目标 .app Bundle 及其所在目录是否具备原地替换权限，
// 并拦截 macOS App Translocation 隔离路径或只读 DMG 卷挂载路径。
func CheckWritableBundle(bundlePath string) error {
	clean := filepath.Clean(strings.TrimSpace(bundlePath))
	if clean == "" || clean == "." {
		return fmt.Errorf("应用包路径为空")
	}
	if !strings.HasSuffix(strings.ToLower(clean), ".app") {
		return fmt.Errorf("目标路径 %q 不是有效的 .app 应用包", clean)
	}
	if strings.Contains(clean, "/AppTranslocation/") {
		return fmt.Errorf("应用正运行于 macOS 随机隔离路径，请先将其移入「应用程序」目录后再自动更新")
	}
	if strings.HasPrefix(clean, "/Volumes/") {
		return fmt.Errorf("应用正直接从只读磁盘映像运行，请先拖入「应用程序」目录后再自动更新")
	}

	st, err := os.Stat(clean)
	if err != nil || !st.IsDir() {
		return fmt.Errorf("找不到应用包目录 %q", clean)
	}

	parent := filepath.Dir(clean)
	probeParent, err := os.CreateTemp(parent, ".snipgo-perm-*")
	if err != nil {
		return fmt.Errorf("安装目录不可写（%s），请通过安装包手动覆盖更新", parent)
	}
	_ = probeParent.Close()
	_ = os.Remove(probeParent.Name())

	probeBundle, err := os.CreateTemp(clean, ".snipgo-perm-*")
	if err != nil {
		return fmt.Errorf("应用包无写入权限（%s），请通过安装包手动覆盖更新", clean)
	}
	_ = probeBundle.Close()
	_ = os.Remove(probeBundle.Name())

	return nil
}

// ParseSHA256Sums 解析 `shasum -a 256` 文本，返回以文件名为键、小写 64 位十六进制摘要为值的映射。
func ParseSHA256Sums(content string) map[string]string {
	out := make(map[string]string)
	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		hexSum := strings.ToLower(strings.TrimSpace(fields[0]))
		if !isHex64(hexSum) {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		name = filepath.Base(strings.TrimPrefix(name, "./"))
		if name == "" || name == "." || name == "/" {
			continue
		}
		out[name] = hexSum
	}
	return out
}

func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		isDigit := c >= '0' && c <= '9'
		isHex := c >= 'a' && c <= 'f'
		if !isDigit && !isHex {
			return false
		}
	}
	return true
}

// FormatBytes 将字节数格式化为人类可读的 B / KB / MB / GB 字符串。
func FormatBytes(n int64) string {
	if n < 0 {
		n = 0
	}
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case n < kb:
		return fmt.Sprintf("%d B", n)
	case n < mb:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(kb))
	case n < gb:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mb))
	default:
		return fmt.Sprintf("%.2f GB", float64(n)/float64(gb))
	}
}
