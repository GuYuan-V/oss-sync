package models

// VaultPluginSetting 存放宿主为某 Vault 管理的单个插件设置。
type VaultPluginSetting struct {
	VaultID  string  `gorm:"primaryKey;size:36"`
	PluginID string  `gorm:"primaryKey;size:64"`
	Config   JSONMap `gorm:"type:json"`
}
