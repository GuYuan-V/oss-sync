package models

// ServerPluginAssociation links a trusted plugin to the theme that bundled it.
type ServerPluginAssociation struct {
	PluginID   string `gorm:"primaryKey;size:64"`
	Kind       string `gorm:"primaryKey;size:32"`
	TargetID   string `gorm:"primaryKey;size:64"`
	TargetName string `gorm:"size:128;not null"`
}
