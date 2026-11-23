package admin

import (
	"context"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"lxdapi/internal/db"
	"lxdapi/internal/service"
	"lxdapi/models"
	"lxdapi/pkg/logger"
	"lxdapi/pkg/response"
)

// ListPortMappings 获取端口映射列表（统一接口）
func ListPortMappings(c *gin.Context) {
	version := c.DefaultQuery("version", "all")
	containerName := c.Query("container")
	
	result := gin.H{}

	if version == "v4" || version == "all" {
		var mappings []models.PortMappingV4
		query := db.DB.Order("id DESC")
		if containerName != "" {
			query = query.Where("container_name = ?", containerName)
		}
		if err := query.Find(&mappings).Error; err != nil {
			response.Error(c, 500, "获取IPv4端口映射失败")
			return
		}
		result["ipv4"] = mappings
		result["ipv4_count"] = len(mappings)
	}

	if version == "v6" || version == "all" {
		var mappings []models.PortMappingV6
		query := db.DB.Order("id DESC")
		if containerName != "" {
			query = query.Where("container_name = ?", containerName)
		}
		if err := query.Find(&mappings).Error; err != nil {
			response.Error(c, 500, "获取IPv6端口映射失败")
			return
		}
		result["ipv6"] = mappings
		result["ipv6_count"] = len(mappings)
	}

	response.Success(c, result)
}

// AllocatePortMapping 分配端口映射（统一接口）
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
		Protocol      string `json:"protocol"`
		Description   string `json:"description"`
	}
	
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}

	if req.Protocol != "" {
		if err := service.ValidatePortMappingProtocol(req.Protocol); err != nil {
			response.Error(c, 400, err.Error())
			return
		}
	}

	portCount, err := service.NormalizeAndValidatePortCount(req.PortCount)
	if err != nil {
		response.Error(c, 400, err.Error())
		return
	}
	req.PortCount = portCount

	containerPortEnd, err := service.ValidatePortRange(req.ContainerPort, req.PortCount, "容器端口")
	if err != nil {
		response.Error(c, 400, err.Error())
		return
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

	ctx := context.Background()
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
			logger.Error("随机分配端口失败: %v", err)
			response.Error(c, 400, "随机分配端口失败")
			return
		}
	}

	publicPortEnd := req.PublicPort + req.PortCount - 1

	if version == "v4" {
		if _, err := service.ValidatePublicPortRange(req.PublicPort, req.PortCount, portRangeConfig.V4PortStart, portRangeConfig.V4PortEnd); err != nil {
			response.Error(c, 400, err.Error())
			return
		}

		var existingMappings []models.PortMappingV4
		db.DB.Where("container_name = ?", req.ContainerName).Find(&existingMappings)
		
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
			response.Error(c, 500, "获取NAT配置失败")
			return
		}
		
		if len(natConfigs) == 0 {
			response.Error(c, 400, "请先在IPv4规则管理页面配置NAT规则")
			return
		}

		for _, natConfig := range natConfigs {
			if err := pmService.CheckV4PortRangeAvailable(natConfig.DisplayIP, req.PublicPort, publicPortEnd, natConfig.Protocol); err != nil {
				response.Error(c, 400, fmt.Sprintf("端口检测失败: %v", err))
				return
			}
		}

		createdMappings := make([]models.PortMappingV4, 0, len(natConfigs))
		for _, natConfig := range natConfigs {
			mapping, err := pmService.AllocateV4Mapping(ctx, req.ContainerName, container.UserID, natConfig.IP, natConfig.DisplayIP, req.PublicPort, publicPortEnd, req.ContainerPort, containerPortEnd, natConfig.Protocol, natConfig.Interface, req.Description)
			if err != nil {
				logger.Error("为 %s:%s 创建端口映射失败: %v", natConfig.Interface, natConfig.IP, err)
				continue
			}
			createdMappings = append(createdMappings, *mapping)
		}

		if len(createdMappings) == 0 {
			response.Error(c, 500, "所有规则创建失败")
			return
		}

		response.Success(c, gin.H{
			"mappings": createdMappings,
			"total":    len(createdMappings),
		})
	} else {
		if _, err := service.ValidatePublicPortRange(req.PublicPort, req.PortCount, portRangeConfig.V6PortStart, portRangeConfig.V6PortEnd); err != nil {
			response.Error(c, 400, err.Error())
			return
		}

		var existingMappings []models.PortMappingV6
		db.DB.Where("container_name = ?", req.ContainerName).Find(&existingMappings)
		
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
			response.Error(c, 500, "获取NAT配置失败")
			return
		}
		
		if len(natConfigs) == 0 {
			response.Error(c, 400, "请先在IPv6规则管理页面配置NAT规则")
			return
		}

		for _, natConfig := range natConfigs {
			if err := pmService.CheckV6PortRangeAvailable(natConfig.DisplayIP, req.PublicPort, publicPortEnd, natConfig.Protocol); err != nil {
				response.Error(c, 400, fmt.Sprintf("端口检测失败: %v", err))
				return
			}
		}

		createdMappings := make([]models.PortMappingV6, 0, len(natConfigs))
		for _, natConfig := range natConfigs {
			mapping, err := pmService.AllocateV6Mapping(ctx, req.ContainerName, container.UserID, natConfig.IP, natConfig.DisplayIP, req.PublicPort, publicPortEnd, req.ContainerPort, containerPortEnd, natConfig.Protocol, natConfig.Interface, req.Description)
			if err != nil {
				logger.Error("为 %s:%s 创建端口映射失败: %v", natConfig.Interface, natConfig.IP, err)
				continue
			}
			createdMappings = append(createdMappings, *mapping)
		}

		if len(createdMappings) == 0 {
			response.Error(c, 500, "所有规则创建失败")
			return
		}

		response.Success(c, gin.H{
			"mappings": createdMappings,
			"total":    len(createdMappings),
		})
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
		ID            uint   `json:"id"`
		IDs           []uint `json:"ids"`
		ContainerName string `json:"container_name"`
		PublicPort    int    `json:"public_port"`
		Protocol      string `json:"protocol"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}

	pmService := service.NewPortMappingService()

	if len(req.IDs) > 0 {
		successIDs := []uint{}
		failed := []gin.H{}
		if version == "v4" {
			for _, id := range req.IDs {
				if err := pmService.ReleaseV4Mapping(id); err != nil {
					logger.Error("释放IPv4端口映射 %d 失败: %v", id, err)
					failed = append(failed, gin.H{"id": id, "error": "释放失败"})
				} else {
					successIDs = append(successIDs, id)
				}
			}
		} else {
			for _, id := range req.IDs {
				if err := pmService.ReleaseV6Mapping(id); err != nil {
					logger.Error("释放IPv6端口映射 %d 失败: %v", id, err)
					failed = append(failed, gin.H{"id": id, "error": "释放失败"})
				} else {
					successIDs = append(successIDs, id)
				}
			}
		}
		if len(failed) > 0 {
			response.ErrorWithData(c, 500, "部分端口映射释放失败", gin.H{
				"released": len(successIDs),
				"success":  successIDs,
				"failed":   failed,
			})
			return
		}
		response.Success(c, gin.H{
			"released": len(successIDs),
			"success":  successIDs,
			"failed":   failed,
			"message":  "批量释放完成",
		})
		return
	}

	if req.ID > 0 {
		if version == "v4" {
			if err := pmService.ReleaseV4Mapping(req.ID); err != nil {
				logger.Error("释放IPv4端口映射 %d 失败: %v", req.ID, err)
				response.Error(c, 500, "释放端口映射失败")
				return
			}
		} else {
			if err := pmService.ReleaseV6Mapping(req.ID); err != nil {
				logger.Error("释放IPv6端口映射 %d 失败: %v", req.ID, err)
				response.Error(c, 500, "释放端口映射失败")
				return
			}
		}
		response.Success(c, "释放成功")
		return
	}

	if req.ContainerName != "" && req.PublicPort > 0 {
		if req.Protocol != "" {
			if err := service.ValidatePortMappingProtocol(req.Protocol); err != nil {
				response.Error(c, 400, err.Error())
				return
			}
		}
		if version == "v4" {
			var mappings []models.PortMappingV4
			query := db.DB.Where("container_name = ? AND public_port = ?", req.ContainerName, req.PublicPort)
			if req.Protocol != "" {
				query = query.Where("protocol = ?", req.Protocol)
			}
			if err := query.Find(&mappings).Error; err != nil || len(mappings) == 0 {
				response.Error(c, 404, "映射不存在")
				return
			}
			for _, mapping := range mappings {
				if err := pmService.ReleaseV4Mapping(mapping.ID); err != nil {
					logger.Error("释放IPv4端口映射 %d 失败: %v", mapping.ID, err)
					response.Error(c, 500, "释放端口映射失败")
					return
				}
			}
		} else {
			var mappings []models.PortMappingV6
			query := db.DB.Where("container_name = ? AND public_port = ?", req.ContainerName, req.PublicPort)
			if req.Protocol != "" {
				query = query.Where("protocol = ?", req.Protocol)
			}
			if err := query.Find(&mappings).Error; err != nil || len(mappings) == 0 {
				response.Error(c, 404, "映射不存在")
				return
			}
			for _, mapping := range mappings {
				if err := pmService.ReleaseV6Mapping(mapping.ID); err != nil {
					logger.Error("释放IPv6端口映射 %d 失败: %v", mapping.ID, err)
					response.Error(c, 500, "释放端口映射失败")
					return
				}
			}
		}
		response.Success(c, "释放成功")
		return
	}

	response.Error(c, 400, "请提供id、ids或container_name+public_port")
}

// GetNATConfig 获取NAT配置（统一接口）
func GetNATConfig(c *gin.Context) {
	version := c.Query("version")
	if version != "v4" && version != "v6" {
		response.Error(c, 400, "version参数必须是v4或v6")
		return
	}

	if version == "v4" {
		var configs []models.NATConfigV4
		db.DB.Find(&configs)
		response.Success(c, gin.H{
			"configs": configs,
			"total":   len(configs),
		})
	} else {
		var configs []models.NATConfigV6
		db.DB.Find(&configs)
		response.Success(c, gin.H{
			"configs": configs,
			"total":   len(configs),
		})
	}
}

// SaveNATConfig 保存NAT配置（统一接口）
func SaveNATConfig(c *gin.Context) {
	version := c.Query("version")
	if version != "v4" && version != "v6" {
		response.Error(c, 400, "version参数必须是v4或v6")
		return
	}

	type NATConfigItem struct {
		IP        string `json:"ip"`
		DisplayIP string `json:"display_ip"`
		Interface string `json:"interface"`
		Protocol  string `json:"protocol"`
	}

	var configs []NATConfigItem
	if err := c.ShouldBindJSON(&configs); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}

	for _, config := range configs {
		if !isValidIPByVersion(config.IP, version) {
			response.Error(c, 400, "NAT IP地址格式无效")
			return
		}
		if config.DisplayIP != "" && !isValidIPByVersion(config.DisplayIP, version) {
			response.Error(c, 400, "NAT展示IP地址格式无效")
			return
		}
		if !isValidInterfaceName(config.Interface) {
			response.Error(c, 400, "NAT网卡名称格式无效")
			return
		}
		proto := strings.ToLower(strings.TrimSpace(config.Protocol))
		if proto != "" && proto != "tcp" && proto != "udp" && proto != "both" && proto != "tcp/udp" && proto != "all" {
			response.Error(c, 400, "NAT协议类型无效")
			return
		}
	}

	if version == "v4" {
		db.DB.Unscoped().Where("1 = 1").Delete(&models.NATConfigV4{})

		for _, config := range configs {
			displayIP := config.DisplayIP
			if displayIP == "" {
				displayIP = config.IP
			}
			natConfig := models.NATConfigV4{
				IP:        config.IP,
				DisplayIP: displayIP,
				Interface: config.Interface,
				Protocol:  config.Protocol,
			}
			if err := db.DB.Create(&natConfig).Error; err != nil {
				logger.Error("保存NAT配置失败: %v", err)
				response.Error(c, 500, "保存失败")
				return
			}
		}

		logger.OK("IPv4 NAT配置已更新，共 %d 条规则", len(configs))
		response.Success(c, gin.H{
			"count":   len(configs),
			"message": "配置保存成功",
		})
	} else {
		db.DB.Unscoped().Where("1 = 1").Delete(&models.NATConfigV6{})

		for _, config := range configs {
			displayIP := config.DisplayIP
			if displayIP == "" {
				displayIP = config.IP
			}
			natConfig := models.NATConfigV6{
				IP:        config.IP,
				DisplayIP: displayIP,
				Interface: config.Interface,
				Protocol:  config.Protocol,
			}
			if err := db.DB.Create(&natConfig).Error; err != nil {
				logger.Error("保存NAT配置失败: %v", err)
				response.Error(c, 500, "保存失败")
				return
			}
		}

		logger.OK("IPv6 NAT配置已更新，共 %d 条规则", len(configs))
		response.Success(c, gin.H{
			"count":   len(configs),
			"message": "配置保存成功",
		})
	}
}

func splitStringToSlice(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}
