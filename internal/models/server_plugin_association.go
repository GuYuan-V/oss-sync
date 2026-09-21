package models

// ServerPluginAssociation 将受信插件与其所属主题关联
type ServerPluginAssociation struct {
	PluginID   string `gorm:"primaryKey;size:64"`
	Kind       string `gorm:"primaryKey;size:32"`
	TargetID   string `gorm:"primaryKey;size:64"`
	TargetName string `gorm:"size:128;not null"`
}
