package admin

import (
	"github.com/gin-gonic/gin"
	"lxdapi/internal/service"
	"lxdapi/models"
	"lxdapi/pkg/logger"
	"lxdapi/pkg/response"
)

type CreateUserRequest struct {
	Username           string `json:"username" binding:"required"`
	CPUQuota           int    `json:"cpu_quota"`
	MemoryQuota        int    `json:"memory_quota"`
	DiskQuota          int    `json:"disk_quota"`
	MaxCPUPerContainer int    `json:"max_cpu_per_container"`
	TrafficLimit       int    `json:"traffic_limit"`
	IPv4PoolLimit      int    `json:"ipv4_pool_limit"`
	IPv4MappingLimit   int    `json:"ipv4_mapping_limit"`
	IPv6PoolLimit      int    `json:"ipv6_pool_limit"`
	IPv6MappingLimit   int    `json:"ipv6_mapping_limit"`
	ReverseProxyLimit  int    `json:"reverse_proxy_limit"`
	Ingress            int    `json:"ingress"`
	Egress             int    `json:"egress"`
	CPUAllowance       int    `json:"cpu_allowance"`
	IORead             int    `json:"io_read"`
	IOWrite            int    `json:"io_write"`
	ProcessesLimit     int    `json:"processes_limit"`
	AllowNesting       bool   `json:"allow_nesting"`
	MemorySwap         bool   `json:"memory_swap"`
}

// CreateUser 创建用户
func CreateUser(c *gin.Context) {
	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}

	user, err := service.CreateUser(req.Username, "", req.CPUQuota, req.MemoryQuota, req.DiskQuota,
		req.MaxCPUPerContainer, req.TrafficLimit, req.IPv4PoolLimit, req.IPv4MappingLimit,
		req.IPv6PoolLimit, req.IPv6MappingLimit, req.ReverseProxyLimit,
		req.Ingress, req.Egress, req.CPUAllowance, req.IORead, req.IOWrite, req.ProcessesLimit,
		req.AllowNesting, req.MemorySwap)
	if err != nil {
		logger.Error("创建用户失败: %v", err)
		response.Error(c, 500, "创建用户失败")
		return
	}

	logger.OK("创建用户成功: %s", user.Username)
	response.Success(c, gin.H{
		"user":     user,
		"password": user.APIKey,
	})
}

// GetUsers 获取用户列表或详情（统一接口）
func GetUsers(c *gin.Context) {
	userID := c.Query("id")

	if userID != "" {
		user, containers, err := service.GetUserWithContainers(userID)
		if err != nil {
			logger.Error("获取用户失败: %v", err)
			response.Error(c, 404, "用户不存在")
			return
		}
		
		stats := service.GetUserFullStats(user.Username)

		response.Success(c, gin.H{
			"user":       user,
			"containers": containers,
			"total":      len(containers),
			"stats":      stats,
		})
		return
	}

	users, err := service.ListUsers()
	if err != nil {
		logger.Error("获取用户列表失败: %v", err)
		response.Error(c, 500, "获取用户列表失败")
		return
	}

	type UserWithCount struct {
		*models.User
		ContainerCount int64 `json:"container_count"`
		UsedCPU        int   `json:"used_cpu"`
		UsedMemory     int   `json:"used_memory"`
		UsedDisk       int   `json:"used_disk"`
	}
	
	usersWithCount := make([]UserWithCount, len(users))
	for i, user := range users {
		count, usedCPU, usedMemory, usedDisk := service.GetUserContainerStats(user.Username)
		usersWithCount[i] = UserWithCount{
			User:           &user,
			ContainerCount: count,
			UsedCPU:        usedCPU,
			UsedMemory:     usedMemory,
			UsedDisk:       usedDisk,
		}
	}

	response.Success(c, gin.H{
		"users": usersWithCount,
		"total": len(users),
	})
}

type UpdateUserRequest struct {
	Status             *string `json:"status"`
	CPUQuota           *int    `json:"cpu_quota"`
	MaxCPUPerContainer *int    `json:"max_cpu_per_container"`
	MemoryQuota        *int    `json:"memory_quota"`
	DiskQuota          *int    `json:"disk_quota"`
	TrafficLimit       *int    `json:"traffic_limit"`
	IPv4PoolLimit      *int    `json:"ipv4_pool_limit"`
	IPv4MappingLimit   *int    `json:"ipv4_mapping_limit"`
	IPv6PoolLimit      *int    `json:"ipv6_pool_limit"`
	IPv6MappingLimit   *int    `json:"ipv6_mapping_limit"`
	ReverseProxyLimit  *int    `json:"reverse_proxy_limit"`
	Ingress            *int    `json:"ingress"`
	Egress             *int    `json:"egress"`
	CPUAllowance       *int    `json:"cpu_allowance"`
	IORead             *int    `json:"io_read"`
	IOWrite            *int    `json:"io_write"`
	ProcessesLimit     *int    `json:"processes_limit"`
	AllowNesting       *bool   `json:"allow_nesting"`
	MemorySwap         *bool   `json:"memory_swap"`
}

// UpdateUser 更新用户
func UpdateUser(c *gin.Context) {
	userID := c.Param("id")
	if userID == "" {
		response.Error(c, 400, "缺少用户ID")
		return
	}

	var req UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}

	updates := make(map[string]interface{})
	if req.Status != nil {
		updates["status"] = *req.Status
	}
	if req.CPUQuota != nil {
		updates["cpu_quota"] = *req.CPUQuota
	}
	if req.MaxCPUPerContainer != nil {
		updates["max_cpu_per_container"] = *req.MaxCPUPerContainer
	}
	if req.MemoryQuota != nil {
		updates["memory_quota"] = *req.MemoryQuota
	}
	if req.DiskQuota != nil {
		updates["disk_quota"] = *req.DiskQuota
	}
	if req.TrafficLimit != nil {
		updates["traffic_limit"] = *req.TrafficLimit
	}
	if req.IPv4PoolLimit != nil {
		updates["ipv4_pool_limit"] = *req.IPv4PoolLimit
	}
	if req.IPv4MappingLimit != nil {
		updates["ipv4_mapping_limit"] = *req.IPv4MappingLimit
	}
	if req.IPv6PoolLimit != nil {
		updates["ipv6_pool_limit"] = *req.IPv6PoolLimit
	}
	if req.IPv6MappingLimit != nil {
		updates["ipv6_mapping_limit"] = *req.IPv6MappingLimit
	}
	if req.ReverseProxyLimit != nil {
		updates["reverse_proxy_limit"] = *req.ReverseProxyLimit
	}
	if req.Ingress != nil {
		updates["ingress"] = *req.Ingress
	}
	if req.Egress != nil {
		updates["egress"] = *req.Egress
	}
	if req.CPUAllowance != nil {
		updates["cpu_allowance"] = *req.CPUAllowance
	}
	if req.IORead != nil {
		updates["io_read"] = *req.IORead
	}
	if req.IOWrite != nil {
		updates["io_write"] = *req.IOWrite
	}
	if req.ProcessesLimit != nil {
		updates["processes_limit"] = *req.ProcessesLimit
	}
	if req.AllowNesting != nil {
		updates["allow_nesting"] = *req.AllowNesting
	}
	if req.MemorySwap != nil {
		updates["memory_swap"] = *req.MemorySwap
	}

	if len(updates) == 0 {
		response.Error(c, 400, "没有要更新的字段")
		return
	}

	if err := service.UpdateUser(userID, updates); err != nil {
		logger.Error("更新用户失败: %v", err)
		response.Error(c, 500, "更新用户失败")
		return
	}

	logger.OK("更新用户成功: %s", userID)
	response.Success(c, nil)
}

// DeleteUser 删除用户
func DeleteUser(c *gin.Context) {
	userID := c.Param("id")
	if userID == "" {
		userID = c.Query("user_id")
	}
	if userID == "" {
		response.Error(c, 400, "缺少用户ID")
		return
	}

	if err := service.DeleteUser(userID); err != nil {
		logger.Error("删除用户失败: %v", err)
		response.Error(c, 500, "删除用户失败")
		return
	}

	logger.OK("删除用户成功: %s", userID)
	response.Success(c, nil)
}

func BatchDeleteUsers(c *gin.Context) {
	var req struct {
		UserIDs []string `json:"user_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}

	if len(req.UserIDs) == 0 {
		response.Error(c, 400, "请选择要删除的用户")
		return
	}

	var deleted, skipped int
	for _, userID := range req.UserIDs {
		if err := service.DeleteUser(userID); err == nil {
			deleted++
		} else {
			skipped++
		}
	}

	logger.OK("批量删除用户: 成功%d, 跳过%d", deleted, skipped)
	response.Success(c, gin.H{"deleted": deleted, "skipped": skipped})
}

// RegenerateAPIKey 重新生成密码
func RegenerateAPIKey(c *gin.Context) {
	userID := c.Param("id")
	if userID == "" {
		userID = c.Query("user_id")
	}
	if userID == "" {
		response.Error(c, 400, "缺少用户ID")
		return
	}

	user, err := service.RegenerateAPIKey(userID, "")
	if err != nil {
		logger.Error("重新生成密码失败: %v", err)
		response.Error(c, 500, "重新生成密码失败")
		return
	}

	logger.OK("重新生成密码成功: %s", userID)
	response.Success(c, gin.H{
		"user":     user,
		"password": user.APIKey,
	})
}
