// Package main 提供服务端插件 SDK 示例

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/helantianshen/oss-sync/pkg/ossplugin"
)

func main() {
	registration := ossplugin.Registration{
		Hooks: []ossplugin.Hook{
			{Name: "blog.content", Callback: "blog.content", Kind: "filter"},
			{Name: "orders.before_save", Callback: "orders.before_save", Kind: "action"},
		},
		Routes: []ossplugin.Route{
			{Method: "GET", Path: "/hello", Callback: "GET:/hello", Auth: "public"},
			{Method: "GET", Path: "/dynamic-hello", Callback: "dynamic.hello", Auth: "public"},
			{Method: "GET", Path: "/host-models", Callback: "host.models", Auth: "public"},
		},
		Middleware: []ossplugin.Middleware{{Name: "audit", Callback: "audit.request", Stage: "before", PathPrefix: "/api/"}},
		AdminPages: []ossplugin.AdminPage{{Slug: "orders", Label: "Orders", Callback: "orders.admin"}},
		Tasks:      []ossplugin.Task{{Name: "sync_orders", Schedule: "@hourly", Callback: "orders.sync"}},
		Migrations: []ossplugin.Migration{{ID: "orders_v1", Statements: []string{"CREATE TABLE IF NOT EXISTS orders (id INTEGER NOT NULL)"}}},
		Lifecycle:  ossplugin.Lifecycle{Activate: "plugin.activate", Deactivate: "plugin.deactivate", Uninstall: "plugin.uninstall"},
	}
	ossplugin.RunMain(registration, func(client *ossplugin.Client) error {
		for callback, handler := range pluginHandlers(client) {
			if err := client.On(callback, handler); err != nil {
				return err
			}
		}
		return client.On("blog.content", func(_ context.Context, request ossplugin.Request) (ossplugin.Response, error) {
			content, _ := request.Payload["content"].(string)
			return client.WriteTextResponse(200, "executable:"+content), nil
		})
	})
}

func pluginHandlers(client *ossplugin.Client) map[string]ossplugin.Handler {
	return map[string]ossplugin.Handler{
		"GET:/hello":         textHandler(client, "hello from SDK"),
		"POST:/echo":         textHandler(client, "echo from SDK"),
		"dynamic.hello":      pathHandler(client),
		"host.models":        hostModelsHandler(client),
		"orders.admin":       textHandler(client, "<h1>Orders</h1>"),
		"orders.sync":        textHandler(client, "sync complete"),
		"audit.request":      nextMiddleware(client),
		"orders.before_save": textHandler(client, "action complete"),
		"plugin.activate":    textHandler(client, "activated"),
		"plugin.deactivate":  textHandler(client, "deactivated"),
		"plugin.uninstall":   textHandler(client, "uninstalled"),
	}
}

func textHandler(client *ossplugin.Client, text string) ossplugin.Handler {
	return func(context.Context, ossplugin.Request) (ossplugin.Response, error) {
		return client.WriteTextResponse(200, text), nil
	}
}

func pathHandler(client *ossplugin.Client) ossplugin.Handler {
	return func(context.Context, ossplugin.Request) (ossplugin.Response, error) {
		return client.WriteTextResponse(200, "/dynamic-hello"), nil
	}
}

func nextMiddleware(client *ossplugin.Client) ossplugin.Handler {
	return func(context.Context, ossplugin.Request) (ossplugin.Response, error) {
		return client.WriteTextResponse(204, ""), nil
	}
}

func hostModelsHandler(client *ossplugin.Client) ossplugin.Handler {
	return func(ctx context.Context, request ossplugin.Request) (ossplugin.Response, error) {
		models, err := client.Services().Models(ctx)
		if err != nil {
			return ossplugin.Response{}, err
		}
		encoded, err := json.Marshal(models)
		if err != nil {
			return ossplugin.Response{}, fmt.Errorf("encode host models: %w", err)
		}
		return ossplugin.Response{Status: 200, BodyBase64: base64.StdEncoding.EncodeToString(encoded)}, nil
	}
}
