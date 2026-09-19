package serverplugin

import "strings"

type coreEvent struct {
	Before string
	After  string
}

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
