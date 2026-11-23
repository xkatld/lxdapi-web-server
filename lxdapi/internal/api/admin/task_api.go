package admin

import (
	"github.com/gin-gonic/gin"
	"lxdapi/internal/db"
	"lxdapi/models"
	"lxdapi/pkg/response"
)

// GetTaskList 获取任务列表
func GetTaskList(c *gin.Context) {
	var tasks []models.Task
	
	query := db.DB.Order("id DESC")
	
	containerName := c.Query("container_name")
	if containerName != "" {
		query = query.Where("container_name = ?", containerName)
	}
	
	if err := query.Find(&tasks).Error; err != nil {
		response.Error(c, 500, "获取任务列表失败")
		return
	}

	response.Success(c, gin.H{
		"tasks": tasks,
		"total": len(tasks),
	})
}

// GetTask 获取任务详情
func GetTask(c *gin.Context) {
	taskID := c.Param("id")
	if taskID == "" {
		response.Error(c, 400, "缺少任务ID")
		return
	}

	var task models.Task
	if err := db.DB.Where("id = ?", taskID).First(&task).Error; err != nil {
		response.Error(c, 404, "任务不存在")
		return
	}

	response.Success(c, task)
}

// DeleteTask 删除任务
func DeleteTask(c *gin.Context) {
	taskID := c.Param("id")
	
	if taskID != "" {
		if err := db.DB.Unscoped().Delete(&models.Task{}, "id = ?", taskID).Error; err != nil {
			response.Error(c, 500, "删除任务失败")
			return
		}
		response.Success(c, "任务删除成功")
		return
	}

	ids := c.Query("ids")
	if ids != "" {
		if err := db.DB.Unscoped().Delete(&models.Task{}, "id IN ?", splitIDs(ids)).Error; err != nil {
			response.Error(c, 500, "批量删除失败")
			return
		}
		response.Success(c, "批量删除成功")
		return
	}

	response.Error(c, 400, "缺少任务ID")
}

// BatchDeleteTasks 批量删除任务
func BatchDeleteTasks(c *gin.Context) {
	var req struct {
		TaskIDs []string `json:"task_ids"`
	}
	
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}
	
	if len(req.TaskIDs) == 0 {
		response.Error(c, 400, "请选择要删除的任务")
		return
	}

	if err := db.DB.Unscoped().Delete(&models.Task{}, "id IN ?", req.TaskIDs).Error; err != nil {
		response.Error(c, 500, "批量删除失败")
		return
	}

	response.Success(c, gin.H{
		"deleted": len(req.TaskIDs),
		"message": "批量删除成功",
	})
}

func splitIDs(ids string) []string {
	var result []string
	for _, id := range []byte(ids) {
		if id != ',' {
			result = append(result, string(id))
		}
	}
	return result
}
