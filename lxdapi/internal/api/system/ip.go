package system

import (
	"github.com/gin-gonic/gin"
	"lxdapi/internal/ipv4"
	"lxdapi/internal/ipv6"
	"lxdapi/internal/service"
	"lxdapi/pkg/logger"
	"lxdapi/pkg/response"
)

var ipv4Service *service.IPv4Service
var ipv6Service *service.IPv6Service

func InitIPv4Service(svc *service.IPv4Service) {
	ipv4Service = svc
}

func InitIPv6Service(svc *service.IPv6Service) {
	ipv6Service = svc
}

// GetContainerIP 获取容器IP地址（统一接口）
// @Summary 获取容器IP地址
// @Description 获取指定容器的IP地址，通过version参数区分IPv4/IPv6
// @Tags System API - IP管理
// @Accept json
// @Produce json
// @Param container query string true "容器名称"
// @Param version query string false "IP版本: v4/v6/all，默认all"
// @Success 200 {object} response.Response "获取成功"
// @Failure 400 {object} response.Response "缺少容器名称"
// @Failure 500 {object} response.Response "获取失败"
// @Security ApiKeyAuth
// @Router /api/system/ip [get]
func GetContainerIP(c *gin.Context) {
	name := c.Query("container")
	if name == "" {
		response.Error(c, 400, "缺少容器名称")
		return
	}

	version := c.DefaultQuery("version", "all")
	result := gin.H{"container": name}

	if version == "v4" || version == "all" {
		if ipv4.GlobalManager == nil {
			if version == "v4" {
				response.Error(c, 500, "IPv4功能未启用")
				return
			}
			result["ipv4"] = []string{}
			result["ipv4_count"] = 0
		} else {
			ips, err := ipv4.GlobalManager.GetContainerIPs(name)
			if err != nil {
				logger.Error("获取容器 %s IPv4失败: %v", name, err)
				if version == "v4" {
					response.Error(c, 500, "获取IPv4地址失败")
					return
				}
				result["ipv4"] = []string{}
				result["ipv4_count"] = 0
			} else {
				result["ipv4"] = ips
				result["ipv4_count"] = len(ips)
			}
		}
	}

	if version == "v6" || version == "all" {
		if ipv6.GlobalManager == nil {
			if version == "v6" {
				response.Error(c, 500, "IPv6功能未启用")
				return
			}
			result["ipv6"] = []string{}
			result["ipv6_count"] = 0
		} else {
			ips, err := ipv6.GlobalManager.GetContainerIPs(name)
			if err != nil {
				logger.Error("获取容器 %s IPv6失败: %v", name, err)
				if version == "v6" {
					response.Error(c, 500, "获取IPv6地址失败")
					return
				}
				result["ipv6"] = []string{}
				result["ipv6_count"] = 0
			} else {
				result["ipv6"] = ips
				result["ipv6_count"] = len(ips)
			}
		}
	}

	response.Success(c, result)
}

// AllocateIP 分配IP地址（统一接口）
// @Summary 分配IP地址
// @Description 为指定容器分配IP地址，通过version参数区分IPv4/IPv6
// @Tags System API - IP管理
// @Accept json
// @Produce json
// @Param version query string true "IP版本: v4 或 v6"
// @Param request body object true "分配参数"
// @Success 200 {object} response.Response "分配成功"
// @Failure 400 {object} response.Response "参数错误"
// @Failure 500 {object} response.Response "分配失败"
// @Security ApiKeyAuth
// @Router /api/system/ip/allocate [post]
func AllocateIP(c *gin.Context) {
	version := c.Query("version")
	if version != "v4" && version != "v6" {
		response.Error(c, 400, "version参数必须是v4或v6")
		return
	}

	var req struct {
		Name   string `json:"name" binding:"required"`
		UserID string `json:"user_id"`
		Count  int    `json:"count"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误: "+err.Error())
		return
	}

	if req.Count <= 0 {
		req.Count = 1
	}

	ctx := c.Request.Context()

	if version == "v4" {
		ips, err := ipv4Service.AllocateIPv4(ctx, req.Name, req.UserID, req.Count)
		if err != nil {
			logger.Error("分配容器 %s IPv4失败: %v", req.Name, err)
			response.Error(c, 500, "分配IPv4地址失败")
			return
		}
		response.Success(c, gin.H{
			"container": req.Name,
			"ipv4":      ips,
			"count":     len(ips),
		})
	} else {
		ips, err := ipv6Service.AllocateIPv6(ctx, req.Name, req.UserID, req.Count)
		if err != nil {
			logger.Error("分配容器 %s IPv6失败: %v", req.Name, err)
			response.Error(c, 500, "分配IPv6地址失败")
			return
		}
		response.Success(c, gin.H{
			"container": req.Name,
			"ipv6":      ips,
			"count":     len(ips),
		})
	}
}

// ReleaseIP 释放IP地址（统一接口）
// @Summary 释放IP地址
// @Description 释放容器的指定IP地址回地址池，通过version参数区分IPv4/IPv6
// @Tags System API - IP管理
// @Accept json
// @Produce json
// @Param version query string true "IP版本: v4 或 v6"
// @Param request body object true "释放参数"
// @Success 200 {object} response.Response "释放成功"
// @Failure 400 {object} response.Response "参数错误"
// @Failure 500 {object} response.Response "释放失败"
// @Security ApiKeyAuth
// @Router /api/system/ip/release [post]
func ReleaseIP(c *gin.Context) {
	version := c.Query("version")
	if version != "v4" && version != "v6" {
		response.Error(c, 400, "version参数必须是v4或v6")
		return
	}

	var req struct {
		Name string `json:"name" binding:"required"`
		IP   string `json:"ip" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误: "+err.Error())
		return
	}

	if version == "v4" {
		if err := ipv4Service.ReleaseIPv4(req.Name, req.IP); err != nil {
			logger.Error("释放容器 %s IPv4失败: %v", req.Name, err)
			response.Error(c, 500, "释放IPv4地址失败")
			return
		}
		response.Success(c, "IPv4释放成功")
	} else {
		if err := ipv6Service.ReleaseIPv6(req.Name, req.IP); err != nil {
			logger.Error("释放容器 %s IPv6失败: %v", req.Name, err)
			response.Error(c, 500, "释放IPv6地址失败")
			return
		}
		response.Success(c, "IPv6释放成功")
	}
}
