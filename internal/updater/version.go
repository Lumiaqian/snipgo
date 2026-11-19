// Package updater 提供 SnipGo 基于 GitHub Releases 的版本检查、SHA-256 校验与 macOS 原子热替换更新能力。
package updater

import (
	"fmt"
	"strconv"
	"strings"
)

// Version 表示解析后的语义化版本号（SemVer 2.0.0 子集，兼容 2~3 段数字及预发布标签）。
type Version struct {
	Major      int
	Minor      int
	Patch      int
	Prerelease []string
	Raw        string
}

// ParseVersion 解析形如 "v0.1.0"、"0.2.0-beta.1" 或 "1.2" 的版本号字符串。
func ParseVersion(raw string) (Version, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return Version{}, fmt.Errorf("空版本号")
	}
	orig := s
	if strings.HasPrefix(s, "v") || strings.HasPrefix(s, "V") {
		s = s[1:]
	}
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return Version{}, fmt.Errorf("无效版本号 %q", raw)
	}

	var pre []string
	if i := strings.IndexByte(s, '-'); i >= 0 {
		preStr := s[i+1:]
		s = s[:i]
		if preStr == "" {
			return Version{}, fmt.Errorf("无效预发布版本号 %q", raw)
		}
		pre = strings.Split(preStr, ".")
		for _, tok := range pre {
			if tok == "" {
				return Version{}, fmt.Errorf("无效预发布标识 %q", raw)
			}
		}
	}

	parts := strings.Split(s, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return Version{}, fmt.Errorf("无效版本号格式 %q", raw)
	}
	nums := make([]int, 3)
	for i, part := range parts {
		if part == "" {
			return Version{}, fmt.Errorf("无效数字段 %q", raw)
		}
		for j := 0; j < len(part); j++ {
			if part[j] < '0' || part[j] > '9' {
				return Version{}, fmt.Errorf("无效数字段 %q", raw)
			}
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return Version{}, fmt.Errorf("无效数字段 %q", raw)
		}
		nums[i] = n
	}

	return Version{
		Major:      nums[0],
		Minor:      nums[1],
		Patch:      nums[2],
		Prerelease: pre,
		Raw:        orig,
	}, nil
}

// CompareVersion 按 SemVer 2.0.0 规则比较版本号 a 与 b（返回 -1、0 或 1）。
func CompareVersion(a, b string) int {
	va, errA := ParseVersion(a)
	vb, errB := ParseVersion(b)
	switch {
	case errA != nil && errB != nil:
		return strings.Compare(strings.TrimSpace(a), strings.TrimSpace(b))
	case errA != nil:
		return -1
	case errB != nil:
		return 1
	}
	return compareParsed(va, vb)
}

func compareParsed(a, b Version) int {
	if a.Major != b.Major {
		if a.Major < b.Major {
			return -1
		}
		return 1
	}
	if a.Minor != b.Minor {
		if a.Minor < b.Minor {
			return -1
		}
		return 1
	}
	if a.Patch != b.Patch {
		if a.Patch < b.Patch {
			return -1
		}
		return 1
	}

	// 正式版 > 预发布版
	if len(a.Prerelease) == 0 && len(b.Prerelease) == 0 {
		return 0
	}
	if len(a.Prerelease) == 0 {
		return 1
	}
	if len(b.Prerelease) == 0 {
		return -1
	}

	n := min(len(a.Prerelease), len(b.Prerelease))
	for i := range n {
		if c := comparePreToken(a.Prerelease[i], b.Prerelease[i]); c != 0 {
			return c
		}
	}
	if len(a.Prerelease) < len(b.Prerelease) {
		return -1
	}
	if len(a.Prerelease) > len(b.Prerelease) {
		return 1
	}
	return 0
}

func comparePreToken(a, b string) int {
	if a == b {
		return 0
	}
	na, okA := parseDigits(a)
	nb, okB := parseDigits(b)
	switch {
	case okA && okB:
		if na < nb {
			return -1
		}
		if na > nb {
			return 1
		}
		return 0
	case okA && !okB:
		// SemVer §11.4.3：纯数字预发布字段优先级低于字母数字字段
		return -1
	case !okA && okB:
		return 1
	default:
		if a < b {
			return -1
		}
		return 1
	}
}

func parseDigits(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	v, err := strconv.Atoi(s)
	return v, err == nil
}

// IsNewer 判断 candidate 是否严格高于 current。若 candidate 无法解析则返回 false。
func IsNewer(candidate, current string) bool {
	vc, errC := ParseVersion(candidate)
	if errC != nil {
		return false
	}
	vr, errR := ParseVersion(current)
	if errR != nil {
		return false
	}
	return compareParsed(vc, vr) > 0
}

// FormatVersion 将版本号规范化为 "vX.Y.Z..." 展示形式。
func FormatVersion(v string) string {
	s := strings.TrimSpace(v)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "V") {
		return "v" + s[1:]
	}
	if strings.HasPrefix(s, "v") {
		return s
	}
	return "v" + s
}
