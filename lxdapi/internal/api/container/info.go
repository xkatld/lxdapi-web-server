package container

import (
	"context"
	"github.com/gin-gonic/gin"
	"lxdapi/internal/db"
	"lxdapi/internal/executor"
	"lxdapi/internal/service"
	"lxdapi/models"
	"lxdapi/pkg/logger"
	"lxdapi/pkg/response"
)

var containerService *service.ContainerService

func InitContainerService(svc *service.ContainerService) {
	containerService = svc
}

// GetInfo 获取容器信息（合并info+status+traffic）
// @Summary 获取容器信息
// @Description 获取容器详细信息，包含状态和流量
// @Tags Container API - 信息
// @Accept json
// @Produce json
// @Success 200 {object} response.Response "获取成功"
// @Failure 404 {object} response.Response "容器不存在"
// @Security ContainerAuth
// @Router /api/container/info [get]
func GetInfo(c *gin.Context) {
	containerName, _ := c.Get("container_name")
	name := containerName.(string)

	container, err := containerService.Get(name)
	if err != nil {
		logger.Error("获取容器信息失败: %v", err)
		response.Error(c, 404, err.Error())
		return
	}

	svc := service.NewContainerService()
	info, err := svc.GetInfo(c.Request.Context(), name)
	if err != nil {
		logger.Warn("获取容器详细信息失败，返回基本信息: %v", err)
		info = map[string]interface{}{
			"name":           name,
			"status":         container.Status,
			"freeze_source":  container.FreezeSource,
			"traffic_locked": service.IsContainerTrafficLocked(container),
		}
	}

	var traffic models.Traffic
	if err := db.DB.Where("container_name = ?", name).First(&traffic).Error; err == nil {
		info["traffic"] = traffic
	}

	response.Success(c, info)
}

// GetTemplateList 获取模板列表
// @Summary 获取模板列表
// @Description 获取可用容器模板列表
// @Tags Container API - 信息
// @Accept json
// @Produce json
// @Success 200 {object} response.Response "获取成功"
// @Failure 500 {object} response.Response "获取失败"
// @Security ContainerAuth
// @Router /api/container/templates [get]
func GetTemplateList(c *gin.Context) {
	svc := service.NewTemplateService()

	templates, err := svc.List()
	if err != nil {
		logger.Error("获取模板列表失败: %v", err)
		response.Error(c, 500, "获取模板列表失败: "+err.Error())
		return
	}

	response.Success(c, gin.H{
		"templates": templates,
		"count":     len(templates),
	})
}

// Action 容器操作统一接口
// @Summary 容器操作
// @Description 对容器执行操作: start/stop/restart/reinstall/reset-password
// @Tags Container API - 操作
// @Accept json
// @Produce json
// @Param action query string true "操作类型: start/stop/restart/reinstall/reset-password"
// @Param request body object false "操作参数（reinstall需要image/password，reset-password需要password）"
// @Success 200 {object} response.Response "操作成功"
// @Failure 400 {object} response.Response "参数错误"
// @Failure 404 {object} response.Response "容器不存在"
// @Failure 500 {object} response.Response "操作失败"
// @Security ContainerAuth
// @Router /api/container/action [post]
func Action(c *gin.Context) {
	containerName, _ := c.Get("container_name")
	name := containerName.(string)

	action := c.Query("action")
	if action == "" {
		response.Error(c, 400, "缺少操作类型")
		return
	}

	var dbContainer models.Container
	if err := db.DB.Where("name = ?", name).First(&dbContainer).Error; err != nil {
		response.Error(c, 404, "容器不存在")
		return
	}

	if deny_code, deny_msg := service.CheckContainerOperationAllowed(&dbContainer, false, action); deny_code != 0 {
		response.Error(c, deny_code, deny_msg)
		return
	}

	userID := dbContainer.UserID

	switch action {
	case "start":
		err := executor.CreateSyncTask(name, "start", userID, func(ctx context.Context) error {
			return containerService.Start(ctx, name)
		})
		if err != nil {
			response.Error(c, 500, err.Error())
			return
		}
		logger.OK("容器启动成功: %s", name)
		response.Success(c, "容器启动成功")

	case "stop":
		err := executor.CreateSyncTask(name, "stop", userID, func(ctx context.Context) error {
			return containerService.Stop(ctx, name)
		})
		if err != nil {
			response.Error(c, 500, err.Error())
			return
		}
		logger.OK("容器停止成功: %s", name)
		response.Success(c, "容器停止成功")

	case "restart":
		err := executor.CreateSyncTask(name, "restart", userID, func(ctx context.Context) error {
			return containerService.Restart(ctx, name)
		})
		if err != nil {
			response.Error(c, 500, err.Error())
			return
		}
		logger.OK("容器重启成功: %s", name)
		response.Success(c, "容器重启成功")

	case "reinstall":
		var req struct {
			Image    string `json:"image"`
			Password string `json:"password"`
		}
		c.ShouldBindJSON(&req)

		finalImage := req.Image
		if finalImage == "" {
			finalImage = dbContainer.Image
		}

		params := map[string]interface{}{
			"container_name": name,
			"original_image": dbContainer.Image,
			"new_image":      finalImage,
			"password_set":   req.Password != "",
			"user_id":        userID,
			"image":          finalImage,
			"password":       req.Password,
		}

		task, err := executor.CreateTask(name, "reinstall", userID, params, func(ctx context.Context) error {
			return containerService.Reinstall(ctx, name, req.Image, req.Password)
		})
		if err != nil {
			response.Error(c, 500, "创建任务失败: "+err.Error())
			return
		}
		logger.OK("容器重装任务已创建: %s, 任务ID: %d", name, task.ID)
		response.Success(c, gin.H{
			"task_id": task.ID,
			"message": "重装任务已创建，正在后台执行",
		})

	case "reset-password":
		var req struct {
			Password string `json:"password" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			response.Error(c, 400, "参数错误: 缺少password")
			return
		}

		svc := service.NewContainerService()
		if err := svc.ResetPassword(c.Request.Context(), name, req.Password); err != nil {
			response.Error(c, 500, err.Error())
			return
		}
		logger.OK("容器密码重置成功: %s", name)
		response.Success(c, "密码重置成功")

	default:
		response.Error(c, 400, "不支持的操作类型: "+action)
	}
}

// GetIP 获取容器IP地址（统一接口）
// @Summary 获取容器IP地址
// @Description 获取容器的IP地址，通过version参数区分IPv4/IPv6
// @Tags Container API - IP管理
// @Accept json
// @Produce json
// @Param version query string false "IP版本: v4/v6/all，默认all"
// @Success 200 {object} response.Response "获取成功"
// @Failure 500 {object} response.Response "获取失败"
// @Security ContainerAuth
// @Router /api/container/ip [get]
func GetIP(c *gin.Context) {
	containerName, _ := c.Get("container_name")
	name := containerName.(string)

	version := c.DefaultQuery("version", "all")
	result := gin.H{"container": name}

	if version == "v4" || version == "all" {
		var bindings []models.IPv4Binding
		if err := db.DB.Where("container_name = ?", name).Find(&bindings).Error; err != nil {
			if version == "v4" {
				response.Error(c, 500, err.Error())
				return
			}
			bindings = []models.IPv4Binding{}
		}
		result["ipv4"] = bindings
		result["ipv4_count"] = len(bindings)
	}

	if version == "v6" || version == "all" {
		var bindings []models.IPv6Binding
		if err := db.DB.Where("container_name = ?", name).Find(&bindings).Error; err != nil {
			if version == "v6" {
				response.Error(c, 500, err.Error())
				return
			}
			bindings = []models.IPv6Binding{}
		}
		result["ipv6"] = bindings
		result["ipv6_count"] = len(bindings)
	}

	response.Success(c, result)
}

// AllocateIP 分配IP地址（统一接口）
// @Summary 分配IP地址
// @Description 为容器分配IP地址，通过version参数区分IPv4/IPv6
// @Tags Container API - IP管理
// @Accept json
// @Produce json
// @Param version query string true "IP版本: v4 或 v6"
// @Param request body object{count=int} false "分配数量"
// @Success 200 {object} response.Response "分配成功"
// @Failure 400 {object} response.Response "参数错误"
// @Failure 500 {object} response.Response "分配失败"
// @Security ContainerAuth
// @Router /api/container/ip/allocate [post]
func AllocateIP(c *gin.Context) {
	containerName, _ := c.Get("container_name")
	name := containerName.(string)

	version := c.Query("version")
	if version != "v4" && version != "v6" {
		response.Error(c, 400, "version参数必须是v4或v6")
		return
	}

	var req struct {
		Count int `json:"count"`
	}
	c.ShouldBindJSON(&req)
	if req.Count <= 0 {
		req.Count = 1
	}

	var container models.Container
	if err := db.DB.Where("name = ?", name).First(&container).Error; err != nil {
		response.Error(c, 404, "容器不存在")
		return
	}

	if deny_code, deny_msg := service.CheckContainerOperationAllowed(&container, false, "allocate_ip"); deny_code != 0 {
		response.Error(c, deny_code, deny_msg)
		return
	}

	ctx := context.Background()

	if version == "v4" {
		ipv4Svc := service.NewIPv4Service()
		ips, err := ipv4Svc.AllocateIPv4(ctx, name, container.UserID, req.Count)
		if err != nil {
			logger.Error("分配IPv4失败: %v", err)
			response.Error(c, 500, err.Error())
			return
		}
		logger.OK("容器 %s 分配IPv4成功: %v", name, ips)
		response.Success(c, gin.H{
			"container": name,
			"ipv4":      ips,
			"count":     len(ips),
		})
	} else {
		ipv6Svc := service.NewIPv6Service()
		ips, err := ipv6Svc.AllocateIPv6(ctx, name, container.UserID, req.Count)
		if err != nil {
			logger.Error("分配IPv6失败: %v", err)
			response.Error(c, 500, err.Error())
			return
		}
		logger.OK("容器 %s 分配IPv6成功: %v", name, ips)
		response.Success(c, gin.H{
			"container": name,
			"ipv6":      ips,
			"count":     len(ips),
		})
	}
}

// ReleaseIP 释放IP地址（统一接口）
// @Summary 释放IP地址
// @Description 释放容器的IP地址，通过version参数区分IPv4/IPv6
// @Tags Container API - IP管理
// @Accept json
// @Produce json
// @Param version query string true "IP版本: v4 或 v6"
// @Param request body object{ip=string} true "要释放的IP地址"
// @Success 200 {object} response.Response "释放成功"
// @Failure 400 {object} response.Response "参数错误"
// @Failure 500 {object} response.Response "释放失败"
// @Security ContainerAuth
// @Router /api/container/ip/release [post]
func ReleaseIP(c *gin.Context) {
	containerName, _ := c.Get("container_name")
	name := containerName.(string)

	version := c.Query("version")
	if version != "v4" && version != "v6" {
		response.Error(c, 400, "version参数必须是v4或v6")
		return
	}

	var settings models.IPPoolSettings
	db.DB.First(&settings)
	
	if version == "v4" && !settings.AllowContainerReleaseIPv4 {
		response.Error(c, 403, "管理员未开放容器释放IPv4权限")
		return
	}
	if version == "v6" && !settings.AllowContainerReleaseIPv6 {
		response.Error(c, 403, "管理员未开放容器释放IPv6权限")
		return
	}

	if deny_code, deny_msg := service.CheckContainerOperationByName(name, false, "release_ip"); deny_code != 0 {
		response.Error(c, deny_code, deny_msg)
		return
	}

	var req struct {
		IP string `json:"ip" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "缺少ip参数")
		return
	}

	if version == "v4" {
		ipv4Svc := service.NewIPv4Service()
		if err := ipv4Svc.ReleaseIPv4(name, req.IP); err != nil {
			logger.Error("释放IPv4失败: %v", err)
			response.Error(c, 500, err.Error())
			return
		}
		logger.OK("容器 %s 释放IPv4成功: %s", name, req.IP)
		response.Success(c, gin.H{
			"container": name,
			"ip":        req.IP,
			"message":   "IPv4释放成功",
		})
	} else {
		ipv6Svc := service.NewIPv6Service()
		if err := ipv6Svc.ReleaseIPv6(name, req.IP); err != nil {
			logger.Error("释放IPv6失败: %v", err)
			response.Error(c, 500, err.Error())
			return
		}
		logger.OK("容器 %s 释放IPv6成功: %s", name, req.IP)
		response.Success(c, gin.H{
			"container": name,
			"ip":        req.IP,
			"message":   "IPv6释放成功",
		})
	}
}

func GetDNS(c *gin.Context) {
	name, _ := c.Get("container_name")
	containerName := name.(string)
	
	ctx := context.Background()
	lxcClient := service.NewContainerService().GetLXCClient()
	dnsServers, err := lxcClient.GetContainerDNS(ctx, containerName)
	if err != nil {
		response.Error(c, 500, err.Error())
		return
	}
	
	response.Success(c, gin.H{
		"container": containerName,
		"dns":       dnsServers,
	})
}

func SetDNS(c *gin.Context) {
	name, _ := c.Get("container_name")
	containerName := name.(string)

	if deny_code, deny_msg := service.CheckContainerOperationByName(containerName, false, "set_dns"); deny_code != 0 {
		response.Error(c, deny_code, deny_msg)
		return
	}
	
	var req struct {
		DNS []string `json:"dns" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}
	
	if len(req.DNS) == 0 {
		response.Error(c, 400, "DNS服务器列表不能为空")
		return
	}
	
	ctx := context.Background()
	lxcClient := service.NewContainerService().GetLXCClient()
	
	if err := lxcClient.SetContainerDNS(ctx, containerName, req.DNS); err != nil {
		response.Error(c, 500, err.Error())
		return
	}
	
	logger.OK("容器 %s DNS设置成功: %v", containerName, req.DNS)
	response.Success(c, gin.H{
		"container": containerName,
		"dns":       req.DNS,
		"message":   "DNS设置成功",
	})
}
