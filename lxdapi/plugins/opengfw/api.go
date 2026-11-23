package opengfw

import (
	"fmt"
	"lxdapi/internal/db"
	"lxdapi/models"
	"lxdapi/pkg/logger"
	"lxdapi/pkg/response"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type APIHandlerV2 struct {
	plugin *OpenGFWPlugin
}

func NewAPIHandlerV2(plugin *OpenGFWPlugin) *APIHandlerV2 {
	return &APIHandlerV2{plugin: plugin}
}

func (h *APIHandlerV2) GetConfig(c *gin.Context) {
	var config models.FirewallConfig
	if err := db.DB.First(&config).Error; err != nil {
		logger.Error("获取防火墙配置失败: %v", err)
		response.Error(c, 500, "获取配置失败")
		return
	}
	response.Success(c, config)
}

func (h *APIHandlerV2) UpdateConfig(c *gin.Context) {
	var req struct {
		Rules string `json:"rules"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}

	var config models.FirewallConfig
	if err := db.DB.First(&config).Error; err != nil {
		response.Error(c, 404, "配置不存在")
		return
	}

	config.Rules = req.Rules

	if err := db.DB.Save(&config).Error; err != nil {
		logger.Error("保存防火墙配置失败: %v", err)
		response.Error(c, 500, "保存配置失败")
		return
	}

	logger.Info("防火墙配置已更新")
	response.Success(c, gin.H{"message": "配置已保存，请点击应用配置使其生效"})
}

func (h *APIHandlerV2) ApplyConfig(c *gin.Context) {
	if err := h.plugin.ApplyConfig(); err != nil {
		logger.Error("应用配置失败: %v", err)
		response.Error(c, 500, "应用配置失败")
		return
	}
	
	now := time.Now()
	db.DB.Model(&models.FirewallConfig{}).Where("id = ?", 1).Update("last_applied", &now)
	
	logger.OK("防火墙配置已应用")
	response.Success(c, gin.H{"message": "配置已应用，规则已生效"})
}

func (h *APIHandlerV2) GetStatus(c *gin.Context) {
	stats, err := h.plugin.processManager.GetStatus()
	if err != nil {
		logger.Error("获取防火墙状态失败: %v", err)
		response.Error(c, 500, "获取状态失败")
		return
	}

	stats.Version = h.plugin.Version()
	stats.ActiveRules = h.countActiveRules()
	stats.BlockedToday = h.countBlockedToday()

	response.Success(c, stats)
}

func (h *APIHandlerV2) StartService(c *gin.Context) {
	db.DB.Model(&models.FirewallConfig{}).Where("id = ?", 1).Update("enabled", true)
	
	if err := h.plugin.loadConfig(); err != nil {
		logger.Error("加载防火墙配置失败: %v", err)
		response.Error(c, 500, "加载配置失败")
		return
	}
	
	if err := h.plugin.Start(); err != nil {
		logger.Error("启动防火墙服务失败: %v", err)
		response.Error(c, 500, "启动失败")
		return
	}
	
	logger.OK("防火墙服务已启动")
	response.Success(c, gin.H{"message": "服务已启动"})
}

func (h *APIHandlerV2) StopService(c *gin.Context) {
	if err := h.plugin.Stop(); err != nil {
		logger.Error("停止防火墙服务失败: %v", err)
		response.Error(c, 500, "停止失败")
		return
	}
	
	db.DB.Model(&models.FirewallConfig{}).Where("id = ?", 1).Update("enabled", false)
	
	logger.OK("防火墙服务已停止")
	response.Success(c, gin.H{"message": "服务已停止"})
}

func (h *APIHandlerV2) RestartService(c *gin.Context) {
	if err := h.plugin.processManager.Restart(); err != nil {
		logger.Error("重启防火墙服务失败: %v", err)
		response.Error(c, 500, "重启失败")
		return
	}
	
	logger.OK("防火墙服务已重启")
	response.Success(c, gin.H{"message": "服务已重启"})
}

func (h *APIHandlerV2) GetLogs(c *gin.Context) {
	lines := 100
	if l := c.Query("lines"); l != "" {
		var err error
		if _, err = fmt.Sscanf(l, "%d", &lines); err != nil {
			lines = 100
		}
	}
	
	logs, err := h.plugin.processManager.GetLogs(lines)
	if err != nil {
		logger.Error("获取防火墙日志失败: %v", err)
		response.Error(c, 500, "获取日志失败")
		return
	}
	
	response.Success(c, gin.H{"logs": logs})
}

func (h *APIHandlerV2) ClearLogs(c *gin.Context) {
	if err := h.plugin.processManager.ClearLogs(); err != nil {
		logger.Error("清空防火墙日志失败: %v", err)
		response.Error(c, 500, "清空日志失败")
		return
	}
	
	logger.Info("防火墙日志已清空")
	response.Success(c, gin.H{"message": "日志已清空"})
}

func (h *APIHandlerV2) countActiveRules() int {
	var config models.FirewallConfig
	if err := db.DB.First(&config).Error; err != nil {
		return 0
	}
	return strings.Count(config.Rules, "- name:")
}

func (h *APIHandlerV2) countBlockedToday() int64 {
	return 0
}
