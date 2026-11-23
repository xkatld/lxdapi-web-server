package system

import (
	"github.com/gin-gonic/gin"
	"lxdapi/internal/db"
	"lxdapi/models"
	"lxdapi/pkg/response"
	"strconv"
)

// GetTask 获取任务详情
func GetTask(c *gin.Context) {
	idStr := c.Query("id")
	if idStr == "" {
		response.Error(c, 400, "缺少任务ID")
		return
	}
	
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		response.Error(c, 400, "任务ID格式错误")
		return
	}
	
	var task models.Task
	if err := db.DB.First(&task, id).Error; err != nil {
		response.Error(c, 404, "任务不存在")
		return
	}
	
	response.Success(c, task)
}

// ListTasks 获取任务列表
func ListTasks(c *gin.Context) {
	containerName := c.Query("name")
	
	query := db.DB.Model(&models.Task{})
	if containerName != "" {
		query = query.Where("container_name = ?", containerName)
	}
	
	var tasks []models.Task
	if err := query.Order("id desc").Find(&tasks).Error; err != nil {
		response.Error(c, 500, err.Error())
		return
	}
	
	response.Success(c, tasks)
}

