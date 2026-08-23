// 页面文案
package webui

func init() {
	registerEntries(map[string][2]string{
		"page.overview":               {"首页", "Home"},
		"page.vaults":                 {"我的仓库", "My vaults"},
		"page.new_vault":              {"新建仓库", "New vault"},
		"page.vault_files":            {"%s", "%s"},
		"page.shares":                 {"分享管理", "Shares"},
		"page.vault_shares":           {"%s · 分享管理", "%s · Shares"},
		"page.recycle":                {"回收站", "Recycle bin"},
		"page.vault_recycle":          {"%s · 回收站", "%s · Recycle bin"},
		"page.vault_settings":         {"%s · 仓库设置", "%s · Vault settings"},
		"page.devices":                {"设备管理", "Device management"},
		"page.vault_history":          {"%s · 修改记录", "%s · History"},
		"page.admin_users":            {"用户管理", "User management"},
		"page.admin_vaults":           {"全部仓库", "All vaults"},
		"page.admin_vault_detail":     {"%s", "%s"},
		"page.admin_devices":          {"全部设备", "All devices"},
		"page.admin_system":           {"系统设置", "System settings"},
		"page.admin_data":             {"数据信息", "Data information"},
		"page.admin_themes":           {"模板管理", "Template management"},
		"page.admin_console_themes":   {"服务器主题管理", "Server theme management"},
		"page.account":                {"个人中心", "Account"},
		"page.vault_theme_settings":   {"%s · %s 设置", "%s · %s Settings"},
		"page.vault_members":          {"%s · 协作成员", "%s · Members"},
		"page.vault_history_detail":   {"%s · 修改详情", "%s · History detail"},
		"page.vault_preview":          {"%s · %s", "%s · %s"},
	})
}

