package admin

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"lxdapi/internal/service"
	"lxdapi/pkg/logger"
	"lxdapi/pkg/response"
)

// GetStoragePools 获取存储池列表
func GetStoragePools(c *gin.Context) {
	svc := service.NewStorageService()
	pools, err := svc.List()
	if err != nil {
		response.Error(c, 500, "获取存储池列表失败")
		return
	}

	type poolResponse struct {
		ID          uint   `json:"id"`
		Name        string `json:"name"`
		Driver      string `json:"driver"`
		Description string `json:"description"`
		Status      string `json:"status"`
		UsedBy      int    `json:"used_by"`
		TotalSpace  string `json:"total_space"`
		UsedSpace   string `json:"used_space"`
		UsagePercent float64 `json:"usage_percent"`
		Priority    int    `json:"priority"`
	}

	result := make([]poolResponse, len(pools))
	for i, p := range pools {
		var usagePercent float64
		if p.TotalSpace > 0 {
			usagePercent = float64(p.UsedSpace) / float64(p.TotalSpace) * 100
		}
		result[i] = poolResponse{
			ID:          p.ID,
			Name:        p.Name,
			Driver:      p.Driver,
			Description: p.Description,
			Status:      p.Status,
			UsedBy:      p.UsedBy,
			TotalSpace:  formatBytes(p.TotalSpace),
			UsedSpace:   formatBytes(p.UsedSpace),
			UsagePercent: usagePercent,
			Priority:    p.Priority,
		}
	}

	response.Success(c, gin.H{"pools": result, "count": len(result)})
}

// SyncStoragePools 同步存储池
func SyncStoragePools(c *gin.Context) {
	svc := service.NewStorageService()
	added, updated, deleted, err := svc.SyncFromLXD(c.Request.Context())
	if err != nil {
		logger.Error("同步存储池失败: %v", err)
		response.Error(c, 500, "同步失败")
		return
	}

	logger.OK("存储池同步完成")
	response.Success(c, gin.H{
		"added":   added,
		"updated": updated,
		"deleted": deleted,
		"message": "同步完成",
	})
}

// SetStoragePoolPriority 设置存储池优先级
func SetStoragePoolPriority(c *gin.Context) {
	name := c.Param("name")
	var req struct {
		Priority int `json:"priority"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}

	svc := service.NewStorageService()
	if err := svc.SetPriority(name, req.Priority); err != nil {
		response.Error(c, 500, "设置失败")
		return
	}

	logger.OK("存储池优先级设置: %s -> %d", name, req.Priority)
	response.Success(c, "设置成功")
}

func formatBytes(bytes int64) string {
	if bytes == 0 {
		return "0 B"
	}
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
