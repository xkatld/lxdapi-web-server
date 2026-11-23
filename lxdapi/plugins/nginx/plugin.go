package nginx

import (
	"context"
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

var _ plugin.Plugin = (*NginxPlugin)(nil)

type NginxPlugin struct {
	workDir         string
	confDir         string
	sitesDir        string
	sslDir          string
	manager         *NginxManager
	configGenerator *ConfigGenerator
	config          *models.NginxConfig
}

func NewNginxPlugin() *NginxPlugin {
	workDir := "plugins/nginx"
	return &NginxPlugin{
		workDir:  workDir,
		confDir:  filepath.Join(workDir, "conf"),
		sitesDir: filepath.Join(workDir, "conf", "sites"),
		sslDir:   filepath.Join(workDir, "ssl"),
	}
}

func (p *NginxPlugin) Name() string {
	return "nginx"
}

func (p *NginxPlugin) Version() string {
	mgr := NewNginxManager()
	version, err := mgr.GetVersion()
	if err != nil {
		return "未安装"
	}
	return version
}

func (p *NginxPlugin) Description() string {
	return "Nginx反向代理插件 - HTTP/HTTPS反向代理服务"
}

func (p *NginxPlugin) Init() error {
	logger.Info("初始化 Nginx 插件...")
	
	for _, dir := range []string{p.workDir, p.confDir, p.sitesDir, p.sslDir} {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return fmt.Errorf("插件目录不存在: %s", dir)
		}
	}
	
	if err := db.DB.AutoMigrate(&models.NginxConfig{}, &models.ReverseProxy{}); err != nil {
		return fmt.Errorf("数据库迁移失败: %v", err)
	}
	
	if err := p.loadConfig(); err != nil {
		return fmt.Errorf("加载配置失败: %v", err)
	}
	
	p.manager = NewNginxManager()
	p.configGenerator = NewConfigGenerator(p.confDir, p.sitesDir, p.sslDir)
	if p.configGenerator == nil {
		return fmt.Errorf("初始化配置生成器失败")
	}
	
	if err := p.manager.CheckInstalled(); err != nil {
		logger.Warn("Nginx未安装或不可用: %v", err)
		logger.Info("请使用 'apt install nginx' 安装Nginx")
	} else {
		if err := p.ensureNginxInclude(); err != nil {
			logger.Warn("设置Nginx include失败: %v", err)
		}
	}
	
	logger.OK("Nginx 插件初始化完成")
	return nil
}

func (p *NginxPlugin) Start() error {
	if !p.config.Enabled {
		logger.Info("Nginx 插件已禁用，跳过启动")
		return nil
	}
	
	logger.Info("启动 Nginx 插件...")
	
	if err := p.generateConfigs(); err != nil {
		return fmt.Errorf("生成配置文件失败: %v", err)
	}
	
	if err := p.manager.TestConfig(); err != nil {
		return fmt.Errorf("配置文件测试失败: %v", err)
	}
	
	if p.manager.IsRunning() {
		if err := p.manager.Reload(); err != nil {
			return fmt.Errorf("重载Nginx失败: %v", err)
		}
		logger.OK("Nginx 插件已启动")
	} else {
		logger.Info("Nginx 未运行，保持当前状态")
	}
	return nil
}

func (p *NginxPlugin) Stop() error {
	logger.Info("停止 Nginx 插件...")
	
	if err := p.cleanupSiteConfigs(); err != nil {
		logger.Warn("清理站点配置失败: %v", err)
	}
	
	if p.manager.IsRunning() {
		if err := p.manager.Stop(); err != nil {
			logger.Warn("停止Nginx服务失败: %v", err)
		}
	}
	
	logger.OK("Nginx 插件已停止")
	return nil
}

func (p *NginxPlugin) RegisterRoutes(r *gin.Engine) {
	api := NewAPIHandler(p)
	
	adminAPI := r.Group("/api/admin/nginx")
	adminAPI.Use(middleware.AdminAuth())
	{
		adminAPI.GET("/config", api.GetConfig)
		adminAPI.PUT("/config", api.UpdateConfig)
		adminAPI.POST("/apply", api.ApplyConfig)
		adminAPI.GET("/status", api.GetStatus)
		adminAPI.POST("/start", api.StartNginx)
		adminAPI.POST("/stop", api.StopNginx)
		adminAPI.POST("/restart", api.RestartNginx)
		adminAPI.POST("/reload", api.ReloadNginx)
		adminAPI.GET("/restrictions", api.GetRestrictions)
		adminAPI.PUT("/restrictions", api.UpdateRestrictions)
		
		adminAPI.GET("/proxies", api.GetProxies)
		adminAPI.POST("/proxies", api.CreateProxy)
		adminAPI.PUT("/proxies/:id", api.UpdateProxy)
		adminAPI.DELETE("/proxies/:id", api.DeleteProxy)
		adminAPI.POST("/proxies/batch-delete", api.BatchDeleteProxies)
		adminAPI.GET("/proxies/container/:name", api.GetContainerProxies)
	}
	
	userAPI := r.Group("/api/user/nginx")
	userAPI.Use(middleware.UserAuthOrBasic())
	{
		userAPI.GET("/proxies", api.GetUserProxies)
		userAPI.POST("/proxies", api.CreateUserProxy)
		userAPI.PUT("/proxies/:id", api.UpdateUserProxy)
		userAPI.DELETE("/proxies/:id", api.DeleteUserProxy)
	}
	
	containerAPI := r.Group("/api/container/nginx")
	containerAPI.Use(middleware.ContainerAuth())
	{
		containerAPI.GET("/proxies", api.GetContainerProxiesAPI)
		containerAPI.POST("/proxies", api.CreateContainerProxy)
		containerAPI.PUT("/proxies/:id", api.UpdateContainerProxy)
		containerAPI.DELETE("/proxies/:id", api.DeleteContainerProxy)
	}
}

func (p *NginxPlugin) RegisterHooks(h *plugin.HookManager) {
	h.Register(plugin.HookAfterContainerDelete, func(ctx context.Context, data map[string]interface{}) error {
		containerName, ok := data["container_name"].(string)
		if !ok {
			return nil
		}
		
		if err := db.DB.Unscoped().Where("container_name = ?", containerName).Delete(&models.ReverseProxy{}).Error; err != nil {
			logger.Warn("删除容器反向代理规则失败: %v", err)
		} else {
			logger.Info("已删除容器 %s 的反向代理规则", containerName)
		}
		
		if p.config.Enabled {
			if err := p.generateConfigs(); err != nil {
				logger.Warn("重新生成Nginx配置失败: %v", err)
			} else {
				p.manager.Reload()
			}
		}
		
		return nil
	})
}

func (p *NginxPlugin) loadConfig() error {
	var config models.NginxConfig
	
	result := db.DB.First(&config)
	if result.Error != nil {
		config = models.NginxConfig{
			Enabled: true,
		}

		if err := db.DB.Create(&config).Error; err != nil {
			return fmt.Errorf("创建默认配置失败: %v", err)
		}

		logger.Info("已创建默认Nginx配置")
	}
	
	p.config = &config
	return nil
}

func (p *NginxPlugin) generateConfigs() error {
	if err := p.loadConfig(); err != nil {
		return err
	}
	
	var proxies []models.ReverseProxy
	if err := db.DB.Where("status = ?", "active").Find(&proxies).Error; err != nil {
		return fmt.Errorf("查询反向代理规则失败: %v", err)
	}
	
	if err := p.configGenerator.Generate(p.config, proxies); err != nil {
		return err
	}
	
	logger.Info("Nginx配置文件已生成，规则数: %d", len(proxies))
	return nil
}

func (p *NginxPlugin) cleanupSiteConfigs() error {
	files, err := filepath.Glob(filepath.Join(p.sitesDir, "*.conf"))
	if err != nil {
		return err
	}
	
	for _, file := range files {
		if err := os.Remove(file); err != nil {
			logger.Warn("删除配置文件失败 %s: %v", file, err)
		}
	}
	
	return nil
}

func (p *NginxPlugin) ApplyConfig() error {
	if err := p.generateConfigs(); err != nil {
		return fmt.Errorf("生成配置文件失败: %v", err)
	}
	
	if err := p.manager.TestConfig(); err != nil {
		return fmt.Errorf("配置文件测试失败: %v", err)
	}
	
	if !p.manager.IsRunning() {
		if err := p.manager.Start(); err != nil {
			return fmt.Errorf("启动Nginx服务失败: %v", err)
		}
	} else {
		if err := p.manager.Reload(); err != nil {
			return fmt.Errorf("重载Nginx失败: %v", err)
		}
	}
	
	logger.OK("配置已应用")
	return nil
}

func (p *NginxPlugin) ensureNginxInclude() error {
	includeFile := "/etc/nginx/conf.d/lxdapi.conf"
	
	sitesDir, err := filepath.Abs(p.sitesDir)
	if err != nil {
		return err
	}
	
	includeContent := fmt.Sprintf("include %s/*.conf;\n", sitesDir)
	
	if data, err := os.ReadFile(includeFile); err == nil {
		if string(data) == includeContent {
			return nil
		}
	}
	
	if err := os.WriteFile(includeFile, []byte(includeContent), 0644); err != nil {
		return fmt.Errorf("写入include配置失败: %v", err)
	}
	
	logger.Info("已创建Nginx include配置: %s", includeFile)
	return nil
}
