package main

import "github.com/egoist/mygo"

// 后台计算与主线程调度的注入点：生产环境为 goroutine + mygo.RunOnMain，测试可替换以确定性地控制时序。
var (
	runBackground = func(fn func()) { go fn() }
	runOnMain     = mygo.RunOnMain
)
