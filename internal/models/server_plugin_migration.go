package models

import "time"

// ServerPluginMigration 记录宿主已应用的一次受信插件迁移。
type ServerPluginMigration struct {
	PluginID  string `gorm:"primaryKey;size:64"`
	ID        string `gorm:"primaryKey;size:128"`
	AppliedAt time.Time
}
