package models

import (
	"time"

	"gorm.io/gorm"
)

type AcmeConfig struct {
	gorm.Model

	Enabled     bool       `json:"enabled" gorm:"default:false"`
	ServerIP    string     `json:"server_ip" gorm:"size:64"`
	Email       string     `json:"email" gorm:"size:255"`
	Webroot     string     `json:"webroot" gorm:"size:255;default:'/var/www/html'"`
	CertPath    string     `json:"cert_path" gorm:"size:500"`
	KeyPath     string     `json:"key_path" gorm:"size:500"`
	LastRenewed *time.Time `json:"last_renewed"`
	ExpiresAt   *time.Time `json:"expires_at"`
	LastError   string     `json:"last_error" gorm:"type:text"`
}

func (AcmeConfig) TableName() string {
	return "acme_configs"
}
