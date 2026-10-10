package blog

import (
	"context"
	"encoding/json"
	"testing"
)

// stubHomePostsHook 记录收到的 payload 并返回预设结果，供 handleVaultBlog 测试 hook 调用
type stubHomePostsHook struct {
	calls   int
	payload HomePostsHookPayload
	result  []HomePost
	err     error
}

func (s *stubHomePostsHook) ApplyHook(_ context.Context, _ string, _ PluginHookPayload) (string, error) {
	return "", nil
}

func (s *stubHomePostsHook) ApplyHomePostsHook(_ context.Context, p HomePostsHookPayload) ([]HomePost, error) {
	s.calls++
	s.payload = p
	return s.result, s.err
}

func TestHandleVaultBlogAppliesHomePostsHook(t *testing.T) {
	// 有 hook 时 post 列表应被插件过滤或重排
	hook := &stubHomePostsHook{
		result: []HomePost{{Title: "pinned", URL: "/p/pinned"}},
	}
	// 验证 hook 契约：传入原始列表，返回过滤后的列表
	original := []HomePost{{Title: "a", URL: "/p/a"}, {Title: "b", URL: "/p/b"}}
	got, err := hook.ApplyHomePostsHook(context.Background(), HomePostsHookPayload{VaultID: "v1", Posts: original})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "pinned" {
		t.Fatalf("filtered = %#v, want only pinned", got)
	}
	if hook.payload.VaultID != "v1" {
		t.Fatalf("payload.VaultID = %q, want v1", hook.payload.VaultID)
	}
}

func TestHomePostsHookPayloadJSONRoundTrip(t *testing.T) {
	// payload 序列化格式是宿主与插件之间的契约，字段名不能随意漂移
	payload := HomePostsHookPayload{
		VaultID: "vault-1",
		Posts:   []HomePost{{Title: "Hello", URL: "/p/hello", Tags: []string{"go", "blog"}, Category: "tech"}},
		Query:   map[string][]string{"tag": {"go"}},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var decoded HomePostsHookPayload
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.VaultID != "vault-1" || len(decoded.Posts) != 1 || decoded.Posts[0].Tags[0] != "go" {
		t.Fatalf("round trip = %#v", decoded)
	}
	if decoded.Query["tag"][0] != "go" {
		t.Fatalf("query round trip = %#v", decoded.Query)
	}
}
