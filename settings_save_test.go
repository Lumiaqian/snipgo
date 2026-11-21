package main

import "testing"

func TestSavedBadgeTextReflectsSaveFailure(t *testing.T) {
	if got := savedBadgeText("✓ 已保存", false); got != "✓ 已保存" {
		t.Fatalf("保存成功应保持原文案, got %q", got)
	}
	if got := savedBadgeText("✓ 已保存", true); got == "✓ 已保存" {
		t.Fatal("保存失败时不应显示成功文案")
	}
	if got := savedBadgeText("请先录制", true); got != "请先录制" {
		t.Fatalf("非成功文案不应被改写, got %q", got)
	}
}

func TestHistoryEnabledFollowsConfig(t *testing.T) {
	a := &app{}
	if !a.historyEnabled() {
		t.Fatal("默认应保存历史")
	}
	a.cfg.DisableHistory = true
	if a.historyEnabled() {
		t.Fatal("DisableHistory=true 时应关闭历史")
	}
}
