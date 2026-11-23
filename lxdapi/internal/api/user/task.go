package user

import (
	"github.com/gin-gonic/gin"
	"lxdapi/internal/db"
	"lxdapi/models"
	"lxdapi/pkg/response"
)

func GetTask(c *gin.Context) {
	taskID := c.Param("id")
	if taskID == "" {
		response.Error(c, 400, "缺少任务ID")
		return
	}

	username, _ := c.Get("username")

	var task models.Task
	if err := db.DB.Where("id = ?", taskID).First(&task).Error; err != nil {
		response.Error(c, 404, "任务不存在")
		return
	}

	if task.UserID != username.(string) {
		response.Error(c, 403, "无权访问该任务")
		return
	}

	response.Success(c, gin.H{
		"id":        task.ID,
		"status":    task.Status,
		"action":    task.Action,
		"error_msg": task.ErrorMsg,
	})
}
