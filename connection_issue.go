package main

import (
	"errors"
	"retty/internal/remote"
)

type mobileConnectionIssue struct {
	title, body      string
	automatic, retry bool
}

func connectionIssueFor(err error) *mobileConnectionIssue {
	issue := &mobileConnectionIssue{title: "暂时无法连接桌面", body: "请确认电脑上的 Retty 正在运行，并检查两端网络。若电脑曾停止连接，请重新扫码。", automatic: true, retry: true}
	switch remote.Failure(err) {
	case remote.InvalidLink:
		issue.title, issue.body = "连接码无法识别", "请在电脑的 Retty「设置 → 连接」中显示二维码，重新扫描。"
		issue.automatic, issue.retry = false, false
	case remote.NetworkUnavailable:
		issue.title, issue.body = "当前网络不可用", "请检查 Wi-Fi、蜂窝网络，以及 Retty 的联网权限，再重试。"
	case remote.RelayUnavailable:
		issue.title, issue.body = "无法访问连接服务", "请检查当前网络或 VPN，再重试。"
	case remote.ConnectionTimeout:
		issue.title = "连接超时"
	case remote.ProtocolMismatch:
		issue.title, issue.body = "桌面版本不兼容", "请更新电脑上的 Retty，再重试连接。"
		issue.automatic = false
	}
	var failure *remote.ConnectionError
	if errors.As(err, &failure) && failure.Stage != "" {
		issue.body = "未完成：" + string(failure.Stage) + "。" + issue.body
	}
	return issue
}
