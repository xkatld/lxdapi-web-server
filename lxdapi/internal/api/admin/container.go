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
func ListContainers(c *gin.Context) {
	userID := c.Query("user_id")
	
	containers, err := service.ListContainersByUser(userID)
	if err != nil {
		logger.Error("获取容器列表失败: %v", err)
		response.Error(c, 500, "获取容器列表失败")
		return
	}

	response.Success(c, gin.H{
		"containers": containers,
		"total":      len(containers),
	})
}

// GetContainer 获取容器详情
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
		logger.Error("获取容器信息失败: %v", err)
		response.Error(c, 500, "获取容器信息失败")
		return
	}

	response.Success(c, info)
}

// DeleteContainer 删除容器
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
		logger.Error("创建删除容器任务失败: %v", err)
		response.Error(c, 500, "创建任务失败")
		return
	}

	logger.OK("容器删除任务已创建: %s, 任务ID: %d", name, task.ID)
	response.Success(c, gin.H{
		"task_id": task.ID,
		"message": "删除任务已创建，正在后台执行",
	})
}

// ContainerAction 容器操作统一接口
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
			logger.Error("容器启动失败: %v", err)
			response.Error(c, 500, "容器启动失败")
			return
		}
		logger.OK("容器启动成功: %s", name)
		response.Success(c, "容器启动成功")

	case "stop":
		if err := executor.CreateSyncTask(name, "stop", userID, func(ctx context.Context) error {
			return svc.Stop(ctx, name)
		}); err != nil {
			logger.Error("容器停止失败: %v", err)
			response.Error(c, 500, "容器停止失败")
			return
		}
		logger.OK("容器停止成功: %s", name)
		response.Success(c, "容器停止成功")

	case "restart":
		if err := executor.CreateSyncTask(name, "restart", userID, func(ctx context.Context) error {
			return svc.Restart(ctx, name)
		}); err != nil {
			logger.Error("容器重启失败: %v", err)
			response.Error(c, 500, "容器重启失败")
			return
		}
		logger.OK("容器重启成功: %s", name)
		response.Success(c, "容器重启成功")

	case "pause":
		if err := executor.CreateSyncTask(name, "pause", userID, func(ctx context.Context) error {
			return svc.PauseWithSource(ctx, name, "admin")
		}); err != nil {
			logger.Error("容器暂停失败: %v", err)
			response.Error(c, 500, "容器暂停失败")
			return
		}
		logger.OK("容器暂停成功: %s", name)
		response.Success(c, "容器暂停成功")

	case "resume":
		if err := executor.CreateSyncTask(name, "resume", userID, func(ctx context.Context) error {
			return svc.Resume(ctx, name)
		}); err != nil {
			logger.Error("容器恢复失败: %v", err)
			response.Error(c, 500, "容器恢复失败")
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
			logger.Error("创建重装任务失败: %v", err)
			response.Error(c, 500, "创建任务失败")
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
			logger.Error("容器密码重置失败: %v", err)
			response.Error(c, 500, "容器密码重置失败")
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
			logger.Error("重置容器流量失败: %v", err)
			response.Error(c, 500, "重置容器流量失败")
			return
		}
		logger.OK("容器流量已重置: %s", name)
		response.Success(c, "流量已重置")

	default:
		response.Error(c, 400, "不支持的操作类型: "+action)
	}
}

// GetContainerCredential 获取容器凭证
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
			response.Error(c, 500, "创建容器凭证失败")
			return
		}
	}

	response.Success(c, gin.H{
		"container_name": cred.ContainerName,
		"hash":           cred.Hash,
	})
}

// RegenerateContainerCredential 重新生成容器凭证
func RegenerateContainerCredential(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		response.Error(c, 400, "缺少容器名称")
		return
	}

	cred, err := service.RegenerateContainerHash(name)
	if err != nil {
		logger.Error("重新生成容器Hash失败: %v", err)
		response.Error(c, 500, "重新生成容器凭证失败")
		return
	}

	logger.OK("重新生成容器Hash成功: %s", name)
	response.Success(c, gin.H{
		"container_name": cred.ContainerName,
		"hash":           cred.Hash,
	})
}

// GetContainerConfig 获取容器配置
func GetContainerConfig(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		response.Error(c, 400, "缺少容器名称")
		return
	}

	svc := service.NewContainerService()
	config, err := svc.GetConfig(name)
	if err != nil {
		logger.Error("获取容器配置失败: %v", err)
		response.Error(c, 404, "获取容器配置失败")
		return
	}

	response.Success(c, config)
}

// UpdateContainerConfig 更新容器配置
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
		response.Error(c, 400, "参数错误")
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
		logger.Error("创建删除容器任务失败: %v", err)
		response.Error(c, 500, "创建任务失败")
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
			response.Error(c, 500, "分配IPv4失败")
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
			response.Error(c, 500, "分配IPv6失败")
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
		logger.Error("获取容器DNS失败: %v", err)
		response.Error(c, 500, "获取容器DNS失败")
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
		logger.Error("设置容器DNS失败: %v", err)
		response.Error(c, 500, "设置容器DNS失败")
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
