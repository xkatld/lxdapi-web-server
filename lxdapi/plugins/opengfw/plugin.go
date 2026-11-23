package opengfw

import (
	"fmt"
	"lxdapi/internal/db"
	"lxdapi/internal/middleware"
	"lxdapi/models"
	"lxdapi/pkg/logger"
	"lxdapi/pkg/plugin"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

var _ plugin.Plugin = (*OpenGFWPlugin)(nil)

type OpenGFWPlugin struct {
	workDir          string
	processManager   *ProcessManager
	nftablesManager  *NFTablesManager
	ruleGenerator    *RuleGeneratorV3
	config           *models.FirewallConfig
}

func NewOpenGFWPlugin() *OpenGFWPlugin {
	workDir := "plugins/opengfw"
	return &OpenGFWPlugin{
		workDir: workDir,
	}
}

func (p *OpenGFWPlugin) Name() string {
	return "opengfw"
}

func (p *OpenGFWPlugin) Version() string {
	return "0.4.1"
}

func (p *OpenGFWPlugin) Description() string {
	return "OpenGFW 防火墙插件 - 协议拦截和流量过滤"
}

func (p *OpenGFWPlugin) Init() error {
	logger.Info("初始化 OpenGFW 插件...")
	
	if _, err := os.Stat(p.workDir); os.IsNotExist(err) {
		return fmt.Errorf("插件目录不存在: %s", p.workDir)
	}
	
	binPath := filepath.Join(p.workDir, "bin")
	if _, err := os.Stat(binPath); os.IsNotExist(err) {
		return fmt.Errorf("二进制文件目录不存在: %s", binPath)
	}
	
	dataPath := filepath.Join(p.workDir, "data")
	if _, err := os.Stat(dataPath); os.IsNotExist(err) {
		return fmt.Errorf("数据文件目录不存在: %s", dataPath)
	}
	
	geoipPath := filepath.Join(dataPath, "geoip.dat")
	geositePath := filepath.Join(dataPath, "geosite.dat")
	
	if _, err := os.Stat(geoipPath); os.IsNotExist(err) {
		logger.Warn("GeoIP 数据文件不存在: %s（地理位置拦截将不可用）", geoipPath)
	}
	
	if _, err := os.Stat(geositePath); os.IsNotExist(err) {
		logger.Warn("GeoSite 数据文件不存在: %s（域名匹配将不可用）", geositePath)
	}
	
	if err := db.DB.AutoMigrate(&models.FirewallConfig{}); err != nil {
		return fmt.Errorf("数据库迁移失败: %v", err)
	}
	
	if err := p.loadConfig(); err != nil {
		return fmt.Errorf("加载配置失败: %v", err)
	}
	
	p.processManager = NewProcessManager(p.workDir)
	p.nftablesManager = NewNFTablesManager(100)
	p.ruleGenerator = NewRuleGeneratorV3(p.config)
	
	if err := p.nftablesManager.CheckNFQueueSupport(); err != nil {
		return fmt.Errorf("NFQueue 支持检查失败: %v", err)
	}
	
	logger.OK("OpenGFW 插件初始化完成")
	return nil
}

func (p *OpenGFWPlugin) Start() error {
	if !p.config.Enabled {
		logger.Info("OpenGFW 插件已禁用，跳过启动")
		return nil
	}
	
	logger.Info("启动 OpenGFW 插件...")
	
	if err := p.generateFiles(); err != nil {
		return fmt.Errorf("生成配置文件失败: %v", err)
	}
	
	if err := p.nftablesManager.Setup(); err != nil {
		return fmt.Errorf("设置 nftables 失败: %v", err)
	}
	
	if err := p.processManager.Start(); err != nil {
		p.nftablesManager.Remove()
		return fmt.Errorf("启动 OpenGFW 进程失败: %v", err)
	}
	
	logger.OK("OpenGFW 插件已启动")
	return nil
}

func (p *OpenGFWPlugin) Stop() error {
	logger.Info("停止 OpenGFW 插件...")
	
	if err := p.processManager.Stop(); err != nil {
		logger.Warn("停止 OpenGFW 进程失败: %v", err)
	}
	
	if err := p.nftablesManager.Remove(); err != nil {
		logger.Warn("移除 nftables 规则失败: %v", err)
	}
	
	logger.OK("OpenGFW 插件已停止")
	return nil
}

func (p *OpenGFWPlugin) RegisterRoutes(r *gin.Engine) {
	api := NewAPIHandlerV2(p)
	
	adminAPI := r.Group("/api/admin/firewall")
	adminAPI.Use(middleware.AdminAuth())
	{
		adminAPI.GET("/config", api.GetConfig)
		adminAPI.PUT("/config", api.UpdateConfig)
		adminAPI.POST("/apply", api.ApplyConfig)
		adminAPI.GET("/status", api.GetStatus)
		adminAPI.POST("/start", api.StartService)
		adminAPI.POST("/stop", api.StopService)
		adminAPI.POST("/restart", api.RestartService)
		adminAPI.GET("/logs", api.GetLogs)
		adminAPI.DELETE("/logs", api.ClearLogs)
	}
}

func (p *OpenGFWPlugin) RegisterHooks(h *plugin.HookManager) {
}

func (p *OpenGFWPlugin) loadConfig() error {
	var config models.FirewallConfig

	result := db.DB.First(&config)
	if result.Error != nil {
		config = models.FirewallConfig{
			Enabled: false,
		}

		if err := db.DB.Create(&config).Error; err != nil {
			return fmt.Errorf("创建默认配置失败: %v", err)
		}

		logger.Info("已创建默认防火墙配置")
	}

	p.config = &config
	return nil
}

func (p *OpenGFWPlugin) generateFiles() error {
	if err := p.loadConfig(); err != nil {
		return err
	}
	
	p.ruleGenerator = NewRuleGeneratorV3(p.config)
	
	rulesContent, err := p.ruleGenerator.Generate()
	if err != nil {
		return fmt.Errorf("生成规则失败: %v", err)
	}
	
	rulesPath := filepath.Join(p.workDir, "rules.yaml")
	if err := os.WriteFile(rulesPath, []byte(rulesContent), 0644); err != nil {
		return fmt.Errorf("写入规则文件失败: %v", err)
	}
	
	logger.Info("规则文件已生成: %s", rulesPath)

	dataDir := filepath.Join(p.workDir, "data")
	geoipPath := filepath.Join(dataDir, "geoip.dat")
	geositePath := filepath.Join(dataDir, "geosite.dat")
	
	absGeoipPath, _ := filepath.Abs(geoipPath)
	absGeositePath, _ := filepath.Abs(geositePath)
	geoipPath = absGeoipPath
	geositePath = absGeositePath
	
	configContent := fmt.Sprintf(`log:
  level: info

io:
  queueSize: 1024
  rcvBuf: 4194304
  sndBuf: 4194304
  workers: 4
  local: true

workers:
  count: 4
  queueSize: 1024
  tcpMaxBufferedPagesTotal: 4096
  tcpMaxBufferedPagesPerConn: 64
  tcpTimeout: 10m
  udpTimeout: 30s

ruleset:
  rules:
    path: %s
    format: yaml

geoip:
  geoip:
    path: %s
    format: maxminddb

geosite:
  geosite:
    path: %s
    format: v2ray
`,
		rulesPath,
		geoipPath,
		geositePath,
	)
	
	configPath := filepath.Join(p.workDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		return fmt.Errorf("写入配置文件失败: %v", err)
	}
	
	logger.Info("配置文件已生成: %s", configPath)
	return nil
}

func (p *OpenGFWPlugin) ApplyConfig() error {
	if err := p.generateFiles(); err != nil {
		return fmt.Errorf("生成配置文件失败: %v", err)
	}
	
	if p.processManager.IsRunning() {
		if err := p.processManager.Reload(); err != nil {
			return fmt.Errorf("热重载失败: %v", err)
		}
	}
	
	logger.OK("配置已应用")
	return nil
}
