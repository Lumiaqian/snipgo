package main

import (
	"strings"
	"testing"
)

// 权限提示面向所有用户：不能只按「开发版重新编译」解释，需要同时说明首次授权与签名变化两种处理方式。
func TestPermissionPromptCoversFirstGrantAndSignatureChange(t *testing.T) {
	opts := permissionPromptOptions()
	text := opts.Title + opts.Message + opts.Detail
	if strings.Contains(text, "开发版") {
		t.Fatalf("提示不应只面向开发版: %q", text)
	}
	for _, want := range []string{"从未授权", "录屏与系统录音", "重置旧记录并重新授权", "0.1.7", "固定签名"} {
		if !strings.Contains(text, want) {
			t.Fatalf("提示缺少 %q: %q", want, text)
		}
	}
	if len(opts.Buttons) != 3 || opts.CancelButton != 2 {
		t.Fatalf("按钮顺序被改动: %v cancel=%d", opts.Buttons, opts.CancelButton)
	}
}
