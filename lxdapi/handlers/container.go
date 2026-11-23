package handlers

import (
	"github.com/gin-gonic/gin"
	"lxdapi/pkg/utils"
	"net/http"
)

func ContainerLogin(c *gin.Context) {
	c.HTML(http.StatusOK, "container/container_login.html", utils.MergeTemplateData(c, gin.H{}))
}

func ContainerDashboard(c *gin.Context) {
	hash := c.Query("hash")
	c.HTML(http.StatusOK, "container/container_dashboard.html", utils.MergeTemplateData(c, gin.H{
		"hash": hash,
	}))
}

func ContainerDashboardBase(c *gin.Context) {
	hash := c.Query("hash")
	c.HTML(http.StatusOK, "container/container_dashboard_base1.html", utils.MergeTemplateData(c, gin.H{
		"hash": hash,
	}))
}

func ContainerDashboardLite(c *gin.Context) {
	hash := c.Query("hash")
	c.HTML(http.StatusOK, "container/container_dashboard_lite1.html", utils.MergeTemplateData(c, gin.H{
		"hash": hash,
	}))
}

