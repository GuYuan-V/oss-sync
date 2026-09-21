package models

import "time"

// ServerPlugin 记录一条管理员安装的服务端扩展
type ServerPlugin struct {
	ID           string `gorm:"primaryKey;size:64"`
	Name         string `gorm:"size:128;not null"`
	Version      string `gorm:"size:64;not null"`
	Description  string `gorm:"size:2000"`
	APIVersion   int    `gorm:"not null"`
	Runtime      string `gorm:"size:32;not null;default:'wasm'"`
	ManifestJSON string `gorm:"type:text;not null"`
	ManifestHash string `gorm:"size:64;not null"`
	WasmHash     string `gorm:"size:64;not null"`
	WasmSize     int64  `gorm:"not null;default:0"`
	PayloadHash  string `gorm:"size:64;not null;default:''"`
	PayloadSize  int64  `gorm:"not null;default:0"`
	Enabled      bool   `gorm:"not null;default:false"`
	Builtin      bool   `gorm:"not null;default:false"`
	LastError    string `gorm:"type:text"`
	InstalledAt  time.Time
	UpdatedAt    time.Time
}
