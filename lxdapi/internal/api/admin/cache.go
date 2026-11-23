package admin

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"
	"lxdapi/internal/cache"
	"lxdapi/internal/db"
	"lxdapi/internal/executor"
	"lxdapi/models"
	"lxdapi/pkg/logger"
	"lxdapi/pkg/response"
)

// GetContainersCache 获取容器缓存
// @Summary 获取容器缓存
// @Description 从缓存获取所有容器信息
// @Tags Admin API - 缓存管理
// @Accept json
// @Produce json
// @Success 200 {object} response.Response "获取成功"
// @Security SessionAuth
// @Router /api/admin/cache/containers [get]
func GetContainersCache(c *gin.Context) {
	containers := cache.GetAllContainersCache()
	
	response.Success(c, gin.H{
		"data":       containers,
		"count":      len(containers),
		"from_cache": true,
	})
}

// RefreshCache 刷新缓存（统一接口）
// @Summary 刷新缓存
// @Description 刷新容器缓存，无name参数刷新所有，有name参数刷新指定容器
// @Tags Admin API - 缓存管理
// @Accept json
// @Produce json
// @Param name query string false "容器名称，不传则刷新所有"
// @Success 200 {object} response.Response "刷新成功"
// @Failure 500 {object} response.Response "刷新失败"
// @Security SessionAuth
// @Router /api/admin/cache/refresh [post]
func RefreshCache(c *gin.Context) {
	ctx := c.Request.Context()
	name := c.Query("name")

	if name != "" {
		if err := cache.RefreshContainerCache(ctx, name); err != nil {
			logger.Error("刷新容器 %s 缓存失败: %v", name, err)
			response.Error(c, 500, "刷新失败")
			return
		}

		logger.Info("容器 %s 缓存刷新成功", name)
		response.Success(c, "缓存刷新成功")
		return
	}

	logger.Info("创建全量刷新缓存任务")

	params := map[string]interface{}{
		"action": "refresh_all_cache",
	}

	task, err := executor.CreateTask("system", "refresh_cache", "admin", params, func(ctx context.Context) error {
		total, success, err := cache.RefreshAllContainersCache(ctx)
		if err != nil {
			return err
		}

		failed := total - success
		logger.Info("刷新容器缓存完成: 总数=%d, 成功=%d, 失败=%d", total, success, failed)

		result := map[string]interface{}{
			"total":   total,
			"success": success,
			"failed":  failed,
		}
		resultBytes, _ := json.Marshal(result)
		executor.UpdateTaskResult(ctx, string(resultBytes))

		return nil
	})

	if err != nil {
		logger.Error("创建刷新缓存任务失败: %v", err)
		response.Error(c, 500, "创建任务失败")
		return
	}

	response.Success(c, gin.H{
		"task_id": task.ID,
		"message": "刷新任务已创建",
	})
}

func GetAutoRefreshSettings(c *gin.Context) {
	var settings models.CacheAutoRefreshSettings
	err := db.DB.Where("id = ?", 1).First(&settings).Error
	
	if err != nil {
		defaultSettings := models.CacheAutoRefreshSettings{
			ID:            1,
			Enabled:       false,
			Interval:      30,
			BatchSize:     10,
			BatchInterval: 5,
			LastRunTime:   time.Now(),
		}
		db.DB.Create(&defaultSettings)
		response.Success(c, defaultSettings)
		return
	}
	
	response.Success(c, settings)
}

func UpdateAutoRefreshSettings(c *gin.Context) {
	type UpdateRequest struct {
		Enabled       bool `json:"enabled"`
		Interval      int  `json:"interval"`
		BatchSize     int  `json:"batch_size"`
		BatchInterval int  `json:"batch_interval"`
	}
	
	var req UpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error("参数绑定失败: %v", err)
		response.Error(c, 400, "参数错误")
		return
	}
	
	if req.Interval < 1 || req.Interval > 1440 {
		response.Error(c, 400, "刷新间隔必须在1-1440分钟之间")
		return
	}
	if req.BatchSize < 1 || req.BatchSize > 100 {
		response.Error(c, 400, "批次大小必须在1-100之间")
		return
	}
	if req.BatchInterval < 1 || req.BatchInterval > 300 {
		response.Error(c, 400, "批次间隔必须在1-300秒之间")
		return
	}
	
	var settings models.CacheAutoRefreshSettings
	err := db.DB.Where("id = ?", 1).First(&settings).Error
	
	if err != nil {
		newSettings := models.CacheAutoRefreshSettings{
			ID:            1,
			Enabled:       req.Enabled,
			Interval:      req.Interval,
			BatchSize:     req.BatchSize,
			BatchInterval: req.BatchInterval,
			LastRunTime:   time.Now(),
		}
		if err := db.DB.Create(&newSettings).Error; err != nil {
			logger.Error("创建配置失败: %v", err)
			response.Error(c, 500, "创建配置失败")
			return
		}
		logger.OK("创建自动刷新配置: 启用=%v, 间隔=%d分钟", req.Enabled, req.Interval)
		response.Success(c, newSettings)
		return
	}
	
	settings.Enabled = req.Enabled
	settings.Interval = req.Interval
	settings.BatchSize = req.BatchSize
	settings.BatchInterval = req.BatchInterval
	
	if err := db.DB.Save(&settings).Error; err != nil {
		logger.Error("保存配置失败: %v", err)
		response.Error(c, 500, "保存配置失败")
		return
	}
	
	logger.OK("更新自动刷新配置: 启用=%v, 间隔=%d分钟", settings.Enabled, settings.Interval)
	response.Success(c, settings)
}
