// Command md-editor 是一个受信服务端插件，在控制台内提供 Markdown 文件的在线编辑与保存
package main

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"

	"github.com/helantianshen/oss-sync/pkg/ossplugin"
)

//go:embed editor.html
var editorHTML string

func main() {
	registration := ossplugin.Registration{
		Routes: []ossplugin.Route{
			{Method: "GET", Path: "/md-editor", Callback: "editor.page", Auth: "user"},
			{Method: "GET", Path: "/md-editor/content", Callback: "editor.content", Auth: "user"},
			{Method: "POST", Path: "/md-editor/save", Callback: "editor.save", Auth: "user"},
		},
		Assets: []ossplugin.Asset{
			{Path: "easymde.min.js"},
			{Path: "easymde.min.css"},
			{Path: "fontawesome.min.css"},
			{Path: "fonts/fontawesome-webfont.woff2"},
		},
	}
	ossplugin.RunMain(registration, func(client *ossplugin.Client) error {
		if err := client.On("editor.page", editorPage()); err != nil {
			return err
		}
		if err := client.On("editor.content", editorContent(client)); err != nil {
			return err
		}
		return client.On("editor.save", editorSave(client))
	})
}

func editorPage() ossplugin.Handler {
	return func(context.Context, ossplugin.Request) (ossplugin.Response, error) {
		return ossplugin.Response{
			Status:     200,
			Headers:    map[string]string{"Content-Type": "text/html; charset=utf-8"},
			BodyBase64: base64.StdEncoding.EncodeToString([]byte(editorHTML)),
		}, nil
	}
}

func editorContent(client *ossplugin.Client) ossplugin.Handler {
	return func(ctx context.Context, req ossplugin.Request) (ossplugin.Response, error) {
		vaultID := firstQuery(req, "vault_id")
		path := firstQuery(req, "path")
		if vaultID == "" || path == "" {
			return jsonError(client, 400, "vault_id 和 path 必填")
		}
		if !ownsVault(ctx, client, req, vaultID) {
			return jsonError(client, 403, "无权访问该 vault")
		}
		file, err := client.Services().GetFile(ctx, vaultID, path)
		if err != nil {
			// 文件尚不存在时打开空白内容，允许在编辑器内新建
			return client.WriteJSONResponse(200, map[string]any{"content": "", "revision": 0})
		}
		return client.WriteJSONResponse(200, map[string]any{"content": file.Content, "revision": file.Revision})
	}
}

func editorSave(client *ossplugin.Client) ossplugin.Handler {
	return func(ctx context.Context, req ossplugin.Request) (ossplugin.Response, error) {
		var body struct {
			VaultID string `json:"vault_id"`
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := decodeBody(req, &body); err != nil {
			return jsonError(client, 400, "请求体无效")
		}
		if body.VaultID == "" || body.Path == "" {
			return jsonError(client, 400, "vault_id 和 path 必填")
		}
		if !ownsVault(ctx, client, req, body.VaultID) {
			return jsonError(client, 403, "无权写入该 vault")
		}
		result, err := client.Services().PutFile(ctx, body.VaultID, body.Path, body.Content)
		if err != nil {
			return jsonError(client, 500, "保存失败: "+err.Error())
		}
		return client.WriteJSONResponse(200, map[string]any{"revision": result.Revision, "size": result.Size, "hash": result.Hash})
	}
}

// ownsVault 仅允许对当前登录用户拥有的 vault 进行读写，避免越权访问他人 vault
func ownsVault(ctx context.Context, client *ossplugin.Client, req ossplugin.Request, vaultID string) bool {
	if req.User == nil {
		return false
	}
	rows, err := client.Services().Query(ctx, "SELECT owner_id FROM vaults WHERE id = ?", vaultID)
	if err != nil || len(rows) == 0 {
		return false
	}
	return toUint(rows[0]["owner_id"]) == req.User.ID
}

func firstQuery(req ossplugin.Request, key string) string {
	if values, ok := req.Query[key]; ok && len(values) > 0 {
		return values[0]
	}
	return ""
}

func decodeBody(req ossplugin.Request, out any) error {
	raw, err := base64.StdEncoding.DecodeString(req.BodyBase64)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func jsonError(client *ossplugin.Client, status int, message string) (ossplugin.Response, error) {
	return client.WriteJSONResponse(status, map[string]any{"error": message})
}

func toUint(value any) uint {
	switch v := value.(type) {
	case int64:
		return uint(v)
	case int:
		return uint(v)
	case uint:
		return v
	case float64:
		return uint(v)
	default:
		return 0
	}
}
