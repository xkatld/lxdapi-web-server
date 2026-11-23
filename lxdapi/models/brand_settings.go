package models

import "time"

type BrandSettings struct {
	ID                   int       `gorm:"primaryKey" json:"id"`
	AdminSystemName      string    `gorm:"size:100;default:'LXD API - 管理后台'" json:"admin_system_name"`
	AdminSystemTitle     string    `gorm:"size:100;default:'管理后台 - LXD容器管理系统'" json:"admin_system_title"`
	AdminLoginTitle      string    `gorm:"size:100;default:'管理员登录'" json:"admin_login_title"`
	AdminBgImage         string    `gorm:"size:500;default:''" json:"admin_bg_image"`
	AdminBgOpacity       int       `gorm:"default:75" json:"admin_bg_opacity"`
	AdminContentOpacity  int       `gorm:"default:85" json:"admin_content_opacity"`
	UserSystemName       string    `gorm:"size:100;default:'LXD API - 用户中心'" json:"user_system_name"`
	UserSystemTitle      string    `gorm:"size:100;default:'用户中心 - LXD容器管理系统'" json:"user_system_title"`
	UserLoginTitle       string    `gorm:"size:100;default:'用户登录'" json:"user_login_title"`
	UserBgImage          string    `gorm:"size:500;default:''" json:"user_bg_image"`
	UserBgOpacity        int       `gorm:"default:75" json:"user_bg_opacity"`
	UserContentOpacity   int       `gorm:"default:85" json:"user_content_opacity"`
	ContainerSystemName  string    `gorm:"size:100;default:'LXD API - 容器控制'" json:"container_system_name"`
	ContainerSystemTitle string    `gorm:"size:100;default:'容器管理 - LXD容器管理系统'" json:"container_system_title"`
	ContainerLoginTitle  string    `gorm:"size:100;default:'容器登录'" json:"container_login_title"`
	ContainerBgImage     string    `gorm:"size:500;default:''" json:"container_bg_image"`
	ContainerBgOpacity   int       `gorm:"default:75" json:"container_bg_opacity"`
	ContainerContentOpacity int    `gorm:"default:85" json:"container_content_opacity"`
	UserNotice           string    `gorm:"default:''" json:"user_notice"`
	UserNoticeOpacity    int       `gorm:"default:85" json:"user_notice_opacity"`
	ContainerNotice      string    `gorm:"default:''" json:"container_notice"`
	ContainerNoticeOpacity int     `gorm:"default:85" json:"container_notice_opacity"`
	FaviconUrl           string    `gorm:"size:500;default:''" json:"favicon_url"`
	FooterText           string    `gorm:"size:200;default:'LXD API 容器管理平台'" json:"footer_text"`
	UseLocalCDN          bool     `gorm:"default:false" json:"use_local_cdn"`
	TLSCertContent       string   `gorm:"type:text;default:''" json:"tls_cert_content"`
	TLSKeyContent        string   `gorm:"type:text;default:''" json:"tls_key_content"`
	UpdatedAt            time.Time `json:"updated_at"`
	CreatedAt            time.Time `json:"created_at"`
}

type PublicBrandSettings struct {
	ID                      int       `json:"id"`
	AdminSystemName         string    `json:"admin_system_name"`
	AdminSystemTitle        string    `json:"admin_system_title"`
	AdminLoginTitle         string    `json:"admin_login_title"`
	AdminBgImage            string    `json:"admin_bg_image"`
	AdminBgOpacity          int       `json:"admin_bg_opacity"`
	AdminContentOpacity     int       `json:"admin_content_opacity"`
	UserSystemName          string    `json:"user_system_name"`
	UserSystemTitle         string    `json:"user_system_title"`
	UserLoginTitle          string    `json:"user_login_title"`
	UserBgImage             string    `json:"user_bg_image"`
	UserBgOpacity           int       `json:"user_bg_opacity"`
	UserContentOpacity      int       `json:"user_content_opacity"`
	ContainerSystemName     string    `json:"container_system_name"`
	ContainerSystemTitle    string    `json:"container_system_title"`
	ContainerLoginTitle     string    `json:"container_login_title"`
	ContainerBgImage        string    `json:"container_bg_image"`
	ContainerBgOpacity      int       `json:"container_bg_opacity"`
	ContainerContentOpacity int       `json:"container_content_opacity"`
	UserNotice              string    `json:"user_notice"`
	UserNoticeOpacity       int       `json:"user_notice_opacity"`
	ContainerNotice         string    `json:"container_notice"`
	ContainerNoticeOpacity  int       `json:"container_notice_opacity"`
	FaviconUrl              string    `json:"favicon_url"`
	FooterText              string    `json:"footer_text"`
	UseLocalCDN             bool      `json:"use_local_cdn"`
	UpdatedAt               time.Time `json:"updated_at"`
	CreatedAt               time.Time `json:"created_at"`
}

func (b *BrandSettings) ToPublic() PublicBrandSettings {
	return PublicBrandSettings{
		ID:                      b.ID,
		AdminSystemName:         b.AdminSystemName,
		AdminSystemTitle:        b.AdminSystemTitle,
		AdminLoginTitle:         b.AdminLoginTitle,
		AdminBgImage:            b.AdminBgImage,
		AdminBgOpacity:          b.AdminBgOpacity,
		AdminContentOpacity:     b.AdminContentOpacity,
		UserSystemName:          b.UserSystemName,
		UserSystemTitle:         b.UserSystemTitle,
		UserLoginTitle:          b.UserLoginTitle,
		UserBgImage:             b.UserBgImage,
		UserBgOpacity:           b.UserBgOpacity,
		UserContentOpacity:      b.UserContentOpacity,
		ContainerSystemName:     b.ContainerSystemName,
		ContainerSystemTitle:    b.ContainerSystemTitle,
		ContainerLoginTitle:     b.ContainerLoginTitle,
		ContainerBgImage:        b.ContainerBgImage,
		ContainerBgOpacity:      b.ContainerBgOpacity,
		ContainerContentOpacity: b.ContainerContentOpacity,
		UserNotice:              b.UserNotice,
		UserNoticeOpacity:       b.UserNoticeOpacity,
		ContainerNotice:         b.ContainerNotice,
		ContainerNoticeOpacity:  b.ContainerNoticeOpacity,
		FaviconUrl:              b.FaviconUrl,
		FooterText:              b.FooterText,
		UseLocalCDN:             b.UseLocalCDN,
		UpdatedAt:               b.UpdatedAt,
		CreatedAt:               b.CreatedAt,
	}
}
