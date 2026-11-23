package system

import (
	"context"
	"github.com/gin-gonic/gin"
	"lxdapi/internal/executor"
	"lxdapi/internal/lxc"
	"lxdapi/internal/service"
	"lxdapi/models"
	"lxdapi/pkg/auth"
	"lxdapi/pkg/response"
)

var containerService *service.ContainerService

func InitContainerService(svc *service.ContainerService) {
	containerService = svc
}

func isAdminAuthLevel(c *gin.Context) bool {
	authLevelVal, exists := c.Get("auth_level")
	if !exists {
		return false
	}
	authLevel, ok := authLevelVal.(auth.APILevel)
	return ok && authLevel == auth.LevelAdmin
}

// CreateContainer 创建容器
func CreateContainer(c *gin.Context) {
	var req models.CreateContainerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误: "+err.Error())
		return
	}

	if err := lxc.ValidateContainerName(req.Name); err != nil {
		response.Error(c, 400, err.Error())
		return
	}
	if err := lxc.ValidateImageName(req.Image); err != nil {
		response.Error(c, 400, err.Error())
		return
	}
	if req.CPU < 0 || req.Memory < 0 || req.Disk < 0 || req.Ingress < 0 || req.Egress < 0 ||
		req.TrafficLimit < 0 || req.IPv4PoolLimit < 0 || req.IPv4MappingLimit < 0 ||
		req.IPv6PoolLimit < 0 || req.IPv6MappingLimit < 0 || req.ReverseProxyLimit < 0 ||
		req.CPUAllowance < 0 || req.IORead < 0 || req.IOWrite < 0 || req.ProcessesLimit < 0 {
		response.Error(c, 400, "资源与限制参数不能为负数")
		return
	}

	isAdmin := isAdminAuthLevel(c)
	if !isAdmin && (req.Privileged || req.AllowNesting) {
		response.Error(c, 403, "系统接口禁止开启特权或嵌套容器")
		return
	}
	if !isAdmin {
		req.Privileged = false
		req.AllowNesting = false
	}

	params := map[string]interface{}{
		"name":                req.Name,
		"image":               req.Image,
		"cpu":                 req.CPU,
		"memory":              req.Memory,
		"disk":                req.Disk,
		"ingress":             req.Ingress,
		"egress":              req.Egress,
		"traffic_limit":       req.TrafficLimit,
		"allow_nesting":       req.AllowNesting,
		"memory_swap":         req.MemorySwap,
		"privileged":          req.Privileged,
		"username":            req.Username,
		"ipv4_pool_limit":     req.IPv4PoolLimit,
		"ipv4_mapping_limit":  req.IPv4MappingLimit,
		"ipv6_pool_limit":     req.IPv6PoolLimit,
		"ipv6_mapping_limit":  req.IPv6MappingLimit,
		"reverse_proxy_limit": req.ReverseProxyLimit,
		"password":            req.Password,
		"cpu_allowance":       req.CPUAllowance,
		"io_read":             req.IORead,
		"io_write":            req.IOWrite,
		"processes_limit":     req.ProcessesLimit,
	}

	task, err := executor.CreateTask(req.Name, "create", req.Username, params, func(ctx context.Context) error {
		return containerService.Create(ctx, &req)
	})

	if err != nil {
		response.Error(c, 500, "创建任务失败: "+err.Error())
		return
	}

	response.Success(c, gin.H{
		"name":    req.Name,
		"task_id": task.ID,
		"message": "容器创建任务已提交，正在后台执行",
	})
}

// ListContainers 获取容器列表
func ListContainers(c *gin.Context) {
	containers, err := containerService.List("")
	if err != nil {
		response.Error(c, 500, err.Error())
		return
	}

	response.Success(c, containers)
}

// GetContainer 获取容器详情（包含状态）
func GetContainer(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		response.Error(c, 400, "缺少容器名称")
		return
	}

	container, err := containerService.Get(name)
	if err != nil {
		response.Error(c, 404, "容器不存在")
		return
	}

	ctx := c.Request.Context()
	status, _ := containerService.GetStatus(ctx, name)

	response.Success(c, gin.H{
		"container": container,
		"status":    status,
	})
}

// DeleteContainer 删除容器
func DeleteContainer(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		response.Error(c, 400, "缺少容器名称")
		return
	}

	container, err := containerService.Get(name)
	if err != nil {
		response.Error(c, 404, "容器不存在")
		return
	}

	params := map[string]interface{}{
		"container_name": name,
		"user_id":        container.UserID,
		"image":          container.Image,
		"status":         container.Status,
		"created_at":     container.CreatedAt,
	}

	task, err := executor.CreateTask(name, "delete", container.UserID, params, func(ctx context.Context) error {
		return containerService.Delete(ctx, name)
	})

	if err != nil {
		response.Error(c, 500, "创建任务失败: "+err.Error())
		return
	}

	response.Success(c, gin.H{
		"task_id": task.ID,
		"message": "容器删除任务已提交，正在后台执行",
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

	container, err := containerService.Get(name)
	if err != nil {
		response.Error(c, 404, "容器不存在")
		return
	}

	userID := container.UserID

	switch action {
	case "start":
		err := executor.CreateSyncTask(name, "start", userID, func(ctx context.Context) error {
			return containerService.Start(ctx, name)
		})
		if err != nil {
			response.Error(c, 500, err.Error())
			return
		}
		response.Success(c, "容器启动成功")

	case "stop":
		err := executor.CreateSyncTask(name, "stop", userID, func(ctx context.Context) error {
			return containerService.Stop(ctx, name)
		})
		if err != nil {
			response.Error(c, 500, err.Error())
			return
		}
		response.Success(c, "容器停止成功")

	case "restart":
		err := executor.CreateSyncTask(name, "restart", userID, func(ctx context.Context) error {
			return containerService.Restart(ctx, name)
		})
		if err != nil {
			response.Error(c, 500, err.Error())
			return
		}
		response.Success(c, "容器重启成功")

	case "pause":
		err := executor.CreateSyncTask(name, "pause", userID, func(ctx context.Context) error {
			return containerService.PauseWithSource(ctx, name, "system")
		})
		if err != nil {
			response.Error(c, 500, err.Error())
			return
		}
		response.Success(c, "容器暂停成功")

	case "resume":
		err := executor.CreateSyncTask(name, "resume", userID, func(ctx context.Context) error {
			return containerService.Resume(ctx, name)
		})
		if err != nil {
			response.Error(c, 500, err.Error())
			return
		}
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
			return containerService.Reinstall(ctx, name, req.Image, req.Password)
		})
		if err != nil {
			response.Error(c, 500, "创建任务失败: "+err.Error())
			return
		}
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

		err := executor.CreateSyncTask(name, "reset_password", userID, func(ctx context.Context) error {
			return containerService.ResetPassword(ctx, name, req.Password)
		})
		if err != nil {
			response.Error(c, 500, err.Error())
			return
		}
		response.Success(c, "密码重置成功")

	default:
		response.Error(c, 400, "不支持的操作类型: "+action)
	}
}

// UpdateContainerConfig 更新容器配置
func UpdateContainerConfig(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		response.Error(c, 400, "缺少容器名称")
		return
	}

	container, err := containerService.Get(name)
	if err != nil {
		response.Error(c, 404, "容器不存在")
		return
	}

	var req models.UpdateContainerConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误: "+err.Error())
		return
	}

	if (req.CPU != nil && *req.CPU < 0) ||
		(req.Memory != nil && *req.Memory < 0) ||
		(req.Disk != nil && *req.Disk < 0) ||
		(req.Ingress != nil && *req.Ingress < 0) ||
		(req.Egress != nil && *req.Egress < 0) ||
		(req.CPUAllowance != nil && *req.CPUAllowance < 0) ||
		(req.IORead != nil && *req.IORead < 0) ||
		(req.IOWrite != nil && *req.IOWrite < 0) ||
		(req.ProcessesLimit != nil && *req.ProcessesLimit < 0) ||
		(req.TrafficLimit != nil && *req.TrafficLimit < 0) ||
		(req.IPv4PoolLimit != nil && *req.IPv4PoolLimit < 0) ||
		(req.IPv4MappingLimit != nil && *req.IPv4MappingLimit < 0) ||
		(req.IPv6PoolLimit != nil && *req.IPv6PoolLimit < 0) ||
		(req.IPv6MappingLimit != nil && *req.IPv6MappingLimit < 0) ||
		(req.ReverseProxyLimit != nil && *req.ReverseProxyLimit < 0) {
		response.Error(c, 400, "资源与限制参数不能为负数")
		return
	}

	if !isAdminAuthLevel(c) && (req.Privileged != nil || req.AllowNesting != nil) {
		response.Error(c, 403, "系统接口禁止修改特权或嵌套容器配置")
		return
	}

	params := map[string]interface{}{
		"container_name": name,
		"user_id":        container.UserID,
		"config":         req,
	}

	task, err := executor.CreateTask(name, "update_config", container.UserID, params, func(ctx context.Context) error {
		_, err := containerService.UpdateConfig(ctx, name, &req)
		return err
	})
	if err != nil {
		response.Error(c, 500, "创建任务失败: "+err.Error())
		return
	}

	response.Success(c, gin.H{
		"name":    name,
		"task_id": task.ID,
		"message": "配置更新任务已提交",
	})
}
