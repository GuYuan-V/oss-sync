package models

// VaultPluginSetting stores one plugin's host-managed settings for a Vault.
type VaultPluginSetting struct {
	VaultID  string  `gorm:"primaryKey;size:36"`
	PluginID string  `gorm:"primaryKey;size:64"`
	Config   JSONMap `gorm:"type:json"`
}
