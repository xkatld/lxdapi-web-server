package nginx

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"lxdapi/internal/db"
	"lxdapi/internal/service"
	"lxdapi/models"
	"lxdapi/pkg/logger"
	"lxdapi/pkg/response"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type APIHandler struct {
	plugin *NginxPlugin
}

type userCreateProxyRequest struct {
	ContainerName string `json:"container_name"`
	Protocol      string `json:"protocol"`
	Domain        string `json:"domain"`
	TargetPort    int    `json:"target_port"`
	EnableSSL     bool   `json:"enable_ssl"`
	SSLCert       string `json:"ssl_cert"`
	SSLKey        string `json:"ssl_key"`
	Description   string `json:"description"`
}

type containerCreateProxyRequest struct {
	Protocol    string `json:"protocol"`
	Domain      string `json:"domain"`
	TargetPort  int    `json:"target_port"`
	EnableSSL   bool   `json:"enable_ssl"`
	SSLCert     string `json:"ssl_cert"`
	SSLKey      string `json:"ssl_key"`
	Description string `json:"description"`
}

type proxyUpdateRequest struct {
	Protocol    string `json:"protocol"`
	Domain      string `json:"domain"`
	TargetPort  int    `json:"target_port"`
	EnableSSL   bool   `json:"enable_ssl"`
	SSLCert     string `json:"ssl_cert"`
	SSLKey      string `json:"ssl_key"`
	Description string `json:"description"`
	Status      string `json:"status"`
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

	if strings.Contains(domain, "..") || strings.ContainsAny(domain, " \t\r\n/\\;:{}") {
		return fmt.Errorf("域名包含非法字符")
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

func validateTargetIP(targetIP string) error {
	if targetIP == "" {
		return fmt.Errorf("目标IP不能为空")
	}
	if strings.ContainsAny(targetIP, " \t\r\n;{}") {
		return fmt.Errorf("目标IP包含非法字符")
	}
	if net.ParseIP(targetIP) == nil {
		return fmt.Errorf("无效的目标IP地址")
	}
	return nil
}

func validateTargetPort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("目标端口必须在1至65535之间")
	}
	return nil
}

func parseProxyID(idStr string) (uint64, error) {
	id, err := strconv.ParseUint(strings.TrimSpace(idStr), 10, 64)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("无效的规则ID")
	}
	return id, nil
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

	req.Domain = strings.TrimSpace(req.Domain)
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

	if req.Protocol != "http" && req.Protocol != "https" {
		response.Error(c, 400, "协议只能是http或https")
		return
	}

	if err := validateTargetPort(req.TargetPort); err != nil {
		response.Error(c, 400, err.Error())
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
	if err := validateTargetIP(targetIP); err != nil {
		response.Error(c, 400, err.Error())
		return
	}
	req.TargetIP = targetIP
	if err := h.checkRestrictions(req.Domain, targetIP); err != nil {
		response.Error(c, 400, err.Error())
		return
	}
	
	var existingProxy models.ReverseProxy
	if err := db.DB.Where("domain = ?", req.Domain).First(&existingProxy).Error; err == nil {
		response.Error(c, 400, "域名已被使用")
		return
	}
	
	var used_count int64
	db.DB.Model(&models.ReverseProxy{}).Where("container_name = ? AND status = ?", req.ContainerName, "active").Count(&used_count)
	if container.ReverseProxyLimit <= 0 || used_count >= int64(container.ReverseProxyLimit) {
		response.Error(c, 400, fmt.Sprintf("反向代理配额已用完 (%d/%d)", used_count, container.ReverseProxyLimit))
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
	id, err := parseProxyID(c.Param("id"))
	if err != nil {
		response.Error(c, 400, err.Error())
		return
	}

	var proxy models.ReverseProxy
	if err := db.DB.Where("id = ?", id).First(&proxy).Error; err != nil {
		response.Error(c, 404, "规则不存在")
		return
	}

	var req models.ReverseProxy
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}

	req.Domain = strings.TrimSpace(req.Domain)
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
	if err := validateTargetPort(req.TargetPort); err != nil {
		response.Error(c, 400, err.Error())
		return
	}
	targetIP := req.TargetIP
	if targetIP == "" {
		targetIP = proxy.TargetIP
	}
	if err := validateTargetIP(targetIP); err != nil {
		response.Error(c, 400, err.Error())
		return
	}
	if err := h.checkRestrictions(req.Domain, targetIP); err != nil {
		response.Error(c, 400, err.Error())
		return
	}

	proxy.Domain = req.Domain
	proxy.Protocol = req.Protocol
	proxy.TargetIP = targetIP
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
	id, err := parseProxyID(c.Param("id"))
	if err != nil {
		response.Error(c, 400, err.Error())
		return
	}
	
	if err := db.DB.Unscoped().Where("id = ?", id).Delete(&models.ReverseProxy{}).Error; err != nil {
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
		IDs []uint `json:"ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}

	if len(req.IDs) == 0 {
		response.Error(c, 400, "请选择要删除的规则")
		return
	}

	for _, id := range req.IDs {
		if id == 0 {
			response.Error(c, 400, "包含无效的规则ID")
			return
		}
	}

	result := db.DB.Unscoped().Where("id IN ?", req.IDs).Delete(&models.ReverseProxy{})
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
	
	var req userCreateProxyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}
	
	var container models.Container
	if err := db.DB.Where("name = ? AND user_id = ?", req.ContainerName, username).First(&container).Error; err != nil {
		response.Error(c, 403, "无权操作此容器")
		return
	}

	if deny_code, deny_msg := service.CheckContainerOperationAllowed(&container, true, "create_proxy"); deny_code != 0 {
		response.Error(c, deny_code, deny_msg)
		return
	}
	
	proxy := models.ReverseProxy{
		ContainerName: req.ContainerName,
		Protocol:      req.Protocol,
		Domain:        strings.TrimSpace(req.Domain),
		TargetPort:    req.TargetPort,
		EnableSSL:     req.EnableSSL,
		SSLCert:       req.SSLCert,
		SSLKey:        req.SSLKey,
		Description:   req.Description,
	}
	h.createProxyInternal(c, proxy, container)
}

func (h *APIHandler) UpdateUserProxy(c *gin.Context) {
	username := c.GetString("username")
	id, err := parseProxyID(c.Param("id"))
	if err != nil {
		response.Error(c, 400, err.Error())
		return
	}
	
	var proxy models.ReverseProxy
	if err := db.DB.Where("id = ?", id).First(&proxy).Error; err != nil {
		response.Error(c, 404, "规则不存在")
		return
	}
	
	var container models.Container
	if err := db.DB.Where("name = ? AND user_id = ?", proxy.ContainerName, username).First(&container).Error; err != nil {
		response.Error(c, 403, "无权操作此规则")
		return
	}

	if deny_code, deny_msg := service.CheckContainerOperationAllowed(&container, true, "update_proxy"); deny_code != 0 {
		response.Error(c, deny_code, deny_msg)
		return
	}
	
	h.updateProxyInternal(c, &proxy, &container)
}

func (h *APIHandler) DeleteUserProxy(c *gin.Context) {
	username := c.GetString("username")
	id, err := parseProxyID(c.Param("id"))
	if err != nil {
		response.Error(c, 400, err.Error())
		return
	}
	
	var proxy models.ReverseProxy
	if err := db.DB.Where("id = ?", id).First(&proxy).Error; err != nil {
		response.Error(c, 404, "规则不存在")
		return
	}
	
	var container models.Container
	if err := db.DB.Where("name = ? AND user_id = ?", proxy.ContainerName, username).First(&container).Error; err != nil {
		response.Error(c, 403, "无权操作此规则")
		return
	}

	if deny_code, deny_msg := service.CheckContainerOperationAllowed(&container, true, "delete_proxy"); deny_code != 0 {
		response.Error(c, deny_code, deny_msg)
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
	container_name := c.GetString("container_name")
	if container_name == "" {
		response.Error(c, 401, "未授权")
		return
	}
	
	var req containerCreateProxyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}
	
	var container models.Container
	if err := db.DB.Where("name = ?", container_name).First(&container).Error; err != nil {
		response.Error(c, 404, "容器不存在")
		return
	}

	if deny_code, deny_msg := service.CheckContainerOperationAllowed(&container, false, "create_proxy"); deny_code != 0 {
		response.Error(c, deny_code, deny_msg)
		return
	}
	
	proxy := models.ReverseProxy{
		ContainerName: container_name,
		Protocol:      req.Protocol,
		Domain:        strings.TrimSpace(req.Domain),
		TargetPort:    req.TargetPort,
		EnableSSL:     req.EnableSSL,
		SSLCert:       req.SSLCert,
		SSLKey:        req.SSLKey,
		Description:   req.Description,
	}
	h.createProxyInternal(c, proxy, container)
}

func (h *APIHandler) UpdateContainerProxy(c *gin.Context) {
	containerName := c.GetString("container_name")
	id, err := parseProxyID(c.Param("id"))
	if err != nil {
		response.Error(c, 400, err.Error())
		return
	}
	
	var proxy models.ReverseProxy
	if err := db.DB.Where("id = ?", id).First(&proxy).Error; err != nil {
		response.Error(c, 404, "规则不存在")
		return
	}
	
	if proxy.ContainerName != containerName {
		response.Error(c, 403, "无权操作此规则")
		return
	}

	var container models.Container
	if err := db.DB.Where("name = ?", containerName).First(&container).Error; err != nil {
		response.Error(c, 404, "容器不存在")
		return
	}

	if deny_code, deny_msg := service.CheckContainerOperationAllowed(&container, false, "update_proxy"); deny_code != 0 {
		response.Error(c, deny_code, deny_msg)
		return
	}
	
	h.updateProxyInternal(c, &proxy, &container)
}

func (h *APIHandler) DeleteContainerProxy(c *gin.Context) {
	containerName := c.GetString("container_name")
	id, err := parseProxyID(c.Param("id"))
	if err != nil {
		response.Error(c, 400, err.Error())
		return
	}
	
	var proxy models.ReverseProxy
	if err := db.DB.Where("id = ?", id).First(&proxy).Error; err != nil {
		response.Error(c, 404, "规则不存在")
		return
	}
	
	if proxy.ContainerName != containerName {
		response.Error(c, 403, "无权操作此规则")
		return
	}

	if deny_code, deny_msg := service.CheckContainerOperationByName(proxy.ContainerName, false, "delete_proxy"); deny_code != 0 {
		response.Error(c, deny_code, deny_msg)
		return
	}
	
	h.DeleteProxy(c)
}

func (h *APIHandler) createProxyInternal(c *gin.Context, req models.ReverseProxy, container models.Container) {
	req.Domain = strings.TrimSpace(req.Domain)
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

	if err := validateTargetPort(req.TargetPort); err != nil {
		response.Error(c, 400, err.Error())
		return
	}
	
	if req.Protocol == "http" {
		req.PublicPort = 80
	} else {
		req.PublicPort = 443
		req.EnableSSL = true
	}

	if container.PrivateIP == "" {
		response.Error(c, 400, "容器内网IP为空，无法创建反向代理")
		return
	}
	req.TargetIP = container.PrivateIP
	if err := validateTargetIP(req.TargetIP); err != nil {
		response.Error(c, 400, err.Error())
		return
	}
	if err := h.checkRestrictions(req.Domain, req.TargetIP); err != nil {
		response.Error(c, 400, err.Error())
		return
	}
	
	var existing_proxy models.ReverseProxy
	if err := db.DB.Where("domain = ?", req.Domain).First(&existing_proxy).Error; err == nil {
		response.Error(c, 400, "域名已被使用")
		return
	}
	
	var used_count int64
	db.DB.Model(&models.ReverseProxy{}).Where("container_name = ? AND status = ?", req.ContainerName, "active").Count(&used_count)
	if container.ReverseProxyLimit <= 0 || used_count >= int64(container.ReverseProxyLimit) {
		response.Error(c, 400, fmt.Sprintf("反向代理配额已用完 (%d/%d)", used_count, container.ReverseProxyLimit))
		return
	}
	
	req.Status = "active"
	
	if err := db.DB.Create(&req).Error; err != nil {
		response.Error(c, 500, "创建失败")
		return
	}
	
	if h.plugin.config.Enabled {
		if err := h.plugin.ApplyConfig(); err != nil {
			logger.Error("应用Nginx配置失败: %v", err)
			response.Error(c, 500, "规则已创建但应用配置失败")
			return
		}
	}
	
	response.Success(c, req)
}

func (h *APIHandler) updateProxyInternal(c *gin.Context, proxy *models.ReverseProxy, container *models.Container) {
	var req proxyUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}

	req.Domain = strings.TrimSpace(req.Domain)
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
	if err := validateTargetPort(req.TargetPort); err != nil {
		response.Error(c, 400, err.Error())
		return
	}
	if req.Status != "" && req.Status != "active" && req.Status != "disabled" {
		response.Error(c, 400, "状态只能是active或disabled")
		return
	}
	if container.PrivateIP == "" {
		response.Error(c, 400, "容器内网IP为空，无法更新反向代理")
		return
	}
	locked_target_ip := container.PrivateIP
	if err := validateTargetIP(locked_target_ip); err != nil {
		response.Error(c, 400, err.Error())
		return
	}
	if err := h.checkRestrictions(req.Domain, locked_target_ip); err != nil {
		response.Error(c, 400, err.Error())
		return
	}

	var existing_proxy models.ReverseProxy
	if err := db.DB.Where("domain = ? AND id != ?", req.Domain, proxy.ID).First(&existing_proxy).Error; err == nil {
		response.Error(c, 400, "域名已被使用")
		return
	}

	target_status := req.Status
	if target_status == "" {
		target_status = proxy.Status
	}
	if target_status == "" {
		target_status = "active"
	}
	if target_status == "active" {
		if container.ReverseProxyLimit <= 0 {
			response.Error(c, 400, fmt.Sprintf("反向代理配额已用完 (0/%d)", container.ReverseProxyLimit))
			return
		}
		var used_count int64
		db.DB.Model(&models.ReverseProxy{}).Where("container_name = ? AND status = ? AND id != ?", container.Name, "active", proxy.ID).Count(&used_count)
		if used_count >= int64(container.ReverseProxyLimit) {
			response.Error(c, 400, fmt.Sprintf("反向代理配额已用完 (%d/%d)", used_count, container.ReverseProxyLimit))
			return
		}
	}

	proxy.Domain = req.Domain
	proxy.Protocol = req.Protocol
	proxy.TargetIP = locked_target_ip
	proxy.TargetPort = req.TargetPort
	proxy.EnableSSL = req.EnableSSL
	proxy.SSLCert = req.SSLCert
	proxy.SSLKey = req.SSLKey
	proxy.Description = req.Description
	proxy.Status = target_status

	if proxy.Protocol == "http" {
		proxy.PublicPort = 80
	} else {
		proxy.PublicPort = 443
		proxy.EnableSSL = true
	}

	if err := db.DB.Save(proxy).Error; err != nil {
		response.Error(c, 500, "更新失败")
		return
	}

	if h.plugin.config.Enabled {
		if err := h.plugin.ApplyConfig(); err != nil {
			logger.Error("应用Nginx配置失败: %v", err)
			response.Error(c, 500, "规则已更新但应用配置失败")
			return
		}
	}

	response.Success(c, proxy)
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
