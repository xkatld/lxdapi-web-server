package admin

import (
	"embed"
	"io/fs"
	"strings"

	"github.com/gin-gonic/gin"
	"lxdapi/internal/service"
	"lxdapi/models"
	"lxdapi/pkg/logger"
	"lxdapi/pkg/response"
)

var brandService *service.BrandService
var embeddedFS embed.FS

func InitBrandService() {
	brandService = service.NewBrandService()
}

func SetEmbeddedFS(efs embed.FS) {
	embeddedFS = efs
}

// GetBrandSettings 获取品牌设置
// @Summary 获取品牌设置
// @Description 获取系统品牌自定义设置
// @Tags Admin API - 品牌设置
// @Accept json
// @Produce json
// @Success 200 {object} response.Response "获取成功"
// @Failure 500 {object} response.Response "获取失败"
// @Security SessionAuth
// @Router /api/admin/brand-settings [get]
func GetBrandSettings(c *gin.Context) {
	settings, err := brandService.GetSettings()
	if err != nil {
		logger.Error("获取品牌设置失败: %v", err)
		response.Error(c, 500, "获取品牌设置失败")
		return
	}
	
	response.Success(c, settings)
}

// UpdateBrandSettings 更新品牌设置（统一接口）
// @Summary 更新品牌设置
// @Description 更新系统品牌自定义设置，reset=true时重置为默认
// @Tags Admin API - 品牌设置
// @Accept json
// @Produce json
// @Param reset query string false "是否重置为默认值"
// @Param request body models.BrandSettings false "品牌设置"
// @Success 200 {object} response.Response "更新成功"
// @Failure 400 {object} response.Response "参数错误"
// @Failure 500 {object} response.Response "更新失败"
// @Security SessionAuth
// @Router /api/admin/brand-settings [post]
func UpdateBrandSettings(c *gin.Context) {
	reset := c.Query("reset")
	if reset == "true" {
		if err := brandService.ResetToDefault(); err != nil {
			response.Error(c, 500, "重置失败: "+err.Error())
			return
		}
		
		logger.OK("品牌设置已重置")
		response.Success(c, "重置成功")
		return
	}

	var req models.BrandSettings
	
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误: "+err.Error())
		return
	}
	
	if len(req.AdminSystemName) == 0 || len(req.AdminSystemName) > 100 {
		response.Error(c, 400, "管理后台系统名称长度必须在1-100字符之间")
		return
	}
	if len(req.AdminSystemTitle) == 0 || len(req.AdminSystemTitle) > 100 {
		response.Error(c, 400, "管理后台浏览器标题长度必须在1-100字符之间")
		return
	}
	if len(req.AdminLoginTitle) == 0 || len(req.AdminLoginTitle) > 100 {
		response.Error(c, 400, "管理后台登录标题长度必须在1-100字符之间")
		return
	}
	
	if len(req.UserSystemName) == 0 || len(req.UserSystemName) > 100 {
		response.Error(c, 400, "用户中心系统名称长度必须在1-100字符之间")
		return
	}
	if len(req.UserSystemTitle) == 0 || len(req.UserSystemTitle) > 100 {
		response.Error(c, 400, "用户中心浏览器标题长度必须在1-100字符之间")
		return
	}
	if len(req.UserLoginTitle) == 0 || len(req.UserLoginTitle) > 100 {
		response.Error(c, 400, "用户中心登录标题长度必须在1-100字符之间")
		return
	}
	
	if len(req.ContainerSystemName) == 0 || len(req.ContainerSystemName) > 100 {
		response.Error(c, 400, "容器面板系统名称长度必须在1-100字符之间")
		return
	}
	if len(req.ContainerSystemTitle) == 0 || len(req.ContainerSystemTitle) > 100 {
		response.Error(c, 400, "容器面板浏览器标题长度必须在1-100字符之间")
		return
	}
	if len(req.ContainerLoginTitle) == 0 || len(req.ContainerLoginTitle) > 100 {
		response.Error(c, 400, "容器面板登录标题长度必须在1-100字符之间")
		return
	}
	
	if len(req.FooterText) > 200 {
		response.Error(c, 400, "页脚文本长度不能超过200字符")
		return
	}

	has_cert := strings.TrimSpace(req.TLSCertContent) != ""
	has_key := strings.TrimSpace(req.TLSKeyContent) != ""
	if has_cert != has_key {
		response.Error(c, 400, "TLS证书与私钥必须同时提供或同时留空")
		return
	}

	if err := brandService.UpdateSettings(&req); err != nil {
		response.Error(c, 500, "更新品牌设置失败: "+err.Error())
		return
	}
	
	logger.OK("品牌设置已更新")
	response.Success(c, "更新成功")
}

func GetContainerTemplates(c *gin.Context) {
	liteTemplates := []string{}
	baseTemplates := []string{}
	
	containerFS, err := fs.Sub(embeddedFS, "templates/container")
	if err == nil {
		fs.WalkDir(containerFS, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			name := d.Name()
			if strings.HasPrefix(name, "container_dashboard_") && strings.HasSuffix(name, ".html") {
				tmplName := strings.TrimPrefix(name, "container_dashboard_")
				tmplName = strings.TrimSuffix(tmplName, ".html")
				
				if strings.HasPrefix(tmplName, "lite") {
					liteTemplates = append(liteTemplates, tmplName)
				} else if strings.HasPrefix(tmplName, "base") {
					baseTemplates = append(baseTemplates, tmplName)
				}
			}
			return nil
		})
	}
	
	if len(liteTemplates) == 0 {
		liteTemplates = []string{"lite1"}
	}
	if len(baseTemplates) == 0 {
		baseTemplates = []string{"base1"}
	}
	
	response.Success(c, gin.H{
		"lite": liteTemplates,
		"base": baseTemplates,
	})
}
