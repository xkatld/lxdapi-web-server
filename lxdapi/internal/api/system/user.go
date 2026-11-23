package system

import (
	"github.com/gin-gonic/gin"
	"lxdapi/internal/service"
	"lxdapi/pkg/logger"
	"lxdapi/pkg/response"
)

func CreateUser(c *gin.Context) {
	var req struct {
		Username           string `json:"username" binding:"required"`
		Password           string `json:"password"`
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
		AllowNesting       *bool  `json:"allow_nesting"`
		MemorySwap         *bool  `json:"memory_swap"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误: "+err.Error())
		return
	}

	allowNesting := false
	memorySwap := true
	if req.AllowNesting != nil {
		allowNesting = *req.AllowNesting
	}
	if req.MemorySwap != nil {
		memorySwap = *req.MemorySwap
	}

	if req.Ingress == 0 {
		req.Ingress = 100
	}
	if req.Egress == 0 {
		req.Egress = 100
	}
	if req.CPUAllowance == 0 {
		req.CPUAllowance = 50
	}
	if req.IORead == 0 {
		req.IORead = 100
	}
	if req.IOWrite == 0 {
		req.IOWrite = 100
	}
	if req.ProcessesLimit == 0 {
		req.ProcessesLimit = 512
	}

	user, err := service.CreateUser(req.Username, req.Password,
		req.CPUQuota, req.MemoryQuota, req.DiskQuota, req.MaxCPUPerContainer, req.TrafficLimit,
		req.IPv4PoolLimit, req.IPv4MappingLimit, req.IPv6PoolLimit, req.IPv6MappingLimit,
		req.ReverseProxyLimit, req.Ingress, req.Egress, req.CPUAllowance,
		req.IORead, req.IOWrite, req.ProcessesLimit, allowNesting, memorySwap)
	if err != nil {
		logger.Error("创建用户失败: %v", err)
		response.Error(c, 500, err.Error())
		return
	}

	logger.OK("创建用户成功: %s", user.Username)
	response.Success(c, gin.H{
		"user": gin.H{
			"id":       user.ID,
			"username": user.Username,
			"password": user.APIKey,
			"status":   user.Status,
		},
	})
}

func GetUsers(c *gin.Context) {
	userID := c.Query("id")

	if userID != "" {
		user, err := service.GetUser(userID)
		if err != nil {
			response.Error(c, 404, err.Error())
			return
		}

		containerCount := service.GetUserContainerCount(user.Username)

		response.Success(c, gin.H{
			"user": gin.H{
				"id":                  user.ID,
				"username":            user.Username,
				"password":            user.APIKey,
				"status":              user.Status,
				"cpu_quota":           user.CPUQuota,
				"memory_quota":        user.MemoryQuota,
				"disk_quota":          user.DiskQuota,
				"traffic_limit":       user.TrafficLimit,
				"ipv4_pool_limit":     user.IPv4PoolLimit,
				"ipv4_mapping_limit":  user.IPv4MappingLimit,
				"ipv6_pool_limit":     user.IPv6PoolLimit,
				"ipv6_mapping_limit":  user.IPv6MappingLimit,
				"reverse_proxy_limit": user.ReverseProxyLimit,
				"container_count":     containerCount,
				"created_at":          user.CreatedAt,
				"updated_at":          user.UpdatedAt,
			},
		})
		return
	}

	users, err := service.ListUsers()
	if err != nil {
		response.Error(c, 500, err.Error())
		return
	}

	var result []gin.H
	for _, user := range users {
		containerCount := service.GetUserContainerCount(user.Username)
		result = append(result, gin.H{
			"id":              user.ID,
			"username":        user.Username,
			"password":        user.APIKey,
			"status":          user.Status,
			"container_count": containerCount,
			"created_at":      user.CreatedAt,
		})
	}

	response.Success(c, gin.H{
		"users": result,
		"total": len(users),
	})
}

func UpdateUser(c *gin.Context) {
	userID := c.Param("id")
	if userID == "" {
		response.Error(c, 400, "缺少用户ID")
		return
	}

	var req struct {
		CPUQuota          *int  `json:"cpu_quota"`
		MemoryQuota       *int  `json:"memory_quota"`
		DiskQuota         *int  `json:"disk_quota"`
		TrafficLimit      *int  `json:"traffic_limit"`
		IPv4PoolLimit     *int  `json:"ipv4_pool_limit"`
		IPv4MappingLimit  *int  `json:"ipv4_mapping_limit"`
		IPv6PoolLimit     *int  `json:"ipv6_pool_limit"`
		IPv6MappingLimit  *int  `json:"ipv6_mapping_limit"`
		ReverseProxyLimit *int  `json:"reverse_proxy_limit"`
		Ingress           *int  `json:"ingress"`
		Egress            *int  `json:"egress"`
		CPUAllowance      *int  `json:"cpu_allowance"`
		IORead            *int  `json:"io_read"`
		IOWrite           *int  `json:"io_write"`
		ProcessesLimit    *int  `json:"processes_limit"`
		AllowNesting      *bool `json:"allow_nesting"`
		MemorySwap        *bool `json:"memory_swap"`
		Status            *string `json:"status"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}

	if !isAdminAuthLevel(c) && req.AllowNesting != nil {
		response.Error(c, 403, "系统接口禁止修改或开启嵌套容器权限")
		return
	}

	updates := make(map[string]interface{})
	if req.CPUQuota != nil {
		updates["cpu_quota"] = *req.CPUQuota
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
	if req.Status != nil {
		updates["status"] = *req.Status
	}

	if len(updates) == 0 {
		response.Error(c, 400, "没有要更新的字段")
		return
	}

	if err := service.UpdateUser(userID, updates); err != nil {
		logger.Error("更新用户失败: %v", err)
		response.Error(c, 500, err.Error())
		return
	}

	logger.OK("更新用户成功: %s", userID)
	response.Success(c, "更新成功")
}

func DeleteUser(c *gin.Context) {
	userID := c.Param("id")
	if userID == "" {
		response.Error(c, 400, "缺少用户ID")
		return
	}

	if err := service.DeleteUser(userID); err != nil {
		logger.Error("删除用户失败: %v", err)
		response.Error(c, 500, err.Error())
		return
	}

	logger.OK("删除用户成功: %s", userID)
	response.Success(c, "删除成功")
}

func RegenerateUserAPIKey(c *gin.Context) {
	userID := c.Param("id")
	if userID == "" {
		response.Error(c, 400, "缺少用户ID")
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	c.ShouldBindJSON(&req)

	user, err := service.RegenerateAPIKey(userID, req.Password)
	if err != nil {
		logger.Error("重新生成密码失败: %v", err)
		response.Error(c, 500, err.Error())
		return
	}

	logger.OK("重新生成密码成功: %s", userID)
	response.Success(c, gin.H{
		"user": gin.H{
			"id":       user.ID,
			"username": user.Username,
			"password": user.APIKey,
			"status":   user.Status,
		},
	})
}
