package public

import (
	"github.com/gin-gonic/gin"
	"lxdapi/internal/service"
	"lxdapi/pkg/logger"
	"lxdapi/pkg/response"
)

// GetBrandSettings 获取品牌设置
// @Summary 获取品牌设置
// @Description 获取公开的品牌设置
// @Tags Public API
// @Accept json
// @Produce json
// @Success 200 {object} response.Response "获取成功"
// @Router /api/public/brand [get]
func GetBrandSettings(c *gin.Context) {
	svc := service.NewBrandService()
	settings, err := svc.GetSettings()
	if err != nil {
		logger.Error("获取品牌设置失败: %v", err)
		response.Success(c, gin.H{
			"system_name":  "LXD API",
			"system_title": "LXD容器管理系统",
			"footer_text":  "LXD API 容器管理平台",
		})
		return
	}
	
	response.Success(c, settings.ToPublic())
}
