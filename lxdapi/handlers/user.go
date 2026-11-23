package handlers

import (
	"github.com/gin-gonic/gin"
	"lxdapi/pkg/utils"
	"net/http"
)

func UserLogin(c *gin.Context) {
	c.HTML(http.StatusOK, "user/user_login.html", utils.MergeTemplateData(c, gin.H{}))
}

func UserDashboard(c *gin.Context) {
	c.Redirect(http.StatusFound, "/user/containers")
}

func UserContainers(c *gin.Context) {
	c.HTML(http.StatusOK, "user/user_containers.html", utils.MergeTemplateData(c, gin.H{
		"title":    "我的容器 - LXD API",
		"username": "User",
	}))
}

func UserContainerDetail(c *gin.Context) {
	c.HTML(http.StatusOK, "user/user_container_detail.html", utils.MergeTemplateData(c, gin.H{
		"title":    "容器详情 - LXD API",
		"username": "User",
		"name":     c.Query("name"),
	}))
}
