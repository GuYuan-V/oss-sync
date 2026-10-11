package main

import (
	"context"
	"strings"

	"github.com/helantianshen/oss-sync/pkg/ossplugin"
)

func main() {
	registration := ossplugin.Registration{
		Settings: []ossplugin.SettingField{
			{Key: "blog_name", Label: "博客名称", Type: "text", MaxLength: 120, Section: "首页设置"},
			{Key: "banner_url", Label: "横幅地址", Type: "url", MaxLength: 512, Section: "首页设置"},
			{Key: "pinned_tags", Label: "置顶标签", Type: "text", MaxLength: 120, Section: "文章设置"},
			{Key: "default_sort", Label: "默认排序", Type: "text", MaxLength: 120, Section: "文章设置"},
		},
		Hooks: []ossplugin.Hook{
			{Name: "blog.home_posts", Kind: "filter", Callback: "posts.filter"},
		},
	}

	ossplugin.RunMain(registration, func(client *ossplugin.Client) error {
		return client.On("posts.filter", func(ctx context.Context, req ossplugin.Request) (ossplugin.Response, error) {
			payload := req.Payload
			posts, _ := payload["posts"].([]any)
			var pinned []any
			var rest []any
			for _, p := range posts {
				post, ok := p.(map[string]any)
				if !ok {
					rest = append(rest, p)
					continue
				}
				title, _ := post["Title"].(string)
				if strings.Contains(title, "置顶") {
					pinned = append(pinned, p)
				} else {
					rest = append(rest, p)
				}
			}
			result := append(pinned, rest...)
			return client.WriteJSONResponse(200, result)
		})
	})
}
