package user

import (
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"lxdapi/internal/db"
	"lxdapi/internal/service"
	"lxdapi/models"
	"lxdapi/pkg/logger"
	"lxdapi/pkg/response"
)

// AllocatePortMapping 分配端口映射（统一接口）
// @Summary 分配端口映射
// @Description 为容器分配端口映射，通过version参数区分IPv4/IPv6
// @Tags User API - 端口映射
// @Accept json
// @Produce json
// @Param version query string true "IP版本: v4 或 v6"
// @Param request body object true "端口映射参数"
// @Success 200 {object} response.Response "分配成功"
// @Failure 400 {object} response.Response "参数错误"
// @Failure 403 {object} response.Response "无权操作"
// @Failure 404 {object} response.Response "容器不存在"
// @Failure 500 {object} response.Response "分配失败"
// @Security UserSession
// @Router /api/user/port-mapping/allocate [post]
func AllocatePortMapping(c *gin.Context) {
	version := c.Query("version")
	if version != "v4" && version != "v6" {
		response.Error(c, 400, "version参数必须是v4或v6")
		return
	}

	var req struct {
		ContainerName string `json:"container_name" binding:"required"`
		PublicPort    int    `json:"public_port"`
		ContainerPort int    `json:"container_port" binding:"required"`
		PortCount     int    `json:"port_count"`
		Description   string `json:"description"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误: "+err.Error())
		return
	}

	username := c.GetString("username")
	if username == "" {
		response.Error(c, 401, "未授权")
		return
	}

	if req.PortCount <= 0 {
		req.PortCount = 1
	}

	var portRangeConfig models.PortRangeConfig
	if err := db.DB.First(&portRangeConfig).Error; err != nil {
		portRangeConfig = models.PortRangeConfig{
			V4PortStart: 10000,
			V4PortEnd:   65535,
			V6PortStart: 10000,
			V6PortEnd:   65535,
		}
	}

	var container models.Container
	if err := db.DB.Where("name = ?", req.ContainerName).First(&container).Error; err != nil {
		response.Error(c, 404, "容器不存在")
		return
	}

	if container.UserID != username {
		response.Error(c, 403, "无权限访问此容器")
		return
	}

	if deny_code, deny_msg := service.CheckContainerOperationAllowed(&container, true, "allocate_port_mapping"); deny_code != 0 {
		response.Error(c, deny_code, deny_msg)
		return
	}

	pmService := service.NewPortMappingService()

	// 如果公网端口为0，随机分配
	if req.PublicPort == 0 {
		var err error
		if version == "v4" {
			req.PublicPort, err = pmService.FindAvailableV4Port(portRangeConfig.V4PortStart, portRangeConfig.V4PortEnd, req.PortCount, "both")
		} else {
			req.PublicPort, err = pmService.FindAvailableV6Port(portRangeConfig.V6PortStart, portRangeConfig.V6PortEnd, req.PortCount, "both")
		}
		if err != nil {
			response.Error(c, 400, "随机分配端口失败: "+err.Error())
			return
		}
	}

	publicPortEnd := req.PublicPort + req.PortCount - 1
	containerPortEnd := req.ContainerPort + req.PortCount - 1

	ctx := context.Background()

	if version == "v4" {
		if req.PublicPort < portRangeConfig.V4PortStart || req.PublicPort > portRangeConfig.V4PortEnd {
			response.Error(c, 400, fmt.Sprintf("公网端口必须在 %d-%d 范围内", portRangeConfig.V4PortStart, portRangeConfig.V4PortEnd))
			return
		}

		var existingMappings []models.PortMappingV4
		if err := db.DB.Where("container_name = ?", req.ContainerName).Find(&existingMappings).Error; err != nil {
			response.Error(c, 500, "查询已有端口映射失败")
			return
		}

		ruleMap := make(map[string]int)
		for _, m := range existingMappings {
			key := fmt.Sprintf("%d-%d-%d-%s", m.PublicPort, m.PublicPortEnd, m.ContainerPort, m.Protocol)
			if _, exists := ruleMap[key]; !exists {
				if m.PublicPortEnd > 0 && m.PublicPortEnd != m.PublicPort {
					ruleMap[key] = m.PublicPortEnd - m.PublicPort + 1
				} else {
					ruleMap[key] = 1
				}
			}
		}

		usedPorts := 0
		for _, count := range ruleMap {
			usedPorts += count
		}

		if usedPorts+req.PortCount > container.IPv4MappingLimit {
			response.Error(c, 400, fmt.Sprintf("超过IPv4端口映射配额限制，已用%d个端口，限制%d个端口", usedPorts, container.IPv4MappingLimit))
			return
		}

		var natConfigs []models.NATConfigV4
		if err := db.DB.Find(&natConfigs).Error; err != nil {
			logger.Error("获取NAT配置失败: %v", err)
			response.Error(c, 500, "获取NAT配置失败")
			return
		}

		if len(natConfigs) == 0 {
			response.Error(c, 400, "请先配置NAT规则")
			return
		}

		for _, natConfig := range natConfigs {
			if err := pmService.CheckV4PortRangeAvailable(natConfig.DisplayIP, req.PublicPort, publicPortEnd, natConfig.Protocol); err != nil {
				response.Error(c, 400, fmt.Sprintf("端口检测失败: %v", err))
				return
			}
		}

		createdMappings := make([]models.PortMappingV4, 0, len(natConfigs))
		failedConfigs := make([]string, 0)

		for _, natConfig := range natConfigs {
			mapping, err := pmService.AllocateV4Mapping(ctx, req.ContainerName, container.UserID, natConfig.IP, natConfig.DisplayIP, req.PublicPort, publicPortEnd, req.ContainerPort, containerPortEnd, natConfig.Protocol, natConfig.Interface, req.Description)
			if err != nil {
				logger.Error("为 %s:%s 创建端口映射失败: %v", natConfig.Interface, natConfig.IP, err)
				failedConfigs = append(failedConfigs, natConfig.Interface+":"+natConfig.IP)
				continue
			}
			createdMappings = append(createdMappings, *mapping)
			logger.OK("为容器 %s 创建IPv4端口映射: %s(%s):%d -> %d", req.ContainerName, natConfig.DisplayIP, natConfig.Interface, req.PublicPort, req.ContainerPort)
		}

		if len(createdMappings) == 0 {
			response.Error(c, 500, "所有规则创建失败")
			return
		}

		result := gin.H{
			"mappings": createdMappings,
			"total":    len(createdMappings),
			"success":  len(createdMappings),
			"failed":   len(failedConfigs),
		}

		if len(failedConfigs) > 0 {
			result["failed_configs"] = failedConfigs
			result["message"] = "部分规则创建失败"
		}

		response.Success(c, result)
	} else {
		if req.PublicPort < portRangeConfig.V6PortStart || req.PublicPort > portRangeConfig.V6PortEnd {
			response.Error(c, 400, fmt.Sprintf("公网端口必须在 %d-%d 范围内", portRangeConfig.V6PortStart, portRangeConfig.V6PortEnd))
			return
		}

		var existingMappings []models.PortMappingV6
		if err := db.DB.Where("container_name = ?", req.ContainerName).Find(&existingMappings).Error; err != nil {
			response.Error(c, 500, "查询已有端口映射失败")
			return
		}

		ruleMap := make(map[string]int)
		for _, m := range existingMappings {
			key := fmt.Sprintf("%d-%d-%d-%s", m.PublicPort, m.PublicPortEnd, m.ContainerPort, m.Protocol)
			if _, exists := ruleMap[key]; !exists {
				if m.PublicPortEnd > 0 && m.PublicPortEnd != m.PublicPort {
					ruleMap[key] = m.PublicPortEnd - m.PublicPort + 1
				} else {
					ruleMap[key] = 1
				}
			}
		}

		usedPorts := 0
		for _, count := range ruleMap {
			usedPorts += count
		}

		if usedPorts+req.PortCount > container.IPv6MappingLimit {
			response.Error(c, 400, fmt.Sprintf("超过IPv6端口映射配额限制，已用%d个端口，限制%d个端口", usedPorts, container.IPv6MappingLimit))
			return
		}

		var natConfigs []models.NATConfigV6
		if err := db.DB.Find(&natConfigs).Error; err != nil {
			logger.Error("获取NAT配置失败: %v", err)
			response.Error(c, 500, "获取NAT配置失败")
			return
		}

		if len(natConfigs) == 0 {
			response.Error(c, 400, "请先配置NAT规则")
			return
		}

		for _, natConfig := range natConfigs {
			if err := pmService.CheckV6PortRangeAvailable(natConfig.DisplayIP, req.PublicPort, publicPortEnd, natConfig.Protocol); err != nil {
				response.Error(c, 400, fmt.Sprintf("端口检测失败: %v", err))
				return
			}
		}

		createdMappings := make([]models.PortMappingV6, 0, len(natConfigs))
		failedConfigs := make([]string, 0)

		for _, natConfig := range natConfigs {
			mapping, err := pmService.AllocateV6Mapping(ctx, req.ContainerName, container.UserID, natConfig.IP, natConfig.DisplayIP, req.PublicPort, publicPortEnd, req.ContainerPort, containerPortEnd, natConfig.Protocol, natConfig.Interface, req.Description)
			if err != nil {
				logger.Error("为 %s:%s 创建IPv6端口映射失败: %v", natConfig.Interface, natConfig.IP, err)
				failedConfigs = append(failedConfigs, natConfig.Interface+":"+natConfig.IP)
				continue
			}
			createdMappings = append(createdMappings, *mapping)
			logger.OK("为容器 %s 创建IPv6端口映射: %s(%s):%d -> %d", req.ContainerName, natConfig.DisplayIP, natConfig.Interface, req.PublicPort, req.ContainerPort)
		}

		if len(createdMappings) == 0 {
			response.Error(c, 500, "所有规则创建失败")
			return
		}

		result := gin.H{
			"mappings": createdMappings,
			"total":    len(createdMappings),
			"success":  len(createdMappings),
			"failed":   len(failedConfigs),
		}

		if len(failedConfigs) > 0 {
			result["failed_configs"] = failedConfigs
			result["message"] = "部分规则创建失败"
		}

		response.Success(c, result)
	}
}

// ReleasePortMapping 释放端口映射（统一接口）
// @Summary 释放端口映射
// @Description 释放端口映射，通过version参数区分IPv4/IPv6
// @Tags User API - 端口映射
// @Accept json
// @Produce json
// @Param version query string true "IP版本: v4 或 v6"
// @Param request body object{ids=[]uint} true "端口映射ID列表"
// @Success 200 {object} response.Response "释放成功"
// @Failure 400 {object} response.Response "参数错误"
// @Failure 403 {object} response.Response "无权操作"
// @Failure 404 {object} response.Response "端口映射不存在"
// @Failure 500 {object} response.Response "释放失败"
// @Security UserSession
// @Router /api/user/port-mapping/release [post]
func ReleasePortMapping(c *gin.Context) {
	version := c.Query("version")
	if version != "v4" && version != "v6" {
		response.Error(c, 400, "version参数必须是v4或v6")
		return
	}

	var req struct {
		IDs []uint `json:"ids" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}

	username := c.GetString("username")
	if username == "" {
		response.Error(c, 401, "未授权")
		return
	}

	pmService := service.NewPortMappingService()

	if version == "v4" {
		var mappings []models.PortMappingV4
		if err := db.DB.Where("id IN ?", req.IDs).Find(&mappings).Error; err != nil {
			response.Error(c, 500, "查询端口映射失败")
			return
		}

		if len(mappings) == 0 {
			response.Error(c, 404, "端口映射不存在")
			return
		}

		for _, m := range mappings {
			if m.UserID != username {
				response.Error(c, 403, "无权限删除其他用户的端口映射")
				return
			}
			if deny_code, deny_msg := service.CheckContainerOperationByName(m.ContainerName, true, "release_port_mapping"); deny_code != 0 {
				response.Error(c, deny_code, deny_msg)
				return
			}
		}

		for _, mapping := range mappings {
			if err := pmService.ReleaseV4Mapping(mapping.ID); err != nil {
				logger.Error("释放IPv4端口映射失败 ID=%d: %v", mapping.ID, err)
			}
		}

		response.Success(c, gin.H{"deleted": len(mappings)})
	} else {
		var mappings []models.PortMappingV6
		if err := db.DB.Where("id IN ?", req.IDs).Find(&mappings).Error; err != nil {
			response.Error(c, 500, "查询端口映射失败")
			return
		}

		if len(mappings) == 0 {
			response.Error(c, 404, "端口映射不存在")
			return
		}

		for _, m := range mappings {
			if m.UserID != username {
				response.Error(c, 403, "无权限删除其他用户的端口映射")
				return
			}
			if deny_code, deny_msg := service.CheckContainerOperationByName(m.ContainerName, true, "release_port_mapping"); deny_code != 0 {
				response.Error(c, deny_code, deny_msg)
				return
			}
		}

		for _, mapping := range mappings {
			if err := pmService.ReleaseV6Mapping(mapping.ID); err != nil {
				logger.Error("释放IPv6端口映射失败 ID=%d: %v", mapping.ID, err)
			}
		}

		response.Success(c, gin.H{"deleted": len(mappings)})
	}
}

// ListPortMappings 获取端口映射列表（统一接口）
// @Summary 获取端口映射列表
// @Description 获取端口映射列表，通过version参数区分IPv4/IPv6
// @Tags User API - 端口映射
// @Accept json
// @Produce json
// @Param version query string false "IP版本: v4/v6/all，默认all"
// @Param container_name query string false "容器名称，不传则返回用户所有端口映射"
// @Success 200 {object} response.Response "获取成功"
// @Failure 403 {object} response.Response "无权访问"
// @Failure 404 {object} response.Response "容器不存在"
// @Failure 500 {object} response.Response "获取失败"
// @Security UserSession
// @Router /api/user/port-mapping [get]
func ListPortMappings(c *gin.Context) {
	version := c.DefaultQuery("version", "all")
	containerName := c.Query("container_name")

	username := c.GetString("username")
	if username == "" {
		response.Error(c, 401, "未授权")
		return
	}

	if containerName != "" {
		var container models.Container
		if err := db.DB.Where("name = ?", containerName).First(&container).Error; err != nil {
			response.Error(c, 404, "容器不存在")
			return
		}
		if container.UserID != username {
			response.Error(c, 403, "无权限访问此容器")
			return
		}
	}

	result := gin.H{}

	if version == "v4" || version == "all" {
		query := db.DB.Model(&models.PortMappingV4{})
		if containerName != "" {
			query = query.Where("container_name = ?", containerName)
		} else {
			query = query.Where("user_id = ?", username)
		}

		var mappingsV4 []models.PortMappingV4
		if err := query.Order("created_at DESC").Find(&mappingsV4).Error; err != nil {
			if version == "v4" {
				response.Error(c, 500, "查询端口映射失败")
				return
			}
			mappingsV4 = []models.PortMappingV4{}
		}
		result["ipv4"] = mappingsV4
		result["ipv4_count"] = len(mappingsV4)
	}

	if version == "v6" || version == "all" {
		query := db.DB.Model(&models.PortMappingV6{})
		if containerName != "" {
			query = query.Where("container_name = ?", containerName)
		} else {
			query = query.Where("user_id = ?", username)
		}

		var mappingsV6 []models.PortMappingV6
		if err := query.Order("created_at DESC").Find(&mappingsV6).Error; err != nil {
			if version == "v6" {
				response.Error(c, 500, "查询端口映射失败")
				return
			}
			mappingsV6 = []models.PortMappingV6{}
		}
		result["ipv6"] = mappingsV6
		result["ipv6_count"] = len(mappingsV6)
	}

	response.Success(c, result)
}
