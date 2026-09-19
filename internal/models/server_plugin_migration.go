package models

import "time"

// ServerPluginMigration records one trusted plugin migration applied by the host.
type ServerPluginMigration struct {
	PluginID  string `gorm:"primaryKey;size:64"`
	ID        string `gorm:"primaryKey;size:128"`
	AppliedAt time.Time
}
