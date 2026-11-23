package admin

import (
	"context"
	"github.com/gin-gonic/gin"
	"lxdapi/internal/db"
	"lxdapi/internal/executor"
	"lxdapi/internal/service"
	"lxdapi/internal/traffic"
	"lxdapi/models"
	"lxdapi/pkg/logger"
	"lxdapi/pkg/response"
)

// ListContainers 获取容器列表
// @Summary 获取容器列表
// @Description 获取容器列表，可按用户筛选
// @Tags Admin API - 容器管理
// @Accept json
// @Produce json
// @Param user_id query string false "用户ID"
// @Success 200 {object} response.Response "获取成功"
// @Failure 500 {object} response.Response "获取失败"
// @Security SessionAuth
// @Router /api/admin/containers [get]
func ListContainers(c *gin.Context) {
	userID := c.Query("user_id")
	
	containers, err := service.ListContainersByUser(userID)
	if err != nil {
		logger.Error("获取容器列表失败: %v", err)
		response.Error(c, 500, err.Error())
		return
	}

	response.Success(c, gin.H{
		"containers": containers,
		"total":      len(containers),
	})
}

// GetContainer 获取容器详情
// @Summary 获取容器详情
// @Description 获取指定容器的详细信息
// @Tags Admin API - 容器管理
// @Accept json
// @Produce json
// @Param name path string true "容器名称"
// @Success 200 {object} response.Response "获取成功"
// @Failure 400 {object} response.Response "缺少参数"
// @Failure 404 {object} response.Response "容器不存在"
// @Security SessionAuth
// @Router /api/admin/containers/:name [get]
func GetContainer(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		response.Error(c, 400, "缺少容器名称")
		return
	}

	svc := service.NewContainerService()
	_, err := svc.Get(name)
	if err != nil {
		response.Error(c, 404, "容器不存在")
		return
	}

	ctx := c.Request.Context()
	info, err := svc.GetInfo(ctx, name)
	if err != nil {
		response.Error(c, 500, "获取容器信息失败: "+err.Error())
		return
	}

	response.Success(c, info)
}

// DeleteContainer 删除容器
// @Summary 删除容器
// @Description 删除指定容器
// @Tags Admin API - 容器管理
// @Accept json
// @Produce json
// @Param name path string true "容器名称"
// @Success 200 {object} response.Response "删除任务已创建"
// @Failure 400 {object} response.Response "缺少参数"
// @Failure 404 {object} response.Response "容器不存在"
// @Failure 500 {object} response.Response "删除失败"
// @Security SessionAuth
// @Router /api/admin/containers/:name [delete]
func DeleteContainer(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		response.Error(c, 400, "缺少容器名称")
		return
	}

	svc := service.NewContainerService()
	container, err := svc.Get(name)
	if err != nil {
		response.Error(c, 404, "容器不存在")
		return
	}

	params := map[string]interface{}{
		"container_name": name,
		"user_id":        container.UserID,
	}

	task, err := executor.CreateTask(name, "delete", container.UserID, params, func(ctx context.Context) error {
		return svc.Delete(ctx, name)
	})
	if err != nil {
		response.Error(c, 500, "创建任务失败: "+err.Error())
		return
	}

	logger.OK("容器删除任务已创建: %s, 任务ID: %d", name, task.ID)
	response.Success(c, gin.H{
		"task_id": task.ID,
		"message": "删除任务已创建，正在后台执行",
	})
}

// ContainerAction 容器操作统一接口
// @Summary 容器操作
// @Description 对容器执行操作: start/stop/restart/pause/resume/reinstall/reset-password/reset-traffic
// @Tags Admin API - 容器管理
// @Accept json
// @Produce json
// @Param name path string true "容器名称"
// @Param action query string true "操作类型"
// @Param request body object false "操作参数"
// @Success 200 {object} response.Response "操作成功"
// @Failure 400 {object} response.Response "参数错误"
// @Failure 404 {object} response.Response "容器不存在"
// @Failure 500 {object} response.Response "操作失败"
// @Security SessionAuth
// @Router /api/admin/containers/:name/action [post]
func ContainerAction(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		response.Error(c, 400, "缺少容器名称")
		return
	}

	action := c.Query("action")
	if action == "" {
		response.Error(c, 400, "缺少操作类型")
		return
	}

	svc := service.NewContainerService()
	container, err := svc.Get(name)
	if err != nil {
		response.Error(c, 404, "容器不存在")
		return
	}

	userID := container.UserID

	switch action {
	case "start":
		if err := executor.CreateSyncTask(name, "start", userID, func(ctx context.Context) error {
			return svc.Start(ctx, name)
		}); err != nil {
			response.Error(c, 500, err.Error())
			return
		}
		logger.OK("容器启动成功: %s", name)
		response.Success(c, "容器启动成功")

	case "stop":
		if err := executor.CreateSyncTask(name, "stop", userID, func(ctx context.Context) error {
			return svc.Stop(ctx, name)
		}); err != nil {
			response.Error(c, 500, err.Error())
			return
		}
		logger.OK("容器停止成功: %s", name)
		response.Success(c, "容器停止成功")

	case "restart":
		if err := executor.CreateSyncTask(name, "restart", userID, func(ctx context.Context) error {
			return svc.Restart(ctx, name)
		}); err != nil {
			response.Error(c, 500, err.Error())
			return
		}
		logger.OK("容器重启成功: %s", name)
		response.Success(c, "容器重启成功")

	case "pause":
		if err := executor.CreateSyncTask(name, "pause", userID, func(ctx context.Context) error {
			return svc.PauseWithSource(ctx, name, "admin")
		}); err != nil {
			response.Error(c, 500, err.Error())
			return
		}
		logger.OK("容器暂停成功: %s", name)
		response.Success(c, "容器暂停成功")

	case "resume":
		if err := executor.CreateSyncTask(name, "resume", userID, func(ctx context.Context) error {
			return svc.Resume(ctx, name)
		}); err != nil {
			response.Error(c, 500, err.Error())
			return
		}
		logger.OK("容器恢复成功: %s", name)
		response.Success(c, "容器恢复成功")

	case "reinstall":
		var req struct {
			Image    string `json:"image"`
			Password string `json:"password"`
		}
		c.ShouldBindJSON(&req)

		finalImage := req.Image
		if finalImage == "" {
			finalImage = container.Image
		}

		params := map[string]interface{}{
			"container_name": name,
			"original_image": container.Image,
			"new_image":      finalImage,
			"password_set":   req.Password != "",
			"user_id":        userID,
			"image":          finalImage,
			"password":       req.Password,
		}

		task, err := executor.CreateTask(name, "reinstall", userID, params, func(ctx context.Context) error {
			return svc.Reinstall(ctx, name, req.Image, req.Password)
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

		if err := executor.CreateSyncTask(name, "reset_password", userID, func(ctx context.Context) error {
			return svc.ResetPassword(ctx, name, req.Password)
		}); err != nil {
			response.Error(c, 500, err.Error())
			return
		}
		logger.OK("容器密码重置成功: %s", name)
		response.Success(c, "密码重置成功")

	case "reset-traffic":
		if traffic.GlobalMonitor == nil {
			response.Error(c, 500, "流量监控未启用")
			return
		}
		if err := traffic.GlobalMonitor.ResetTraffic(name); err != nil {
			response.Error(c, 500, err.Error())
			return
		}
		logger.OK("容器流量已重置: %s", name)
		response.Success(c, "流量已重置")

	default:
		response.Error(c, 400, "不支持的操作类型: "+action)
	}
}

// GetContainerCredential 获取容器凭证
// @Summary 获取容器凭证
// @Description 获取或创建容器访问凭证
// @Tags Admin API - 容器管理
// @Accept json
// @Produce json
// @Param name path string true "容器名称"
// @Success 200 {object} response.Response "获取成功"
// @Failure 400 {object} response.Response "缺少参数"
// @Failure 500 {object} response.Response "创建失败"
// @Security SessionAuth
// @Router /api/admin/containers/:name/credential [get]
func GetContainerCredential(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		response.Error(c, 400, "缺少容器名称")
		return
	}

	cred, err := service.GetContainerCredential(name)
	if err != nil {
		cred, err = service.CreateContainerCredential(name)
		if err != nil {
			logger.Error("创建容器凭证失败: %v", err)
			response.Error(c, 500, err.Error())
			return
		}
	}

	response.Success(c, gin.H{
		"container_name": cred.ContainerName,
		"hash":           cred.Hash,
	})
}

// RegenerateContainerCredential 重新生成容器凭证
// @Summary 重新生成容器凭证
// @Description 重新生成容器访问Hash
// @Tags Admin API - 容器管理
// @Accept json
// @Produce json
// @Param name path string true "容器名称"
// @Success 200 {object} response.Response "生成成功"
// @Failure 400 {object} response.Response "缺少参数"
// @Failure 500 {object} response.Response "生成失败"
// @Security SessionAuth
// @Router /api/admin/containers/:name/credential [post]
func RegenerateContainerCredential(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		response.Error(c, 400, "缺少容器名称")
		return
	}

	cred, err := service.RegenerateContainerHash(name)
	if err != nil {
		logger.Error("重新生成容器Hash失败: %v", err)
		response.Error(c, 500, err.Error())
		return
	}

	logger.OK("重新生成容器Hash成功: %s", name)
	response.Success(c, gin.H{
		"container_name": cred.ContainerName,
		"hash":           cred.Hash,
	})
}

// GetContainerConfig 获取容器配置
// @Summary 获取容器配置
// @Description 获取容器的资源配置信息（仅管理员可用）
// @Tags Admin API - 容器管理
// @Accept json
// @Produce json
// @Param name path string true "容器名称"
// @Success 200 {object} response.Response "获取成功"
// @Failure 400 {object} response.Response "缺少参数"
// @Failure 404 {object} response.Response "容器不存在"
// @Security SessionAuth
// @Router /api/admin/containers/:name/config [get]
func GetContainerConfig(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		response.Error(c, 400, "缺少容器名称")
		return
	}

	svc := service.NewContainerService()
	config, err := svc.GetConfig(name)
	if err != nil {
		response.Error(c, 404, err.Error())
		return
	}

	response.Success(c, config)
}

// UpdateContainerConfig 更新容器配置
// @Summary 更新容器配置
// @Description 热更新容器配置，支持CPU、内存、磁盘、带宽等升降级，磁盘只能扩容
// @Tags Admin API - 容器管理
// @Accept json
// @Produce json
// @Param name path string true "容器名称"
// @Param request body models.UpdateContainerConfigRequest true "配置更新参数"
// @Success 200 {object} response.Response "更新成功"
// @Failure 400 {object} response.Response "参数错误"
// @Failure 404 {object} response.Response "容器不存在"
// @Failure 500 {object} response.Response "更新失败"
// @Security SessionAuth
// @Router /api/admin/containers/:name/config [put]
func UpdateContainerConfig(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		response.Error(c, 400, "缺少容器名称")
		return
	}

	svc := service.NewContainerService()
	container, err := svc.Get(name)
	if err != nil {
		response.Error(c, 404, "容器不存在")
		return
	}

	var req models.UpdateContainerConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误: "+err.Error())
		return
	}

	params := map[string]interface{}{
		"container_name": name,
		"user_id":        container.UserID,
		"config":         req,
	}

	task, err := executor.CreateTask(name, "update_config", container.UserID, params, func(ctx context.Context) error {
		_, err := svc.UpdateConfig(ctx, name, &req)
		return err
	})
	if err != nil {
		response.Error(c, 500, "创建任务失败: "+err.Error())
		return
	}

	logger.OK("容器 %s 配置更新任务已创建, 任务ID: %d", name, task.ID)
	response.Success(c, gin.H{
		"name":    name,
		"task_id": task.ID,
		"message": "配置更新任务已提交",
	})
}

// GetContainerIP 获取容器IP
// @Summary 获取容器IP
// @Description 获取容器的IP地址
// @Tags Admin API - 容器管理
// @Accept json
// @Produce json
// @Param name path string true "容器名称"
// @Param version query string false "IP版本: v4/v6/all，默认all"
// @Success 200 {object} response.Response "获取成功"
// @Failure 400 {object} response.Response "缺少参数"
// @Failure 500 {object} response.Response "获取失败"
// @Security SessionAuth
// @Router /api/admin/containers/:name/ip [get]
func GetContainerIP(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		response.Error(c, 400, "缺少容器名称")
		return
	}

	version := c.DefaultQuery("version", "all")
	result := gin.H{"container": name}

	if version == "v4" || version == "all" {
		var bindings []models.IPv4Binding
		db.DB.Where("container_name = ?", name).Find(&bindings)
		result["ipv4"] = bindings
		result["ipv4_count"] = len(bindings)
	}

	if version == "v6" || version == "all" {
		var bindings []models.IPv6Binding
		db.DB.Where("container_name = ?", name).Find(&bindings)
		result["ipv6"] = bindings
		result["ipv6_count"] = len(bindings)
	}

	response.Success(c, result)
}

// AllocateContainerIP 分配容器IP
// @Summary 分配容器IP
// @Description 为容器分配IP地址
// @Tags Admin API - 容器管理
// @Accept json
// @Produce json
// @Param name path string true "容器名称"
// @Param version query string true "IP版本: v4 或 v6"
// @Param request body object{count=int} false "分配数量"
// @Success 200 {object} response.Response "分配成功"
// @Failure 400 {object} response.Response "参数错误"
// @Failure 404 {object} response.Response "容器不存在"
// @Failure 500 {object} response.Response "分配失败"
// @Security SessionAuth
// @Router /api/admin/containers/:name/ip/allocate [post]
func AllocateContainerIP(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		response.Error(c, 400, "缺少容器名称")
		return
	}

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

func GetContainerDNS(c *gin.Context) {
	name := c.Param("name")
	ctx := context.Background()
	
	lxcClient := service.NewContainerService().GetLXCClient()
	dnsServers, err := lxcClient.GetContainerDNS(ctx, name)
	if err != nil {
		response.Error(c, 500, err.Error())
		return
	}
	
	response.Success(c, gin.H{
		"container": name,
		"dns":       dnsServers,
	})
}

func SetContainerDNS(c *gin.Context) {
	name := c.Param("name")
	
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
	
	if err := lxcClient.SetContainerDNS(ctx, name, req.DNS); err != nil {
		response.Error(c, 500, err.Error())
		return
	}
	
	logger.OK("容器 %s DNS设置成功: %v", name, req.DNS)
	response.Success(c, gin.H{
		"container": name,
		"dns":       req.DNS,
		"message":   "DNS设置成功",
	})
}

func UpdateContainerRemark(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		response.Error(c, 400, "缺少容器名称")
		return
	}

	var req struct {
		Remark string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}

	if len(req.Remark) > 20 {
		response.Error(c, 400, "备注长度不能超过20个字符")
		return
	}

	var container models.Container
	if err := db.DB.Where("name = ?", name).First(&container).Error; err != nil {
		response.Error(c, 404, "容器不存在")
		return
	}

	if err := db.DB.Model(&container).Update("remark", req.Remark).Error; err != nil {
		response.Error(c, 500, "更新备注失败")
		return
	}

	logger.OK("容器 %s 备注已更新: %s", name, req.Remark)
	response.Success(c, gin.H{
		"container": name,
		"remark":    req.Remark,
		"message":   "备注更新成功",
	})
}
