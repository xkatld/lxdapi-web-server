package admin

import (
	"github.com/gin-gonic/gin"
	"lxdapi/internal/db"
	"lxdapi/models"
	"lxdapi/pkg/logger"
	"lxdapi/pkg/response"
)

func GetPortRangeConfig(c *gin.Context) {
	var config models.PortRangeConfig
	
	if err := db.DB.First(&config).Error; err != nil {
		config = models.PortRangeConfig{
			V4PortStart:      10000,
			V4PortEnd:        65535,
			V6PortStart:      10000,
			V6PortEnd:        65535,
			V4AutoAllocate22: false,
			V6AutoAllocate22: false,
		}
		db.DB.Create(&config)
	}
	
	response.Success(c, config)
}

func SavePortRangeConfig(c *gin.Context) {
	var req struct {
		V4PortStart      int  `json:"v4_port_start" binding:"required"`
		V4PortEnd        int  `json:"v4_port_end" binding:"required"`
		V6PortStart      int  `json:"v6_port_start" binding:"required"`
		V6PortEnd        int  `json:"v6_port_end" binding:"required"`
		V4AutoAllocate22 bool `json:"v4_auto_allocate_22"`
		V6AutoAllocate22 bool `json:"v6_auto_allocate_22"`
	}
	
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}
	
	if req.V4PortStart < 1 || req.V4PortStart > 65535 || req.V4PortEnd < 1 || req.V4PortEnd > 65535 {
		response.Error(c, 400, "IPv4端口范围必须在 1-65535 之间")
		return
	}
	
	if req.V4PortStart >= req.V4PortEnd {
		response.Error(c, 400, "IPv4起始端口必须小于结束端口")
		return
	}
	
	if req.V6PortStart < 1 || req.V6PortStart > 65535 || req.V6PortEnd < 1 || req.V6PortEnd > 65535 {
		response.Error(c, 400, "IPv6端口范围必须在 1-65535 之间")
		return
	}
	
	if req.V6PortStart >= req.V6PortEnd {
		response.Error(c, 400, "IPv6起始端口必须小于结束端口")
		return
	}
	
	var config models.PortRangeConfig
	if err := db.DB.First(&config).Error; err != nil {
		config = models.PortRangeConfig{
			V4PortStart:      req.V4PortStart,
			V4PortEnd:        req.V4PortEnd,
			V6PortStart:      req.V6PortStart,
			V6PortEnd:        req.V6PortEnd,
			V4AutoAllocate22: req.V4AutoAllocate22,
			V6AutoAllocate22: req.V6AutoAllocate22,
		}
		if err := db.DB.Create(&config).Error; err != nil {
			logger.Error("创建端口范围配置失败: %v", err)
			response.Error(c, 500, "保存失败")
			return
		}
	} else {
		config.V4PortStart = req.V4PortStart
		config.V4PortEnd = req.V4PortEnd
		config.V6PortStart = req.V6PortStart
		config.V6PortEnd = req.V6PortEnd
		config.V4AutoAllocate22 = req.V4AutoAllocate22
		config.V6AutoAllocate22 = req.V6AutoAllocate22
		if err := db.DB.Save(&config).Error; err != nil {
			logger.Error("保存端口范围配置失败: %v", err)
			response.Error(c, 500, "保存失败")
			return
		}
	}
	
	response.Success(c, config)
}
