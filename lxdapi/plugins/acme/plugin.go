package acme

import (
	"fmt"
	"lxdapi/internal/db"
	"lxdapi/internal/middleware"
	"lxdapi/models"
	"lxdapi/pkg/logger"
	"lxdapi/pkg/plugin"
	"net"
	"net/mail"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
)

const RENEW_INTERVAL = 12 * time.Hour

var _ plugin.Plugin = (*AcmePlugin)(nil)

type AcmePlugin struct {
	certDir        string
	manager        *Manager
	mu             sync.Mutex
	stopCh         chan struct{}
	running        bool
	reloadCallback func(certPath, keyPath string) error
}

func NewAcmePlugin(certDir string) *AcmePlugin {
	return &AcmePlugin{
		certDir: certDir,
	}
}

func has_space_or_control(value string) bool {
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func validate_server_ip(server_ip string) error {
	trimmed := strings.TrimSpace(server_ip)
	if trimmed == "" {
		return nil
	}
	if strings.HasPrefix(trimmed, "-") || has_space_or_control(trimmed) || strings.Contains(trimmed, "..") || strings.ContainsAny(trimmed, "/\\") {
		return fmt.Errorf("服务器 IP 或域名包含非法字符")
	}
	if net.ParseIP(trimmed) != nil {
		return nil
	}
	if len(trimmed) > 253 {
		return fmt.Errorf("服务器域名长度不能超过253字符")
	}
	labels := strings.Split(trimmed, ".")
	for _, label := range labels {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return fmt.Errorf("服务器 IP 或域名格式不正确")
		}
	}
	return nil
}

func validate_email(email string) error {
	trimmed := strings.TrimSpace(email)
	if trimmed == "" {
		return nil
	}
	if strings.HasPrefix(trimmed, "-") || has_space_or_control(trimmed) || strings.Contains(trimmed, "..") {
		return fmt.Errorf("邮箱包含非法字符")
	}
	addr, err := mail.ParseAddress(trimmed)
	if err != nil || addr.Address != trimmed {
		return fmt.Errorf("邮箱格式不正确")
	}
	return nil
}

func validate_webroot(webroot string) error {
	trimmed := strings.TrimSpace(webroot)
	if trimmed == "" {
		return nil
	}
	if strings.HasPrefix(trimmed, "-") || has_space_or_control(trimmed) || strings.Contains(trimmed, "..") {
		return fmt.Errorf("webroot 路径包含非法字符")
	}
	if !filepath.IsAbs(trimmed) {
		return fmt.Errorf("webroot 必须为绝对路径")
	}
	return nil
}

func validate_acme_params(server_ip, email, webroot string) error {
	if err := validate_server_ip(server_ip); err != nil {
		return err
	}
	if err := validate_email(email); err != nil {
		return err
	}
	if err := validate_webroot(webroot); err != nil {
		return err
	}
	return nil
}

func (p *AcmePlugin) Name() string {
	return "acme"
}

func (p *AcmePlugin) Version() string {
	return "1.0.0"
}

func (p *AcmePlugin) Description() string {
	return "ACME 自动证书签发与续签插件"
}

func (p *AcmePlugin) SetReloadCallback(fn func(certPath, keyPath string) error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reloadCallback = fn
}

func (p *AcmePlugin) GetCertPaths() (string, string) {
	if p.manager == nil {
		return "", ""
	}
	return p.manager.GetCertPaths()
}

func (p *AcmePlugin) Init() error {
	logger.Info("初始化 ACME 插件...")
	if err := db.DB.AutoMigrate(&models.AcmeConfig{}); err != nil {
		return fmt.Errorf("数据库迁移失败: %w", err)
	}
	mgr, err := NewManager(p.certDir)
	if err != nil {
		return err
	}
	p.manager = mgr

	cfg, err := p.ensure_config()
	if err != nil {
		return fmt.Errorf("初始化 ACME 配置失败: %w", err)
	}
	cert_path, key_path := p.manager.GetCertPaths()
	if cfg.CertPath != cert_path || cfg.KeyPath != key_path {
		cfg.CertPath = cert_path
		cfg.KeyPath = key_path
		_ = db.DB.Save(cfg).Error
	}

	logger.OK("ACME 插件初始化完成")
	return nil
}

func (p *AcmePlugin) Start() error {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return nil
	}
	p.stopCh = make(chan struct{})
	p.running = true
	p.mu.Unlock()

	go p.renew_loop()
	logger.OK("ACME 自动续签巡检任务已启动")
	return nil
}

func (p *AcmePlugin) Stop() error {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return nil
	}
	close(p.stopCh)
	p.running = false
	p.mu.Unlock()
	logger.OK("ACME 插件已停止")
	return nil
}

func (p *AcmePlugin) RegisterRoutes(r *gin.Engine) {
	api := NewAPIHandler(p)
	admin_api := r.Group("/api/admin/acme")
	admin_api.Use(middleware.AdminAuth())
	{
		admin_api.GET("/config", api.GetConfig)
		admin_api.PUT("/config", api.UpdateConfig)
		admin_api.GET("/status", api.GetStatus)
		admin_api.POST("/issue", api.IssueCert)
		admin_api.POST("/renew", api.RenewCert)
	}
}

func (p *AcmePlugin) RegisterHooks(h *plugin.HookManager) {
}

func (p *AcmePlugin) ensure_config() (*models.AcmeConfig, error) {
	var cfg models.AcmeConfig
	if err := db.DB.First(&cfg).Error; err != nil {
		cert_path, key_path := "", ""
		if p.manager != nil {
			cert_path, key_path = p.manager.GetCertPaths()
		}
		cfg = models.AcmeConfig{
			Enabled:  false,
			Webroot:  DEFAULT_WEBROOT,
			CertPath: cert_path,
			KeyPath:  key_path,
		}
		if create_err := db.DB.Create(&cfg).Error; create_err != nil {
			return nil, create_err
		}
	}
	if strings.TrimSpace(cfg.Webroot) == "" {
		cfg.Webroot = DEFAULT_WEBROOT
	}
	return &cfg, nil
}

func (p *AcmePlugin) save_error(err_msg string) {
	cfg, err := p.ensure_config()
	if err != nil {
		return
	}
	cfg.LastError = err_msg
	_ = db.DB.Save(cfg).Error
}

func (p *AcmePlugin) trigger_reload(cert_path, key_path string) error {
	if p.reloadCallback == nil {
		return nil
	}
	return p.reloadCallback(cert_path, key_path)
}

func (p *AcmePlugin) execute_and_save(server_ip, email, webroot string, is_issue bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.manager == nil {
		return fmt.Errorf("ACME 管理器未初始化")
	}
	if err := validate_acme_params(server_ip, email, webroot); err != nil {
		p.save_error(err.Error())
		return err
	}

	cfg, err := p.ensure_config()
	if err != nil {
		return err
	}

	target_ip := strings.TrimSpace(server_ip)
	if target_ip == "" {
		target_ip = strings.TrimSpace(cfg.ServerIP)
	}
	if target_ip == "" {
		detected_ip, ip_err := p.manager.GetPublicIP()
		if ip_err != nil {
			p.save_error(ip_err.Error())
			return ip_err
		}
		target_ip = detected_ip
	}

	target_email := strings.TrimSpace(email)
	if target_email == "" {
		target_email = strings.TrimSpace(cfg.Email)
	}

	target_webroot := strings.TrimSpace(webroot)
	if target_webroot == "" {
		target_webroot = strings.TrimSpace(cfg.Webroot)
	}
	if target_webroot == "" {
		target_webroot = DEFAULT_WEBROOT
	}

	if err := validate_acme_params(target_ip, target_email, target_webroot); err != nil {
		p.save_error(err.Error())
		return err
	}

	if is_issue {
		if !p.manager.IsInstalled() {
			if install_err := p.manager.Install(target_email); install_err != nil {
				p.save_error(install_err.Error())
				return install_err
			}
		}
		if issue_err := p.manager.Issue(target_ip, target_email, target_webroot); issue_err != nil {
			p.save_error(issue_err.Error())
			return issue_err
		}
		cfg.Enabled = true
	} else {
		if renew_err := p.manager.Renew(target_ip, target_email, target_webroot); renew_err != nil {
			p.save_error(renew_err.Error())
			return renew_err
		}
	}

	cert_path, key_path := p.manager.GetCertPaths()
	now := time.Now()
	cfg.ServerIP = target_ip
	cfg.Email = target_email
	cfg.Webroot = target_webroot
	cfg.CertPath = cert_path
	cfg.KeyPath = key_path
	cfg.LastRenewed = &now
	cfg.LastError = ""
	if expiry, exp_err := p.manager.GetCertExpiry(); exp_err == nil {
		cfg.ExpiresAt = &expiry
	}

	if save_err := db.DB.Save(cfg).Error; save_err != nil {
		return save_err
	}

	if reload_err := p.trigger_reload(cert_path, key_path); reload_err != nil {
		p.save_error(reload_err.Error())
		return reload_err
	}

	return nil
}

func (p *AcmePlugin) IssueAndSave(serverIP, email, webroot string) error {
	return p.execute_and_save(serverIP, email, webroot, true)
}

func (p *AcmePlugin) RenewAndSave(serverIP, email, webroot string) error {
	return p.execute_and_save(serverIP, email, webroot, false)
}

func (p *AcmePlugin) AutoIssueOnStartup() error {
	if p.manager == nil {
		return fmt.Errorf("ACME 管理器未初始化")
	}
	cfg, err := p.ensure_config()
	if err != nil {
		return err
	}

	if !p.manager.CertExists() {
		if !cfg.Enabled && cfg.LastRenewed != nil {
			return fmt.Errorf("ACME 已在后台禁用")
		}
		return p.IssueAndSave(cfg.ServerIP, cfg.Email, cfg.Webroot)
	}

	if !cfg.Enabled {
		return fmt.Errorf("ACME 已在后台禁用")
	}

	if p.manager.NeedsRenewal() {
		if renew_err := p.RenewAndSave(cfg.ServerIP, cfg.Email, cfg.Webroot); renew_err != nil {
			logger.Warn("启动时 ACME 证书续签失败，尝试继续使用现有证书: %v", renew_err)
		}
	}

	cert_path, key_path := p.manager.GetCertPaths()
	p.mu.Lock()
	reload_err := p.trigger_reload(cert_path, key_path)
	p.mu.Unlock()
	return reload_err
}

func (p *AcmePlugin) renew_loop() {
	ticker := time.NewTicker(RENEW_INTERVAL)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			cfg, err := p.ensure_config()
			if err != nil {
				logger.Error("ACME 巡检读取配置失败: %v", err)
				continue
			}
			if !cfg.Enabled || p.manager == nil {
				continue
			}
			if !p.manager.NeedsRenewal() {
				continue
			}
			logger.Info("检测到 ACME 证书即将过期或缺失，开始自动续签...")
			if !p.manager.CertExists() {
				if err := p.IssueAndSave(cfg.ServerIP, cfg.Email, cfg.Webroot); err != nil {
					logger.Error("ACME 自动签发失败: %v", err)
				} else {
					logger.OK("ACME 自动签发成功并已热加载")
				}
			} else {
				if err := p.RenewAndSave(cfg.ServerIP, cfg.Email, cfg.Webroot); err != nil {
					logger.Error("ACME 自动续签失败: %v", err)
				} else {
					logger.OK("ACME 自动续签成功并已热加载")
				}
			}
		}
	}
}
