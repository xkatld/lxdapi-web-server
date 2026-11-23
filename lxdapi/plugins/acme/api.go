package acme

import (
	"errors"
	"io"
	"lxdapi/internal/core"
	"lxdapi/internal/db"
	"lxdapi/models"
	"lxdapi/pkg/response"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type APIHandler struct {
	plugin *AcmePlugin
}

func NewAPIHandler(p *AcmePlugin) *APIHandler {
	return &APIHandler{
		plugin: p,
	}
}

func (h *APIHandler) GetConfig(c *gin.Context) {
	cfg, err := h.plugin.ensure_config()
	if err != nil {
		response.Error(c, 500, "获取 ACME 配置失败")
		return
	}
	response.Success(c, cfg)
}

func (h *APIHandler) UpdateConfig(c *gin.Context) {
	var req struct {
		Enabled  *bool  `json:"enabled"`
		ServerIP string `json:"server_ip"`
		Email    string `json:"email"`
		Webroot  string `json:"webroot"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}

	server_ip := strings.TrimSpace(req.ServerIP)
	email := strings.TrimSpace(req.Email)
	webroot := strings.TrimSpace(req.Webroot)

	if err := validate_acme_params(server_ip, email, webroot); err != nil {
		response.Error(c, 400, err.Error())
		return
	}
	if webroot == "" {
		webroot = DEFAULT_WEBROOT
	}

	cfg, err := h.plugin.ensure_config()
	if err != nil {
		response.Error(c, 500, "获取 ACME 配置失败")
		return
	}

	if req.Enabled != nil {
		cfg.Enabled = *req.Enabled
	}
	cfg.ServerIP = server_ip
	cfg.Email = email
	cfg.Webroot = webroot
	cfg.LastError = ""

	if err := db.DB.Save(cfg).Error; err != nil {
		response.Error(c, 500, "保存 ACME 配置失败")
		return
	}
	response.Success(c, cfg)
}

func (h *APIHandler) GetStatus(c *gin.Context) {
	cfg, err := h.plugin.ensure_config()
	if err != nil {
		response.Error(c, 500, "获取 ACME 状态失败")
		return
	}

	plugin_enabled := false
	if core.GlobalConfig != nil {
		plugin_enabled = core.GlobalConfig.Plugins.Acme.Enabled
	}

	installed := false
	cert_exists := false
	needs_renewal := true
	cert_path := cfg.CertPath
	key_path := cfg.KeyPath
	var expires_at *time.Time = cfg.ExpiresAt

	if h.plugin.manager != nil {
		installed = h.plugin.manager.IsInstalled()
		cert_exists = h.plugin.manager.CertExists()
		needs_renewal = h.plugin.manager.NeedsRenewal()
		p_cert, p_key := h.plugin.manager.GetCertPaths()
		if p_cert != "" {
			cert_path = p_cert
		}
		if p_key != "" {
			key_path = p_key
		}
		if cert_exists {
			if exp, exp_err := h.plugin.manager.GetCertExpiry(); exp_err == nil {
				expires_at = &exp
			}
		}
	}

	response.Success(c, gin.H{
		"plugin_enabled": plugin_enabled,
		"installed":      installed,
		"cert_exists":    cert_exists,
		"needs_renewal":  needs_renewal,
		"cert_path":      cert_path,
		"key_path":       key_path,
		"expires_at":     expires_at,
		"last_renewed":   cfg.LastRenewed,
		"last_error":     cfg.LastError,
		"server_ip":      cfg.ServerIP,
		"email":          cfg.Email,
		"webroot":        cfg.Webroot,
	})
}

func (h *APIHandler) handle_cert_action(c *gin.Context, action func(string, string, string) error) {
	var req struct {
		ServerIP string `json:"server_ip"`
		Email    string `json:"email"`
		Webroot  string `json:"webroot"`
	}
	if c.Request != nil && c.Request.Body != nil && c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
			response.Error(c, 400, "请求参数格式错误")
			return
		}
	}

	server_ip := strings.TrimSpace(req.ServerIP)
	email := strings.TrimSpace(req.Email)
	webroot := strings.TrimSpace(req.Webroot)

	if err := validate_acme_params(server_ip, email, webroot); err != nil {
		response.Error(c, 400, err.Error())
		return
	}

	if err := action(server_ip, email, webroot); err != nil {
		response.Error(c, 500, err.Error())
		return
	}

	var cfg models.AcmeConfig
	_ = db.DB.First(&cfg).Error
	response.Success(c, cfg)
}

func (h *APIHandler) IssueCert(c *gin.Context) {
	h.handle_cert_action(c, h.plugin.IssueAndSave)
}

func (h *APIHandler) RenewCert(c *gin.Context) {
	h.handle_cert_action(c, h.plugin.RenewAndSave)
}
