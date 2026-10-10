package serverplugin

import "strings"

// coreEvent 是核心 API 请求在插件钩子体系中的前后事件名
type coreEvent struct {
	Before string
	After  string
}

// coreRequestEvent 将核心 API 的方法与路径映射为一对前后事件
// 未命中任何规则时返回零值，表示该请求不触发插件钩子
func coreRequestEvent(method, requestPath string) coreEvent {
	switch {
	case requestPath == "/api/auth/register" && method == "POST":
		return coreEvent{Before: "user.before_create", After: "user.created"}
	case requestPath == "/api/auth/login" && method == "POST":
		return coreEvent{Before: "auth.before_login", After: "auth.logged_in"}
	case requestPath == "/api/vaults" && method == "POST":
		return coreEvent{Before: "vault.before_create", After: "vault.created"}
	case strings.Contains(requestPath, "/members"):
		return mutationEvent(method, "vault.member")
	case strings.Contains(requestPath, "/collaborations"):
		return mutationEvent(method, "collaboration")
	case strings.Contains(requestPath, "/shares") || requestPath == "/api/shares":
		return mutationEvent(method, "share")
	case strings.Contains(requestPath, "/devices"):
		return mutationEvent(method, "device")
	case strings.Contains(requestPath, "/settings"):
		return mutationEvent(method, "settings")
	case strings.Contains(requestPath, "/sync/upload"):
		return coreEvent{Before: "file.before_save", After: "file.saved"}
	case strings.Contains(requestPath, "/sync/delete"):
		return coreEvent{Before: "file.before_delete", After: "file.deleted"}
	case strings.Contains(requestPath, "/sync/rename"):
		return coreEvent{Before: "file.before_rename", After: "file.renamed"}
	case strings.HasPrefix(requestPath, "/api/vaults/"):
		return mutationEvent(method, "vault")
	default:
		return coreEvent{}
	}
}

// mutationEvent 按 HTTP 方法拼接某领域的增删改事件
// GET、HEAD 等只读方法返回零值
func mutationEvent(method, domain string) coreEvent {
	switch method {
	case "POST":
		return coreEvent{Before: domain + ".before_create", After: domain + ".created"}
	case "PUT", "PATCH":
		return coreEvent{Before: domain + ".before_update", After: domain + ".updated"}
	case "DELETE":
		return coreEvent{Before: domain + ".before_delete", After: domain + ".deleted"}
	default:
		return coreEvent{}
	}
}
