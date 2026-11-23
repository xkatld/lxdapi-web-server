package handlers

import (
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"lxdapi/internal/console"
	"lxdapi/internal/db"
	"lxdapi/models"
	"lxdapi/pkg/utils"
	"net/http"
)

func AdminLogin(c *gin.Context) {
	c.HTML(http.StatusOK, "admin/admin_login.html", utils.MergeTemplateData(c, gin.H{}))
}

func AdminLogout(c *gin.Context) {
	session := sessions.Default(c)
	session.Clear()
	session.Save()
	c.Redirect(http.StatusFound, "/admin/login")
}

func AdminDashboard(c *gin.Context) {
	c.HTML(http.StatusOK, "admin/admin_dashboard.html", utils.MergeTemplateData(c, gin.H{
		"title":    "仪表盘 - LXD API 管理",
		"username": "admin",
	}))
}

func AdminContainers(c *gin.Context) {
	c.HTML(http.StatusOK, "admin/admin_containers.html", utils.MergeTemplateData(c, gin.H{
		"title":    "容器管理 - LXD API 管理",
		"username": "admin",
	}))
}

func AdminContainerDetail(c *gin.Context) {
	containerName := c.Param("name")
	c.HTML(http.StatusOK, "admin/admin_container_detail.html", utils.MergeTemplateData(c, gin.H{
		"title":          "容器详情 - LXD API 管理",
		"username":       "admin",
		"container_name": containerName,
	}))
}

func AdminTasks(c *gin.Context) {
	c.HTML(http.StatusOK, "admin/admin_tasks.html", utils.MergeTemplateData(c, gin.H{
		"title":    "任务管理 - LXD API 管理",
		"username": "admin",
	}))
}

func AdminIPv4(c *gin.Context) {
	c.HTML(http.StatusOK, "admin/admin_ipv4.html", utils.MergeTemplateData(c, gin.H{
		"title":    "IPv4端口映射 - LXD API 管理",
		"username": "admin",
	}))
}

func AdminIPv6(c *gin.Context) {
	c.HTML(http.StatusOK, "admin/admin_ipv6.html", utils.MergeTemplateData(c, gin.H{
		"title":    "IPv6端口映射 - LXD API 管理",
		"username": "admin",
	}))
}

func AdminIPPoolV4(c *gin.Context) {
	c.HTML(http.StatusOK, "admin/admin_ip_pool_v4.html", utils.MergeTemplateData(c, gin.H{
		"title":    "IPv4地址池 - LXD API 管理",
		"username": "admin",
	}))
}

func AdminIPPoolV6(c *gin.Context) {
	c.HTML(http.StatusOK, "admin/admin_ip_pool_v6.html", utils.MergeTemplateData(c, gin.H{
		"title":    "IPv6地址池 - LXD API 管理",
		"username": "admin",
	}))
}

func AdminPortMappingV4(c *gin.Context) {
	c.HTML(http.StatusOK, "admin/admin_port_mapping_v4.html", utils.MergeTemplateData(c, gin.H{
		"title":    "IPv4端口映射 - LXD API 管理",
		"username": "admin",
	}))
}

func AdminPortMappingV6(c *gin.Context) {
	c.HTML(http.StatusOK, "admin/admin_port_mapping_v6.html", utils.MergeTemplateData(c, gin.H{
		"title":    "IPv6端口映射 - LXD API 管理",
		"username": "admin",
	}))
}

func AdminTemplates(c *gin.Context) {
	c.HTML(http.StatusOK, "admin/admin_templates.html", utils.MergeTemplateData(c, gin.H{
		"title":    "模板管理 - LXD API 管理",
		"username": "admin",
	}))
}

func AdminUsers(c *gin.Context) {
	c.HTML(http.StatusOK, "admin/admin_users.html", utils.MergeTemplateData(c, gin.H{
		"title":    "用户管理 - LXD API 管理",
		"username": "admin",
	}))
}

func AdminBrandSettings(c *gin.Context) {
	c.HTML(http.StatusOK, "admin/admin_brand_settings.html", utils.MergeTemplateData(c, gin.H{
		"title":    "主题设置 - LXD API 管理",
		"username": "admin",
	}))
}

func AdminFirewall(c *gin.Context) {
	c.HTML(http.StatusOK, "admin/admin_firewall.html", utils.MergeTemplateData(c, gin.H{
		"title":    "防火墙管理 - LXD API 管理",
		"username": "admin",
	}))
}

func AdminNginx(c *gin.Context) {
	c.HTML(http.StatusOK, "admin/admin_nginx.html", utils.MergeTemplateData(c, gin.H{
		"title":    "Nginx反向代理 - LXD API 管理",
		"username": "admin",
	}))
}

func AdminStoragePools(c *gin.Context) {
	c.HTML(http.StatusOK, "admin/storage_pools.html", utils.MergeTemplateData(c, gin.H{
		"title":    "存储池管理 - LXD API 管理",
		"username": "admin",
	}))
}

func AdminUserDetail(c *gin.Context) {
	userID := c.Param("id")
	c.HTML(http.StatusOK, "admin/admin_user_detail.html", utils.MergeTemplateData(c, gin.H{
		"title":    "用户详情 - LXD API 管理",
		"username": "admin",
		"user_id":  userID,
	}))
}

func ConsolePage(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.HTML(http.StatusBadRequest, "error.html", gin.H{
			"error": "缺少访问令牌",
		})
		return
	}

	containerName, valid, errMsg := console.ValidateToken(token)
	if !valid {
		c.HTML(http.StatusUnauthorized, "error.html", gin.H{
			"error": errMsg,
		})
		return
	}

	var container models.Container
	image := "未知"
	if err := db.DB.Where("name = ?", containerName).First(&container).Error; err == nil {
		if container.Image != "" {
			image = container.Image
		}
	}

	c.HTML(http.StatusOK, "console.html", gin.H{
		"hostname": containerName,
		"image":    image,
	})
}

