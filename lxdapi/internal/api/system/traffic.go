package system

import (
	"github.com/gin-gonic/gin"
	"lxdapi/internal/traffic"
	"lxdapi/pkg/response"
)

// GetTraffic 获取流量信息
func GetTraffic(c *gin.Context) {
	name := c.Query("name")
	if name == "" {
		response.Error(c, 400, "缺少容器名称")
		return
	}
	
	if traffic.GlobalMonitor == nil {
		response.Error(c, 500, "流量监控未启用")
		return
	}
	
	t, err := traffic.GlobalMonitor.GetTraffic(name)
	if err != nil {
		response.Error(c, 500, err.Error())
		return
	}
	
	response.Success(c, t)
}

// ResetTraffic 重置流量
func ResetTraffic(c *gin.Context) {
	name := c.Query("name")
	username := c.Query("username")
	
	if name == "" && username == "" {
		response.Error(c, 400, "缺少容器名称或用户名")
		return
	}
	
	if traffic.GlobalMonitor == nil {
		response.Error(c, 500, "流量监控未启用")
		return
	}
	
	if name != "" {
		if err := traffic.GlobalMonitor.ResetTraffic(name); err != nil {
			response.Error(c, 500, err.Error())
			return
		}
		response.Success(c, "流量已重置")
		return
	}
	
	if username != "" {
		containers, err := containerService.List(username)
		if err != nil {
			response.Error(c, 500, "获取用户容器列表失败")
			return
		}
		
		resetCount := 0
		for _, container := range containers {
			if err := traffic.GlobalMonitor.ResetTraffic(container.Name); err == nil {
				resetCount++
			}
		}
		
		response.Success(c, gin.H{
			"message": "流量已重置",
			"count": resetCount,
		})
	}
}

