package user

import (
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"lxdapi/internal/cache"
	"lxdapi/internal/db"
	"lxdapi/internal/executor"
	"lxdapi/internal/service"
	"lxdapi/internal/traffic"
	"lxdapi/models"
	"lxdapi/pkg/logger"
	"lxdapi/pkg/response"
)

var containerService *service.ContainerService

func InitContainerService(svc *service.ContainerService) {
	containerService = svc
}

// UserCreateContainerRequest 用户创建容器请求（只有9个可见参数）
type UserCreateContainerRequest struct {
	Name              string `json:"name" binding:"required"`
	Password          string `json:"password"`
	Image             string `json:"image" binding:"required"`
	CPU               int    `json:"cpu"`
	Memory            int    `json:"memory"`
	Disk              int    `json:"disk"`
	TrafficLimit      int    `json:"traffic_limit"`
	IPv4PoolLimit     int    `json:"ipv4_pool_limit"`
	IPv4MappingLimit  int    `json:"ipv4_mapping_limit"`
	IPv6PoolLimit     int    `json:"ipv6_pool_limit"`
	IPv6MappingLimit  int    `json:"ipv6_mapping_limit"`
	ReverseProxyLimit int    `json:"reverse_proxy_limit"`
}

func CreateContainer(c *gin.Context) {
	username, _ := c.Get("username")
	
	user, err := service.GetUserByUsername(username.(string))
	if err != nil {
		response.Error(c, 500, "获取用户信息失败")
		return
	}

	var req UserCreateContainerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误: "+err.Error())
		return
	}

	if req.CPU < 0 || req.Memory < 0 || req.Disk < 0 || req.TrafficLimit < 0 ||
		req.IPv4PoolLimit < 0 || req.IPv4MappingLimit < 0 || req.IPv6PoolLimit < 0 ||
		req.IPv6MappingLimit < 0 || req.ReverseProxyLimit < 0 {
		response.Error(c, 400, "资源与限制参数不能为负数")
		return
	}

	if code, msg := service.CheckTemplateAccess(req.Image, username.(string)); code != 0 {
		response.Error(c, code, msg)
		return
	}

	if user.MaxCPUPerContainer > 0 && req.CPU > user.MaxCPUPerContainer {
		response.Error(c, 400, fmt.Sprintf("单容器CPU超过限制，最大允许%d核", user.MaxCPUPerContainer))
		return
	}

	stats := service.GetUserFullStats(username.(string))
	if user.CPUQuota > 0 && stats.UsedCPU+req.CPU > user.CPUQuota {
		response.Error(c, 400, "CPU配额不足")
		return
	}
	if user.MemoryQuota > 0 && stats.UsedMemory+req.Memory > user.MemoryQuota {
		response.Error(c, 400, "内存配额不足")
		return
	}
	if user.DiskQuota > 0 && stats.UsedDisk+req.Disk > user.DiskQuota {
		response.Error(c, 400, "磁盘配额不足")
		return
	}
	usedTrafficGB := float64(stats.UsedTraffic) / (1024 * 1024 * 1024)
	if user.TrafficLimit > 0 && usedTrafficGB+float64(req.TrafficLimit) > float64(user.TrafficLimit) {
		response.Error(c, 400, "流量配额不足")
		return
	}
	if user.IPv4PoolLimit > 0 && stats.UsedIPv4Pool+int64(req.IPv4PoolLimit) > int64(user.IPv4PoolLimit) {
		response.Error(c, 400, "IPv4地址池限制超过配额")
		return
	}
	if user.IPv4MappingLimit > 0 && stats.UsedIPv4Map+int64(req.IPv4MappingLimit) > int64(user.IPv4MappingLimit) {
		response.Error(c, 400, "IPv4端口映射限制超过配额")
		return
	}
	if user.IPv6PoolLimit > 0 && stats.UsedIPv6Pool+int64(req.IPv6PoolLimit) > int64(user.IPv6PoolLimit) {
		response.Error(c, 400, "IPv6地址池限制超过配额")
		return
	}
	if user.IPv6MappingLimit > 0 && stats.UsedIPv6Map+int64(req.IPv6MappingLimit) > int64(user.IPv6MappingLimit) {
		response.Error(c, 400, "IPv6端口映射限制超过配额")
		return
	}
	if user.ReverseProxyLimit > 0 && stats.UsedProxy+int64(req.ReverseProxyLimit) > int64(user.ReverseProxyLimit) {
		response.Error(c, 400, "反向代理限制超过配额")
		return
	}

	createReq := &models.CreateContainerRequest{
		Name:              req.Name,
		Password:          req.Password,
		Image:             req.Image,
		CPU:               req.CPU,
		Memory:            req.Memory,
		Disk:              req.Disk,
		TrafficLimit:      req.TrafficLimit,
		IPv4PoolLimit:     req.IPv4PoolLimit,
		IPv4MappingLimit:  req.IPv4MappingLimit,
		IPv6PoolLimit:     req.IPv6PoolLimit,
		IPv6MappingLimit:  req.IPv6MappingLimit,
		ReverseProxyLimit: req.ReverseProxyLimit,
		Username:          username.(string),
		Privileged:        false,
		Ingress:           user.Ingress,
		Egress:            user.Egress,
		CPUAllowance:      user.CPUAllowance,
		IORead:            user.IORead,
		IOWrite:           user.IOWrite,
		ProcessesLimit:    user.ProcessesLimit,
		AllowNesting:      false,
		MemorySwap:        user.MemorySwap,
	}

	params := map[string]interface{}{
		"name":                createReq.Name,
		"image":               createReq.Image,
		"cpu":                 createReq.CPU,
		"memory":              createReq.Memory,
		"disk":                createReq.Disk,
		"ingress":             createReq.Ingress,
		"egress":              createReq.Egress,
		"traffic_limit":       createReq.TrafficLimit,
		"allow_nesting":       false,
		"memory_swap":         createReq.MemorySwap,
		"privileged":          false,
		"username":            createReq.Username,
		"ipv4_pool_limit":     createReq.IPv4PoolLimit,
		"ipv4_mapping_limit":  createReq.IPv4MappingLimit,
		"ipv6_pool_limit":     createReq.IPv6PoolLimit,
		"ipv6_mapping_limit":  createReq.IPv6MappingLimit,
		"reverse_proxy_limit": createReq.ReverseProxyLimit,
		"password":            createReq.Password,
		"cpu_allowance":       createReq.CPUAllowance,
		"io_read":             createReq.IORead,
		"io_write":            createReq.IOWrite,
		"processes_limit":     createReq.ProcessesLimit,
	}

	task, err := executor.CreateTask(createReq.Name, "create", createReq.Username, params, func(ctx context.Context) error {
		return containerService.Create(ctx, createReq)
	})

	if err != nil {
		logger.Error("创建容器任务失败: %v", err)
		response.Error(c, 500, "创建任务失败: "+err.Error())
		return
	}

	logger.OK("用户 %s 创建容器任务已提交: %s, 任务ID: %d", username, createReq.Name, task.ID)
	response.Success(c, gin.H{
		"name":    createReq.Name,
		"task_id": task.ID,
		"message": "容器创建任务已提交，正在后台执行",
	})
}

func DeleteContainer(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		response.Error(c, 400, "缺少容器名称")
		return
	}

	username, _ := c.Get("username")
	
	container, err := containerService.Get(name)
	if err != nil {
		response.Error(c, 404, "容器不存在")
		return
	}

	if container.UserID != username.(string) {
		response.Error(c, 403, "无权操作该容器")
		return
	}

	if deny_code, deny_msg := service.CheckContainerOperationAllowed(container, true, "delete"); deny_code != 0 {
		response.Error(c, deny_code, deny_msg)
		return
	}

	params := map[string]interface{}{
		"container_name": name,
		"user_id":        container.UserID,
	}

	task, err := executor.CreateTask(name, "delete", container.UserID, params, func(ctx context.Context) error {
		return containerService.Delete(ctx, name)
	})
	if err != nil {
		logger.Error("创建删除任务失败: %v", err)
		response.Error(c, 500, "创建任务失败: "+err.Error())
		return
	}

	logger.OK("容器删除任务已创建: %s, 任务ID: %d", name, task.ID)
	response.Success(c, gin.H{
		"task_id": task.ID,
		"message": "删除任务已创建，正在后台执行",
	})
}

func ListContainers(c *gin.Context) {
	username, _ := c.Get("username")
	
	allContainers := cache.GetAllContainersCache()
	
	userContainers := []cache.ContainerCacheJSON{}
	for _, container := range allContainers {
		dbContainer, err := containerService.Get(container.Name)
		if err == nil && dbContainer.UserID == username.(string) {
			userContainers = append(userContainers, container)
		}
	}

	response.Success(c, gin.H{
		"data":  userContainers,
		"count": len(userContainers),
	})
}

func GetContainer(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		response.Error(c, 400, "缺少容器名称")
		return
	}

	username, _ := c.Get("username")
	
	container, err := containerService.Get(name)
	if err != nil {
		logger.Error("获取容器失败: %v", err)
		response.Error(c, 404, err.Error())
		return
	}

	if container.UserID != username.(string) {
		response.Error(c, 403, "无权访问该容器")
		return
	}

	svc := service.NewContainerService()
	info, err := svc.GetInfo(c.Request.Context(), name)
	if err != nil {
		logger.Warn("获取容器详细信息失败: %v", err)
		response.Error(c, 500, "获取容器信息失败")
		return
	}

	response.Success(c, info)
}

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

	username, _ := c.Get("username")
	
	container, err := containerService.Get(name)
	if err != nil {
		logger.Error("获取容器失败: %v", err)
		response.Error(c, 404, err.Error())
		return
	}

	if container.UserID != username.(string) {
		response.Error(c, 403, "无权操作该容器")
		return
	}

	if deny_code, deny_msg := service.CheckContainerOperationAllowed(container, true, action); deny_code != 0 {
		response.Error(c, deny_code, deny_msg)
		return
	}

	switch action {
	case "start":
		if err := executor.CreateSyncTask(name, "start", container.UserID, func(ctx context.Context) error {
			return containerService.Start(ctx, name)
		}); err != nil {
			logger.Error("启动容器失败: %v", err)
			response.Error(c, 500, err.Error())
			return
		}
		logger.OK("容器启动成功: %s", name)
		response.Success(c, "容器启动成功")

	case "stop":
		if err := executor.CreateSyncTask(name, "stop", container.UserID, func(ctx context.Context) error {
			return containerService.Stop(ctx, name)
		}); err != nil {
			logger.Error("停止容器失败: %v", err)
			response.Error(c, 500, err.Error())
			return
		}
		logger.OK("容器停止成功: %s", name)
		response.Success(c, "容器停止成功")

	case "restart":
		if err := executor.CreateSyncTask(name, "restart", container.UserID, func(ctx context.Context) error {
			return containerService.Restart(ctx, name)
		}); err != nil {
			logger.Error("重启容器失败: %v", err)
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

		finalImage, code, msg := service.ResolveReinstallTemplateAccess(req.Image, container.Image, username.(string))
		if code != 0 {
			response.Error(c, code, msg)
			return
		}

		params := map[string]interface{}{
			"container_name": name,
			"original_image": container.Image,
			"new_image":      finalImage,
			"password_set":   req.Password != "",
			"user_id":        container.UserID,
			"image":          finalImage,
			"password":       req.Password,
		}

		task, err := executor.CreateTask(name, "reinstall", username.(string), params, func(ctx context.Context) error {
			return containerService.Reinstall(ctx, name, finalImage, req.Password)
		})
		if err != nil {
			logger.Error("创建重装任务失败: %v", err)
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

		ctx := context.Background()
		if err := containerService.ResetPassword(ctx, name, req.Password); err != nil {
			logger.Error("重置密码失败: %v", err)
			response.Error(c, 500, err.Error())
			return
		}
		logger.OK("容器密码重置成功: %s", name)
		response.Success(c, "密码重置成功")

	case "suspend":
		if err := executor.CreateSyncTask(name, "suspend", container.UserID, func(ctx context.Context) error {
			return containerService.PauseWithSource(ctx, name, "user")
		}); err != nil {
			logger.Error("暂停容器失败: %v", err)
			response.Error(c, 500, err.Error())
			return
		}
		logger.OK("容器暂停成功: %s", name)
		response.Success(c, "容器暂停成功")

	case "unsuspend":
		if err := executor.CreateSyncTask(name, "unsuspend", container.UserID, func(ctx context.Context) error {
			return containerService.Resume(ctx, name)
		}); err != nil {
			logger.Error("恢复容器失败: %v", err)
			response.Error(c, 500, err.Error())
			return
		}
		logger.OK("容器恢复成功: %s", name)
		response.Success(c, "容器恢复成功")

	case "reset-traffic":
		if traffic.GlobalMonitor == nil {
			response.Error(c, 500, "流量监控未启用")
			return
		}
		if err := traffic.GlobalMonitor.ResetTraffic(name); err != nil {
			response.Error(c, 500, err.Error())
			return
		}
		logger.OK("用户 %s 重置容器流量: %s", username.(string), name)
		response.Success(c, "流量重置成功")

	default:
		response.Error(c, 400, "不支持的操作类型: "+action)
	}
}

func GetContainerConfig(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		response.Error(c, 400, "缺少容器名称")
		return
	}

	username, _ := c.Get("username")

	container, err := containerService.Get(name)
	if err != nil {
		response.Error(c, 404, "容器不存在")
		return
	}

	if container.UserID != username.(string) {
		response.Error(c, 403, "无权访问该容器")
		return
	}

	config, err := containerService.GetConfig(name)
	if err != nil {
		response.Error(c, 404, err.Error())
		return
	}

	response.Success(c, config)
}

type UserUpdateContainerConfigRequest struct {
	CPU               *int    `json:"cpu"`
	Memory            *int    `json:"memory"`
	Disk              *int    `json:"disk"`
	IPv4PoolLimit     *int    `json:"ipv4_pool_limit"`
	IPv4MappingLimit  *int    `json:"ipv4_mapping_limit"`
	IPv6PoolLimit     *int    `json:"ipv6_pool_limit"`
	IPv6MappingLimit  *int    `json:"ipv6_mapping_limit"`
	ReverseProxyLimit *int    `json:"reverse_proxy_limit"`
	Remark            *string `json:"remark"`
}

func UpdateContainerConfig(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		response.Error(c, 400, "缺少容器名称")
		return
	}

	username, _ := c.Get("username")

	container, err := containerService.Get(name)
	if err != nil {
		response.Error(c, 404, "容器不存在")
		return
	}

	if container.UserID != username.(string) {
		response.Error(c, 403, "无权操作该容器")
		return
	}

	if deny_code, deny_msg := service.CheckContainerOperationAllowed(container, true, "update_config"); deny_code != 0 {
		response.Error(c, deny_code, deny_msg)
		return
	}

	var userReq UserUpdateContainerConfigRequest
	if err := c.ShouldBindJSON(&userReq); err != nil {
		response.Error(c, 400, "参数错误: "+err.Error())
		return
	}

	if (userReq.CPU != nil && *userReq.CPU < 0) ||
		(userReq.Memory != nil && *userReq.Memory < 0) ||
		(userReq.Disk != nil && *userReq.Disk < 0) ||
		(userReq.IPv4PoolLimit != nil && *userReq.IPv4PoolLimit < 0) ||
		(userReq.IPv4MappingLimit != nil && *userReq.IPv4MappingLimit < 0) ||
		(userReq.IPv6PoolLimit != nil && *userReq.IPv6PoolLimit < 0) ||
		(userReq.IPv6MappingLimit != nil && *userReq.IPv6MappingLimit < 0) ||
		(userReq.ReverseProxyLimit != nil && *userReq.ReverseProxyLimit < 0) {
		response.Error(c, 400, "配额参数不能为负数")
		return
	}

	req := models.UpdateContainerConfigRequest{
		CPU:               userReq.CPU,
		Memory:            userReq.Memory,
		Disk:              userReq.Disk,
		IPv4PoolLimit:     userReq.IPv4PoolLimit,
		IPv4MappingLimit:  userReq.IPv4MappingLimit,
		IPv6PoolLimit:     userReq.IPv6PoolLimit,
		IPv6MappingLimit:  userReq.IPv6MappingLimit,
		ReverseProxyLimit: userReq.ReverseProxyLimit,
		Remark:            userReq.Remark,
	}

	user, err := service.GetUserByUsername(username.(string))
	if err != nil {
		response.Error(c, 500, "获取用户信息失败")
		return
	}

	stats := service.GetUserFullStats(username.(string))

	if req.CPU != nil {
		if user.MaxCPUPerContainer > 0 && *req.CPU > user.MaxCPUPerContainer {
			response.Error(c, 400, fmt.Sprintf("单容器CPU超过限制，最大允许%d核", user.MaxCPUPerContainer))
			return
		}
		cpuDiff := *req.CPU - container.CPU
		if user.CPUQuota > 0 && stats.UsedCPU+cpuDiff > user.CPUQuota {
			response.Error(c, 400, "CPU配额不足")
			return
		}
	}

	if req.Memory != nil {
		memoryDiff := *req.Memory - container.Memory
		if user.MemoryQuota > 0 && stats.UsedMemory+memoryDiff > user.MemoryQuota {
			response.Error(c, 400, "内存配额不足")
			return
		}
	}

	if req.Disk != nil {
		if *req.Disk < container.Disk {
			response.Error(c, 400, "磁盘只能扩容，不能缩容")
			return
		}
		diskDiff := *req.Disk - container.Disk
		if user.DiskQuota > 0 && stats.UsedDisk+diskDiff > user.DiskQuota {
			response.Error(c, 400, "磁盘配额不足")
			return
		}
	}

	if req.IPv4PoolLimit != nil {
		var containerIPv4PoolUsed int64
		db.DB.Model(&models.IPv4Binding{}).Where("container_name = ?", name).Count(&containerIPv4PoolUsed)
		if *req.IPv4PoolLimit < int(containerIPv4PoolUsed) {
			response.Error(c, 400, fmt.Sprintf("IPv4地址池配额不能小于当前使用量(%d)", containerIPv4PoolUsed))
			return
		}
		ipv4PoolDiff := *req.IPv4PoolLimit - container.IPv4PoolLimit
		if user.IPv4PoolLimit > 0 && stats.UsedIPv4Pool+int64(ipv4PoolDiff) > int64(user.IPv4PoolLimit) {
			response.Error(c, 400, "IPv4地址池配额不足")
			return
		}
	}

	if req.IPv4MappingLimit != nil {
		var containerIPv4MapUsed int64
		db.DB.Model(&models.PortMappingV4{}).Where("container_name = ?", name).Count(&containerIPv4MapUsed)
		if *req.IPv4MappingLimit < int(containerIPv4MapUsed) {
			response.Error(c, 400, fmt.Sprintf("IPv4端口映射配额不能小于当前使用量(%d)", containerIPv4MapUsed))
			return
		}
		ipv4MappingDiff := *req.IPv4MappingLimit - container.IPv4MappingLimit
		if user.IPv4MappingLimit > 0 && stats.UsedIPv4Map+int64(ipv4MappingDiff) > int64(user.IPv4MappingLimit) {
			response.Error(c, 400, "IPv4端口映射配额不足")
			return
		}
	}

	if req.IPv6PoolLimit != nil {
		var containerIPv6PoolUsed int64
		db.DB.Model(&models.IPv6Binding{}).Where("container_name = ?", name).Count(&containerIPv6PoolUsed)
		if *req.IPv6PoolLimit < int(containerIPv6PoolUsed) {
			response.Error(c, 400, fmt.Sprintf("IPv6地址池配额不能小于当前使用量(%d)", containerIPv6PoolUsed))
			return
		}
		ipv6PoolDiff := *req.IPv6PoolLimit - container.IPv6PoolLimit
		if user.IPv6PoolLimit > 0 && stats.UsedIPv6Pool+int64(ipv6PoolDiff) > int64(user.IPv6PoolLimit) {
			response.Error(c, 400, "IPv6地址池配额不足")
			return
		}
	}

	if req.IPv6MappingLimit != nil {
		var containerIPv6MapUsed int64
		db.DB.Model(&models.PortMappingV6{}).Where("container_name = ?", name).Count(&containerIPv6MapUsed)
		if *req.IPv6MappingLimit < int(containerIPv6MapUsed) {
			response.Error(c, 400, fmt.Sprintf("IPv6端口映射配额不能小于当前使用量(%d)", containerIPv6MapUsed))
			return
		}
		ipv6MappingDiff := *req.IPv6MappingLimit - container.IPv6MappingLimit
		if user.IPv6MappingLimit > 0 && stats.UsedIPv6Map+int64(ipv6MappingDiff) > int64(user.IPv6MappingLimit) {
			response.Error(c, 400, "IPv6端口映射配额不足")
			return
		}
	}

	if req.ReverseProxyLimit != nil {
		var containerProxyUsed int64
		db.DB.Model(&models.ReverseProxy{}).Where("container_name = ?", name).Count(&containerProxyUsed)
		if *req.ReverseProxyLimit < int(containerProxyUsed) {
			response.Error(c, 400, fmt.Sprintf("反向代理配额不能小于当前使用量(%d)", containerProxyUsed))
			return
		}
		reverseProxyDiff := *req.ReverseProxyLimit - container.ReverseProxyLimit
		if user.ReverseProxyLimit > 0 && stats.UsedProxy+int64(reverseProxyDiff) > int64(user.ReverseProxyLimit) {
			response.Error(c, 400, "反向代理配额不足")
			return
		}
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

	logger.OK("用户 %s 更新容器 %s 配置任务已创建, 任务ID: %d", username, name, task.ID)
	response.Success(c, gin.H{
		"name":    name,
		"task_id": task.ID,
		"message": "配置更新任务已提交",
	})
}

func RefreshCache(c *gin.Context) {
	username, _ := c.Get("username")
	ctx := c.Request.Context()
	name := c.Query("name")

	if name != "" {
		dbContainer, err := containerService.Get(name)
		if err != nil {
			response.Error(c, 404, "容器不存在")
			return
		}
		
		if dbContainer.UserID != username.(string) {
			response.Error(c, 403, "无权访问此容器")
			return
		}
		
		if err := cache.RefreshContainerCache(ctx, name); err != nil {
			logger.Error("刷新容器 %s 缓存失败: %v", name, err)
			response.Error(c, 500, "刷新失败: "+err.Error())
			return
		}
		
		logger.Info("容器 %s 缓存刷新成功", name)
		response.Success(c, "缓存刷新成功")
		return
	}

	logger.Info("用户 %s 开始刷新所有容器缓存", username)
	
	userContainerNames, err := service.ListContainersByUser(username.(string))
	if err != nil {
		logger.Error("获取用户容器列表失败: %v", err)
		response.Error(c, 500, "获取容器列表失败: "+err.Error())
		return
	}
	
	total := len(userContainerNames)
	success := 0
	failed := 0
	
	for _, container := range userContainerNames {
		if err := cache.RefreshContainerCache(ctx, container.Name); err != nil {
			logger.Error("刷新容器 %s 缓存失败: %v", container.Name, err)
			failed++
		} else {
			success++
		}
	}
	
	logger.Info("用户 %s 刷新容器缓存完成: 总数=%d, 成功=%d, 失败=%d", username, total, success, failed)
	
	response.Success(c, gin.H{
		"total":   total,
		"success": success,
		"failed":  failed,
		"message": "缓存刷新完成",
	})
}

func GetContainerCredential(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		response.Error(c, 400, "缺少容器名称")
		return
	}

	username, _ := c.Get("username")
	
	container, err := containerService.Get(name)
	if err != nil {
		response.Error(c, 404, "容器不存在")
		return
	}
	
	if container.UserID != username.(string) {
		response.Error(c, 403, "无权访问此容器")
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

func RegenerateContainerCredential(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		response.Error(c, 400, "缺少容器名称")
		return
	}

	username, _ := c.Get("username")
	
	container, err := containerService.Get(name)
	if err != nil {
		response.Error(c, 404, "容器不存在")
		return
	}
	
	if container.UserID != username.(string) {
		response.Error(c, 403, "无权访问此容器")
		return
	}

	if deny_code, deny_msg := service.CheckContainerOperationAllowed(container, true, "regenerate_credential"); deny_code != 0 {
		response.Error(c, deny_code, deny_msg)
		return
	}

	cred, err := service.RegenerateContainerHash(name)
	if err != nil {
		logger.Error("重新生成容器Hash失败: %v", err)
		response.Error(c, 500, err.Error())
		return
	}

	logger.OK("用户 %s 重新生成容器Hash成功: %s", username.(string), name)
	response.Success(c, gin.H{
		"container_name": cred.ContainerName,
		"hash":           cred.Hash,
	})
}

func GetContainerIP(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		response.Error(c, 400, "缺少容器名称")
		return
	}

	username, _ := c.Get("username")
	
	var container models.Container
	if err := db.DB.Where("name = ?", name).First(&container).Error; err != nil {
		response.Error(c, 404, "容器不存在")
		return
	}
	
	if container.UserID != username.(string) {
		response.Error(c, 403, "无权访问此容器")
		return
	}

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

	username, _ := c.Get("username")

	var container models.Container
	if err := db.DB.Where("name = ?", name).First(&container).Error; err != nil {
		response.Error(c, 404, "容器不存在")
		return
	}

	if container.UserID != username.(string) {
		response.Error(c, 403, "无权操作此容器")
		return
	}

	if deny_code, deny_msg := service.CheckContainerOperationAllowed(&container, true, "allocate_ip"); deny_code != 0 {
		response.Error(c, deny_code, deny_msg)
		return
	}

	var req struct {
		Count int `json:"count"`
	}
	c.ShouldBindJSON(&req)
	if req.Count <= 0 {
		req.Count = 1
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
		logger.OK("用户 %s 为容器 %s 分配IPv4成功: %v", username, name, ips)
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
		logger.OK("用户 %s 为容器 %s 分配IPv6成功: %v", username, name, ips)
		response.Success(c, gin.H{
			"container": name,
			"ipv6":      ips,
			"count":     len(ips),
		})
	}
}

func ReleaseContainerIP(c *gin.Context) {
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

	var settings models.IPPoolSettings
	db.DB.First(&settings)
	
	if version == "v4" && !settings.AllowUserReleaseIPv4 {
		response.Error(c, 403, "管理员未开放用户释放IPv4权限")
		return
	}
	if version == "v6" && !settings.AllowUserReleaseIPv6 {
		response.Error(c, 403, "管理员未开放用户释放IPv6权限")
		return
	}

	username, _ := c.Get("username")

	var container models.Container
	if err := db.DB.Where("name = ?", name).First(&container).Error; err != nil {
		response.Error(c, 404, "容器不存在")
		return
	}

	if container.UserID != username.(string) {
		response.Error(c, 403, "无权操作此容器")
		return
	}

	if deny_code, deny_msg := service.CheckContainerOperationAllowed(&container, true, "release_ip"); deny_code != 0 {
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
		logger.OK("用户 %s 释放容器 %s IPv4成功: %s", username, name, req.IP)
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
		logger.OK("用户 %s 释放容器 %s IPv6成功: %s", username, name, req.IP)
		response.Success(c, gin.H{
			"container": name,
			"ip":        req.IP,
			"message":   "IPv6释放成功",
		})
	}
}

func GetContainerDNS(c *gin.Context) {
	name := c.Param("name")
	username, _ := c.Get("username")
	
	var container models.Container
	if err := db.DB.Where("name = ? AND user_id = ?", name, username).First(&container).Error; err != nil {
		response.Error(c, 404, "容器不存在")
		return
	}
	
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
	username, _ := c.Get("username")
	
	var container models.Container
	if err := db.DB.Where("name = ? AND user_id = ?", name, username).First(&container).Error; err != nil {
		response.Error(c, 404, "容器不存在")
		return
	}

	if deny_code, deny_msg := service.CheckContainerOperationAllowed(&container, true, "set_dns"); deny_code != 0 {
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
	
	if err := lxcClient.SetContainerDNS(ctx, name, req.DNS); err != nil {
		response.Error(c, 500, err.Error())
		return
	}
	
	logger.OK("用户 %s 设置容器 %s DNS成功: %v", username, name, req.DNS)
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

	username, _ := c.Get("username")

	var container models.Container
	if err := db.DB.Where("name = ?", name).First(&container).Error; err != nil {
		response.Error(c, 404, "容器不存在")
		return
	}

	if container.UserID != username.(string) {
		response.Error(c, 403, "无权操作此容器")
		return
	}

	if deny_code, deny_msg := service.CheckContainerOperationAllowed(&container, true, "update_remark"); deny_code != 0 {
		response.Error(c, deny_code, deny_msg)
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

	if err := db.DB.Model(&container).Update("remark", req.Remark).Error; err != nil {
		response.Error(c, 500, "更新备注失败")
		return
	}

	logger.OK("用户 %s 更新容器 %s 备注: %s", username, name, req.Remark)
	response.Success(c, gin.H{
		"container": name,
		"remark":    req.Remark,
		"message":   "备注更新成功",
	})
}
