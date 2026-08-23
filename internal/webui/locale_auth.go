// 认证文案
package webui

func init() {
	registerEntries(map[string][2]string{
		"common.main_navigation":       {"主导航", "Main navigation"},
		"common.site_console":          {"OSS Sync 控制台", "OSS Sync console"},
		"common.open_menu":             {"打开菜单", "Open menu"},
		"common.close_menu":            {"关闭菜单", "Close menu"},
		"common.dismiss":               {"关闭提示", "Dismiss"},
		"common.theme_mode":            {"主题模式", "Theme mode"},
		"auth.login_aria_label":        {"OSS Sync 登录", "OSS Sync sign-in"},
		"auth.register_link":           {"注册账户", "Create account"},
		"auth.login_title":             {"登录你的同步账本。", "Log in to your sync ledger."},
		"auth.login_subtitle":          {"使用创建账户时的用户名和密码。管理员与普通用户使用同一个登录入口。", "Use the username and password you set when creating your account. Admins and regular users share the same sign-in page."},
		"auth.login_button":            {"进入控制台", "Enter console"},
		"auth.login_aside_title":       {"一个账户，多台设备。", "One account, multiple devices."},
		"auth.login_aside_text":        {"登录后管理你的仓库、设备授权、分享、历史与回收站。管理员可在同一侧边栏进入管理后台。", "Manage your vaults, device approvals, shares, history, and recycle bin after sign-in. Admins can access the admin panel from the same sidebar."},
		"auth.register_aria_label":     {"OSS Sync 注册页", "OSS Sync registration"},
		"auth.register_login_link":     {"已有账户，登录", "Have an account? Sign in"},
		"auth.register_status_label":   {"注册状态", "Registration status"},
		"auth.register_open_text":      {"创建一次账户，随后在每台 Obsidian 设备中使用同一组凭据登录。", "Create your account once, then use the same credentials on every Obsidian device."},
		"auth.register_closed_text":    {"管理员暂时停止了新账户创建。已有账户仍可正常登录和同步。", "The administrator has temporarily paused new account creation. Existing accounts can still sign in and sync normally."},
		"auth.register_step_create":    {"创建账户", "Create account"},
		"auth.register_step_settings":  {"打开插件设置", "Open plugin settings"},
		"auth.register_step_bind":      {"登录、创建并绑定 Vault", "Sign in, create and bind vault"},
		"auth.register_success_title":  {"账户已写入同步账本。", "Account added to the sync ledger."},
		"auth.register_created_prefix": {"用户 ", "User "},
		"auth.register_created_suffix": {" 已创建。", " created."},
		"auth.register_success_text":   {"回到 Obsidian 的 OSS Sync 设置完成登录，或在网页继续使用同一账户登录控制台。", "Go back to Obsidian's OSS Sync settings to sign in, or continue to the console in your browser with the same account."},
		"auth.register_success_link":   {"前往登录", "Go to sign in"},
		"auth.register_suspended_title": {"新用户注册已暂停。", "New user registration suspended."},
		"auth.register_suspended_text":  {"这不会影响已有用户。需要新账户时，请联系服务管理员重新开放注册。", "This does not affect existing users. Contact the server administrator to reopen registration if you need a new account."},
		"auth.register_form_title":      {"为你的笔记建立一个同步身份。", "Create a sync identity for your notes."},
		"auth.register_form_text":       {"凭据只用于这个自托管服务。注册后，使用它登录控制台或 Obsidian 插件。", "Credentials are only for this self-hosted service. After registration, use them to sign in to the console or the Obsidian plugin."},
		"auth.register_username_hint":   {"3–64 个字符", "3–64 characters"},
		"auth.register_password_hint":   {"至少 8 个字符、最多 72 字节；请使用独立密码", "At least 8 characters, max 72 bytes; use a unique password"},
		"auth.register_button":          {"创建账户", "Create account"},
	})
}

