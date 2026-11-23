package admin

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"lxdapi/pkg/logger"
	"lxdapi/pkg/response"
)

func GetNetworkNATStatus(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result := gin.H{}

	v4Output, err := exec.CommandContext(ctx, "lxc", "network", "get", "lxdbr0", "ipv4.nat").Output()
	if err != nil {
		result["ipv4_nat"] = false
	} else {
		result["ipv4_nat"] = strings.TrimSpace(string(v4Output)) == "true"
	}

	v6Output, err := exec.CommandContext(ctx, "lxc", "network", "get", "lxdbr0", "ipv6.nat").Output()
	if err != nil {
		result["ipv6_nat"] = false
	} else {
		result["ipv6_nat"] = strings.TrimSpace(string(v6Output)) == "true"
	}

	response.Success(c, result)
}

func SetNetworkNATStatus(c *gin.Context) {
	var req struct {
		IPv4NAT *bool `json:"ipv4_nat"`
		IPv6NAT *bool `json:"ipv6_nat"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if req.IPv4NAT != nil {
		value := "false"
		if *req.IPv4NAT {
			value = "true"
		}
		if err := exec.CommandContext(ctx, "lxc", "network", "set", "lxdbr0", "ipv4.nat", value).Run(); err != nil {
			logger.Error("设置IPv4 NAT失败: %v", err)
			response.Error(c, 500, "设置IPv4 NAT失败")
			return
		}
	}

	if req.IPv6NAT != nil {
		value := "false"
		if *req.IPv6NAT {
			value = "true"
		}
		if err := exec.CommandContext(ctx, "lxc", "network", "set", "lxdbr0", "ipv6.nat", value).Run(); err != nil {
			logger.Error("设置IPv6 NAT失败: %v", err)
			response.Error(c, 500, "设置IPv6 NAT失败")
			return
		}
	}

	response.Success(c, "设置成功")
}
