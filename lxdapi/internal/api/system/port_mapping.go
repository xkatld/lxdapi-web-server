package system

import (
	"context"
	"github.com/gin-gonic/gin"
	"lxdapi/internal/db"
	"lxdapi/internal/service"
	"lxdapi/models"
	"lxdapi/pkg/logger"
	"lxdapi/pkg/response"
)

// AllocatePortMapping 分配端口映射（统一接口）
func AllocatePortMapping(c *gin.Context) {
	version := c.Query("version")
	if version != "v4" && version != "v6" {
		response.Error(c, 400, "version参数必须是v4或v6")
		return
	}

	var req struct {
		ContainerName string `json:"container_name" binding:"required"`
		Interface     string `json:"interface" binding:"required"`
		PublicIP      string `json:"public_ip" binding:"required"`
		PublicPort    int    `json:"public_port" binding:"required"`
		ContainerPort int    `json:"container_port" binding:"required"`
		Protocol      string `json:"protocol" binding:"required"`
		Description   string `json:"description"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误: "+err.Error())
		return
	}

	if err := service.ValidatePortMappingProtocol(req.Protocol); err != nil {
		response.Error(c, 400, err.Error())
		return
	}
	if _, err := service.ValidatePortRange(req.PublicPort, 1, "公网端口"); err != nil {
		response.Error(c, 400, err.Error())
		return
	}
	if _, err := service.ValidatePortRange(req.ContainerPort, 1, "容器端口"); err != nil {
		response.Error(c, 400, err.Error())
		return
	}

	var container models.Container
	if err := db.DB.Where("name = ?", req.ContainerName).First(&container).Error; err != nil {
		response.Error(c, 404, "容器不存在")
		return
	}

	ctx := context.Background()
	pmService := service.NewPortMappingService()

	if version == "v4" {
		mapping, err := pmService.AllocateV4Mapping(ctx, req.ContainerName, container.UserID, req.PublicIP, req.PublicIP, req.PublicPort, req.PublicPort, req.ContainerPort, req.ContainerPort, req.Protocol, req.Interface, req.Description)
		if err != nil {
			logger.Error("分配IPv4端口映射失败: %v", err)
			response.Error(c, 500, err.Error())
			return
		}
		logger.OK("系统API为容器 %s 分配IPv4端口映射: %s:%d -> %d", req.ContainerName, req.PublicIP, req.PublicPort, req.ContainerPort)
		response.Success(c, gin.H{"mapping": mapping})
	} else {
		mapping, err := pmService.AllocateV6Mapping(ctx, req.ContainerName, container.UserID, req.PublicIP, req.PublicIP, req.PublicPort, req.PublicPort, req.ContainerPort, req.ContainerPort, req.Protocol, req.Interface, req.Description)
		if err != nil {
			logger.Error("分配IPv6端口映射失败: %v", err)
			response.Error(c, 500, err.Error())
			return
		}
		logger.OK("系统API为容器 %s 分配IPv6端口映射: %s:%d -> %d", req.ContainerName, req.PublicIP, req.PublicPort, req.ContainerPort)
		response.Success(c, gin.H{"mapping": mapping})
	}
}

// ReleasePortMapping 释放端口映射（统一接口）
func ReleasePortMapping(c *gin.Context) {
	version := c.Query("version")
	if version != "v4" && version != "v6" {
		response.Error(c, 400, "version参数必须是v4或v6")
		return
	}

	var req struct {
		ID uint `json:"id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误: "+err.Error())
		return
	}

	pmService := service.NewPortMappingService()

	if version == "v4" {
		if err := pmService.ReleaseV4Mapping(req.ID); err != nil {
			logger.Error("释放IPv4端口映射失败: %v", err)
			response.Error(c, 500, err.Error())
			return
		}
		logger.OK("系统API释放IPv4端口映射成功: ID=%d", req.ID)
		response.Success(c, "IPv4端口映射释放成功")
	} else {
		if err := pmService.ReleaseV6Mapping(req.ID); err != nil {
			logger.Error("释放IPv6端口映射失败: %v", err)
			response.Error(c, 500, err.Error())
			return
		}
		logger.OK("系统API释放IPv6端口映射成功: ID=%d", req.ID)
		response.Success(c, "IPv6端口映射释放成功")
	}
}

// ListPortMappings 获取端口映射列表（统一接口）
func ListPortMappings(c *gin.Context) {
	version := c.DefaultQuery("version", "all")
	containerName := c.Query("container")

	pmService := service.NewPortMappingService()

	result := gin.H{}

	if version == "v4" || version == "all" {
		mappingsV4, err := pmService.ListV4Mappings(containerName)
		if err != nil {
			logger.Error("获取IPv4端口映射列表失败: %v", err)
			mappingsV4 = []models.PortMappingV4{}
		}
		result["ipv4"] = mappingsV4
		if version == "v4" {
			result["total"] = len(mappingsV4)
		}
	}

	if version == "v6" || version == "all" {
		mappingsV6, err := pmService.ListV6Mappings(containerName)
		if err != nil {
			logger.Error("获取IPv6端口映射列表失败: %v", err)
			mappingsV6 = []models.PortMappingV6{}
		}
		result["ipv6"] = mappingsV6
		if version == "v6" {
			result["total"] = len(mappingsV6)
		}
	}

	if version == "all" {
		v4Len := 0
		v6Len := 0
		if v4, ok := result["ipv4"].([]models.PortMappingV4); ok {
			v4Len = len(v4)
		}
		if v6, ok := result["ipv6"].([]models.PortMappingV6); ok {
			v6Len = len(v6)
		}
		result["total"] = v4Len + v6Len
	}

	if containerName != "" {
		result["container"] = containerName
	}

	response.Success(c, result)
}
