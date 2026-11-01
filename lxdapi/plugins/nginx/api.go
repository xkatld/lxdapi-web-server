package nginx

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"lxdapi/internal/db"
	"lxdapi/models"
	"lxdapi/pkg/logger"
	"lxdapi/pkg/response"
	"regexp"
	"strconv"
	"strings"
	"time"
	"net"
	"github.com/gin-gonic/gin"
)

type APIHandler struct {
	plugin *NginxPlugin
}

func NewAPIHandler(plugin *NginxPlugin) *APIHandler {
	return &APIHandler{
		plugin: plugin,
	}
}

func validateSSL(certStr, keyStr string) error {
	certBytes := []byte(certStr)
	keyBytes := []byte(keyStr)
	tlsCert, err := tls.X509KeyPair(certBytes, keyBytes)
	if err != nil {
		return fmt.Errorf("解析失败或不匹配: %w", err)
	}
	if len(tlsCert.Certificate) == 0 {
		return fmt.Errorf("未在输入中找到任何有效的证书数据块")
	}
	_, err = x509.ParseCertificate(tlsCert.Certificate[0])
	if err != nil {
		return fmt.Errorf("证书数据解析失败: %w", err)
	}
	return nil
}

func validateDomain(domain string) error {
	domain = strings.TrimSpace(domain)

	if len(domain) == 0 {
		return fmt.Errorf("域名不能为空")
	}

	if len(domain) > 253 {
		return fmt.Errorf("域名长度不能超过253个字符")
	}

	if strings.Contains(domain, " ") {
		return fmt.Errorf("域名不能包含空格")
	}

	domainRegex := regexp.MustCompile(`\A(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}\z`)
	if !domainRegex.MatchString(domain) {
		return fmt.Errorf("域名格式不正确，只能包含字母、数字、点和中划线，且不能以中划线开头或结尾")
	}

	labels := strings.Split(domain, ".")
	for _, label := range labels {
		if len(label) > 63 {
			return fmt.Errorf("域名标签长度不能超过63个字符")
		}
		if len(label) == 0 {
			return fmt.Errorf("域名不能包含连续的点")
		}
	}

	return nil
}

// validateTargetIP 严格校验目标 IP，防止 Nginx 配置注入
func validateTargetIP(ip string) error {
    ip = strings.TrimSpace(ip)
    if ip == "" {
        return nil // 允许为空，后续会用容器 PrivateIP 兜底
    }
    // 必须是合法的 IP 字面量（IPv4 或 IPv6），不接受域名
    if net.ParseIP(ip) == nil {
        return fmt.Errorf("目标IP格式非法")
    }
    // 双重保险：显式拒绝危险字符
    if strings.ContainsAny(ip, ";{}# \t\r\n'\"") {
        return fmt.Errorf("目标IP包含非法字符")
    }
    return nil
}

func (h *APIHandler) GetConfig(c *gin.Context) {
	var config models.NginxConfig
	if err := db.DB.First(&config).Error; err != nil {
		response.Error(c, 500, "获取配置失败")
		return
	}
	response.Success(c, config)
}

func (h *APIHandler) UpdateConfig(c *gin.Context) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}

	var config models.NginxConfig
	if err := db.DB.First(&config).Error; err != nil {
		response.Error(c, 500, "获取配置失败")
		return
	}

	config.Enabled = req.Enabled

	if err := db.DB.Save(&config).Error; err != nil {
		response.Error(c, 500, "保存配置失败")
		return
	}

	response.Success(c, config)
}

func (h *APIHandler) GetRestrictions(c *gin.Context) {
	var config models.NginxConfig
	if err := db.DB.First(&config).Error; err != nil {
		response.Error(c, 500, "获取配置失败")
		return
	}
	response.Success(c, gin.H{
		"restricted_domains": config.RestrictedDomains,
		"restricted_ips":     config.RestrictedIPs,
	})
}

func (h *APIHandler) UpdateRestrictions(c *gin.Context) {
	var req struct {
		RestrictedDomains string `json:"restricted_domains"`
		RestrictedIPs     string `json:"restricted_ips"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}

	var config models.NginxConfig
	if err := db.DB.First(&config).Error; err != nil {
		response.Error(c, 500, "获取配置失败")
		return
	}

	config.RestrictedDomains = req.RestrictedDomains
	config.RestrictedIPs = req.RestrictedIPs

	if err := db.DB.Save(&config).Error; err != nil {
		response.Error(c, 500, "保存配置失败")
		return
	}

	response.Success(c, "保存成功")
}

func (h *APIHandler) ApplyConfig(c *gin.Context) {
	if err := h.plugin.ApplyConfig(); err != nil {
		response.Error(c, 500, fmt.Sprintf("应用配置失败: %v", err))
		return
	}
	
	now := time.Now()
	db.DB.Model(&models.NginxConfig{}).Where("id = ?", 1).Update("last_applied", now)
	
	response.Success(c, "配置已应用")
}

func (h *APIHandler) checkRestrictions(domain, targetIP string) error {
	var config models.NginxConfig
	if err := db.DB.First(&config).Error; err != nil {
		return nil
	}

	if config.RestrictedDomains != "" {
		lines := strings.Split(config.RestrictedDomains, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && strings.EqualFold(domain, line) {
				return fmt.Errorf("域名 %s 已被限制使用", domain)
			}
		}
	}

	if config.RestrictedIPs != "" && targetIP != "" {
		lines := strings.Split(config.RestrictedIPs, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && targetIP == line {
				return fmt.Errorf("IP %s 已被限制使用", targetIP)
			}
		}
	}

	return nil
}

func (h *APIHandler) GetStatus(c *gin.Context) {
	status, err := h.plugin.manager.GetStatus()
	if err != nil {
		response.Error(c, 500, "获取状态失败")
		return
	}
	
	var totalProxies int64
	var activeProxies int64
	db.DB.Model(&models.ReverseProxy{}).Count(&totalProxies)
	db.DB.Model(&models.ReverseProxy{}).Where("status = ?", "active").Count(&activeProxies)
	
	status["total_proxies"] = totalProxies
	status["active_proxies"] = activeProxies
	
	if startedAt, ok := status["started_at"].(string); ok && startedAt != "" {
		if t, err := time.Parse("Mon 2006-01-02 15:04:05 MST", startedAt); err == nil {
			duration := time.Since(t)
			status["uptime"] = formatDuration(duration)
		}
	}
	
	response.Success(c, status)
}

func (h *APIHandler) StartNginx(c *gin.Context) {
	if err := h.plugin.manager.Start(); err != nil {
		response.Error(c, 500, fmt.Sprintf("启动失败: %v", err))
		return
	}
	response.Success(c, map[string]string{"message": "Nginx服务已启动"})
}

func (h *APIHandler) StopNginx(c *gin.Context) {
	if err := h.plugin.manager.Stop(); err != nil {
		response.Error(c, 500, fmt.Sprintf("停止失败: %v", err))
		return
	}
	response.Success(c, map[string]string{"message": "Nginx服务已停止"})
}

func (h *APIHandler) RestartNginx(c *gin.Context) {
	if err := h.plugin.manager.Restart(); err != nil {
		response.Error(c, 500, fmt.Sprintf("重启失败: %v", err))
		return
	}
	response.Success(c, map[string]string{"message": "Nginx服务已重启"})
}
func (h *APIHandler) ReloadNginx(c *gin.Context) {
	if err := h.plugin.manager.Reload(); err != nil {
		response.Error(c, 500, fmt.Sprintf("重载失败: %v", err))
		return
	}
	response.Success(c, "Nginx已重载")
}

func (h *APIHandler) GetProxies(c *gin.Context) {
	var proxies []models.ReverseProxy
	query := db.DB.Model(&models.ReverseProxy{})
	
	if containerName := c.Query("container_name"); containerName != "" {
		query = query.Where("container_name = ?", containerName)
	}
	
	if protocol := c.Query("protocol"); protocol != "" {
		query = query.Where("protocol = ?", protocol)
	}
	
	if err := query.Order("created_at DESC").Find(&proxies).Error; err != nil {
		response.Error(c, 500, "查询失败")
		return
	}
	
	response.Success(c, proxies)
}

func (h *APIHandler) CreateProxy(c *gin.Context) {
	var req models.ReverseProxy
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}

	if req.ContainerName == "" || req.Domain == "" || req.Protocol == "" {
		response.Error(c, 400, "容器名称、域名和协议不能为空")
		return
	}

	if req.SSLCert != "" || req.SSLKey != "" {
		if err := validateSSL(req.SSLCert, req.SSLKey); err != nil {
			response.Error(c, 400, err.Error())
			return
		}
	}
	if err := validateDomain(req.Domain); err != nil {
		response.Error(c, 400, err.Error())
		return
	}
	if err := validateTargetIP(req.TargetIP); err != nil {
	    response.Error(c, 400, err.Error())
	    return
	}
	if req.Protocol != "http" && req.Protocol != "https" {
		response.Error(c, 400, "协议只能是http或https")
		return
	}
	
	if req.Protocol == "http" && req.PublicPort != 80 {
		req.PublicPort = 80
	}
	if req.Protocol == "https" && req.PublicPort != 443 {
		req.PublicPort = 443
	}
	
	var container models.Container
	if err := db.DB.Where("name = ?", req.ContainerName).First(&container).Error; err != nil {
		response.Error(c, 404, "容器不存在")
		return
	}

	targetIP := req.TargetIP
	if targetIP == "" {
		targetIP = container.PrivateIP
	}
	if err := h.checkRestrictions(req.Domain, targetIP); err != nil {
		response.Error(c, 400, err.Error())
		return
	}
	
	var existingProxy models.ReverseProxy
	if err := db.DB.Where("domain = ?", req.Domain).First(&existingProxy).Error; err == nil {
		response.Error(c, 400, "域名已被使用")
		return
	}
	
	var usedCount int64
	db.DB.Model(&models.ReverseProxy{}).Where("container_name = ? AND status = ?", req.ContainerName, "active").Count(&usedCount)
	if container.ReverseProxyLimit > 0 && usedCount >= int64(container.ReverseProxyLimit) {
		response.Error(c, 400, fmt.Sprintf("反向代理配额已用完 (%d/%d)", usedCount, container.ReverseProxyLimit))
		return
	}
	
	if req.TargetIP == "" {
		req.TargetIP = container.PrivateIP
	}
	if req.Status == "" {
		req.Status = "active"
	}
	if req.Protocol == "https" {
		req.EnableSSL = true
	}
	
	if err := db.DB.Create(&req).Error; err != nil {
		response.Error(c, 500, "创建失败")
		return
	}
	
	if h.plugin.config.Enabled {
		if err := h.plugin.ApplyConfig(); err != nil {
			logger.Error("应用Nginx配置失败: %v", err)
			response.Error(c, 500, fmt.Sprintf("规则已创建但应用配置失败: %v", err))
			return
		}
	}
	
	response.Success(c, req)
}

func (h *APIHandler) UpdateProxy(c *gin.Context) {
	id := c.Param("id")

	var proxy models.ReverseProxy
	if err := db.DB.First(&proxy, id).Error; err != nil {
		response.Error(c, 404, "规则不存在")
		return
	}

	var req models.ReverseProxy
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}

	if req.SSLCert != "" || req.SSLKey != "" {
		if err := validateSSL(req.SSLCert, req.SSLKey); err != nil {
			response.Error(c, 400, err.Error())
			return
		}
	}
	if err := validateDomain(req.Domain); err != nil {
		response.Error(c, 400, err.Error())
		return
	}
	if err := validateTargetIP(req.TargetIP); err != nil {
	    response.Error(c, 400, err.Error())
	    return
	}
	proxy.Domain = req.Domain
	proxy.Protocol = req.Protocol
	proxy.TargetIP = req.TargetIP
	proxy.TargetPort = req.TargetPort
	proxy.EnableSSL = req.EnableSSL
	proxy.SSLCert = req.SSLCert
	proxy.SSLKey = req.SSLKey
	proxy.CustomConfig = req.CustomConfig
	proxy.Description = req.Description
	proxy.Status = req.Status
	
	if proxy.Protocol == "http" {
		proxy.PublicPort = 80
	} else if proxy.Protocol == "https" {
		proxy.PublicPort = 443
	}
	
	if err := db.DB.Save(&proxy).Error; err != nil {
		response.Error(c, 500, "更新失败")
		return
	}
	
	if h.plugin.config.Enabled {
		h.plugin.ApplyConfig()
	}
	
	response.Success(c, proxy)
}

func (h *APIHandler) DeleteProxy(c *gin.Context) {
	id := c.Param("id")
	
	if err := db.DB.Unscoped().Delete(&models.ReverseProxy{}, id).Error; err != nil {
		response.Error(c, 500, "删除失败")
		return
	}
	
	if h.plugin.config.Enabled {
		h.plugin.ApplyConfig()
	}
	
	response.Success(c, "删除成功")
}

func (h *APIHandler) BatchDeleteProxies(c *gin.Context) {
	var req struct {
		IDs []int `json:"ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}

	if len(req.IDs) == 0 {
		response.Error(c, 400, "请选择要删除的规则")
		return
	}

	result := db.DB.Unscoped().Delete(&models.ReverseProxy{}, req.IDs)
	deleted := int(result.RowsAffected)

	if h.plugin.config.Enabled {
		h.plugin.ApplyConfig()
	}

	logger.OK("批量删除代理规则: %d", deleted)
	response.Success(c, gin.H{"deleted": deleted})
}

func (h *APIHandler) GetContainerProxies(c *gin.Context) {
	containerName := c.Param("name")
	
	var proxies []models.ReverseProxy
	if err := db.DB.Where("container_name = ?", containerName).Find(&proxies).Error; err != nil {
		response.Error(c, 500, "查询失败")
		return
	}
	
	response.Success(c, proxies)
}

func (h *APIHandler) GetUserProxies(c *gin.Context) {
	username := c.GetString("username")
	if username == "" {
		response.Error(c, 401, "未授权")
		return
	}
	
	var containers []models.Container
	if err := db.DB.Where("user_id = ?", username).Find(&containers).Error; err != nil {
		response.Error(c, 500, "查询容器失败")
		return
	}
	
	containerNames := make([]string, len(containers))
	for i, c := range containers {
		containerNames[i] = c.Name
	}
	
	var proxies []models.ReverseProxy
	if err := db.DB.Where("container_name IN ?", containerNames).Find(&proxies).Error; err != nil {
		response.Error(c, 500, "查询失败")
		return
	}
	
	response.Success(c, proxies)
}

func (h *APIHandler) CreateUserProxy(c *gin.Context) {
	username := c.GetString("username")
	if username == "" {
		response.Error(c, 401, "未授权")
		return
	}
	
	var req models.ReverseProxy
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}
	
	var container models.Container
	if err := db.DB.Where("name = ? AND user_id = ?", req.ContainerName, username).First(&container).Error; err != nil {
		response.Error(c, 403, "无权操作此容器")
		return
	}
	
	h.createProxyInternal(c, req, container)
}

func (h *APIHandler) UpdateUserProxy(c *gin.Context) {
	username := c.GetString("username")
	id, _ := strconv.Atoi(c.Param("id"))
	
	var proxy models.ReverseProxy
	if err := db.DB.First(&proxy, id).Error; err != nil {
		response.Error(c, 404, "规则不存在")
		return
	}
	
	var container models.Container
	if err := db.DB.Where("name = ? AND user_id = ?", proxy.ContainerName, username).First(&container).Error; err != nil {
		response.Error(c, 403, "无权操作此规则")
		return
	}
	
	h.UpdateProxy(c)
}

func (h *APIHandler) DeleteUserProxy(c *gin.Context) {
	username := c.GetString("username")
	id, _ := strconv.Atoi(c.Param("id"))
	
	var proxy models.ReverseProxy
	if err := db.DB.First(&proxy, id).Error; err != nil {
		response.Error(c, 404, "规则不存在")
		return
	}
	
	var container models.Container
	if err := db.DB.Where("name = ? AND user_id = ?", proxy.ContainerName, username).First(&container).Error; err != nil {
		response.Error(c, 403, "无权操作此规则")
		return
	}
	
	h.DeleteProxy(c)
}

func (h *APIHandler) GetContainerProxiesAPI(c *gin.Context) {
	containerName := c.GetString("container_name")
	if containerName == "" {
		response.Error(c, 401, "未授权")
		return
	}
	
	var proxies []models.ReverseProxy
	if err := db.DB.Where("container_name = ?", containerName).Find(&proxies).Error; err != nil {
		response.Error(c, 500, "查询失败")
		return
	}
	
	response.Success(c, proxies)
}

func (h *APIHandler) CreateContainerProxy(c *gin.Context) {
	containerName := c.GetString("container_name")
	if containerName == "" {
		response.Error(c, 401, "未授权")
		return
	}
	
	var req models.ReverseProxy
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}
	
	req.ContainerName = containerName
	
	var container models.Container
	if err := db.DB.Where("name = ?", containerName).First(&container).Error; err != nil {
		response.Error(c, 404, "容器不存在")
		return
	}
	
	h.createProxyInternal(c, req, container)
}

func (h *APIHandler) UpdateContainerProxy(c *gin.Context) {
	containerName := c.GetString("container_name")
	id, _ := strconv.Atoi(c.Param("id"))
	
	var proxy models.ReverseProxy
	if err := db.DB.First(&proxy, id).Error; err != nil {
		response.Error(c, 404, "规则不存在")
		return
	}
	
	if proxy.ContainerName != containerName {
		response.Error(c, 403, "无权操作此规则")
		return
	}
	
	h.UpdateProxy(c)
}

func (h *APIHandler) DeleteContainerProxy(c *gin.Context) {
	containerName := c.GetString("container_name")
	id, _ := strconv.Atoi(c.Param("id"))
	
	var proxy models.ReverseProxy
	if err := db.DB.First(&proxy, id).Error; err != nil {
		response.Error(c, 404, "规则不存在")
		return
	}
	
	if proxy.ContainerName != containerName {
		response.Error(c, 403, "无权操作此规则")
		return
	}
	
	h.DeleteProxy(c)
}

func (h *APIHandler) createProxyInternal(c *gin.Context, req models.ReverseProxy, container models.Container) {
	if req.Domain == "" || req.Protocol == "" {
		response.Error(c, 400, "域名和协议不能为空")
		return
	}

	if req.SSLCert != "" || req.SSLKey != "" {
		if err := validateSSL(req.SSLCert, req.SSLKey); err != nil {
			response.Error(c, 400, err.Error())
			return
		}
	}
	if err := validateDomain(req.Domain); err != nil {
		response.Error(c, 400, err.Error())
		return
	}

	if req.Protocol != "http" && req.Protocol != "https" {
		response.Error(c, 400, "协议只能是http或https")
		return
	}
	
	if req.Protocol == "http" {
		req.PublicPort = 80
	} else {
		req.PublicPort = 443
		req.EnableSSL = true
	}

	targetIP := req.TargetIP
	if targetIP == "" {
		targetIP = container.PrivateIP
	}
	if err := h.checkRestrictions(req.Domain, targetIP); err != nil {
		response.Error(c, 400, err.Error())
		return
	}
	
	var existingProxy models.ReverseProxy
	if err := db.DB.Where("domain = ?", req.Domain).First(&existingProxy).Error; err == nil {
		response.Error(c, 400, "域名已被使用")
		return
	}
	
	var usedCount int64
	db.DB.Model(&models.ReverseProxy{}).Where("container_name = ? AND status = ?", req.ContainerName, "active").Count(&usedCount)
	if container.ReverseProxyLimit > 0 && usedCount >= int64(container.ReverseProxyLimit) {
		response.Error(c, 400, fmt.Sprintf("反向代理配额已用完 (%d/%d)", usedCount, container.ReverseProxyLimit))
		return
	}
	
	if req.TargetIP == "" {
		req.TargetIP = container.PrivateIP
	}
	if req.Status == "" {
		req.Status = "active"
	}
	
	if err := db.DB.Create(&req).Error; err != nil {
		response.Error(c, 500, "创建失败")
		return
	}
	
	if h.plugin.config.Enabled {
		if err := h.plugin.ApplyConfig(); err != nil {
			logger.Error("应用Nginx配置失败: %v", err)
			response.Error(c, 500, fmt.Sprintf("规则已创建但应用配置失败: %v", err))
			return
		}
	}
	
	response.Success(c, req)
}

func formatDuration(d time.Duration) string {
	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60
	
	if days > 0 {
		return fmt.Sprintf("%dd%dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh%dm", hours, minutes)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}
