package admin

import (
	"context"
	"fmt"
	"net"
	"regexp"

	"github.com/gin-gonic/gin"
	"lxdapi/internal/db"
	"lxdapi/internal/ipv4"
	"lxdapi/internal/ipv6"
	"lxdapi/internal/service"
	"lxdapi/models"
	"lxdapi/pkg/logger"
	"lxdapi/pkg/response"
)

// GetIPList 获取IP绑定列表（统一接口）
var ifaceNameRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,14}$`)

func isValidInterfaceName(name string) bool {
	return ifaceNameRegex.MatchString(name)
}

func isValidIPByVersion(ipStr, version string) bool {
	parsed := net.ParseIP(ipStr)
	if parsed == nil {
		return false
	}
	if version == "v4" {
		return parsed.To4() != nil
	}
	return parsed.To4() == nil
}

func GetIPList(c *gin.Context) {
	version := c.DefaultQuery("version", "all")
	result := gin.H{}

	if version == "v4" || version == "all" {
		var bindings []models.IPv4Binding
		db.DB.Find(&bindings)
		result["v4"] = bindings
		result["v4_count"] = len(bindings)
	}

	if version == "v6" || version == "all" {
		var bindings []models.IPv6Binding
		db.DB.Find(&bindings)
		result["v6"] = bindings
		result["v6_count"] = len(bindings)
	}

	response.Success(c, result)
}

// AllocateIP 分配IP（统一接口）
func AllocateIP(c *gin.Context) {
	version := c.Query("version")
	if version != "v4" && version != "v6" {
		response.Error(c, 400, "version参数必须是v4或v6")
		return
	}

	var req struct {
		Name  string `json:"name" binding:"required"`
		Count int    `json:"count"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}

	if req.Count <= 0 {
		req.Count = 1
	}

	var container models.Container
	if err := db.DB.Where("name = ?", req.Name).First(&container).Error; err != nil {
		response.Error(c, 404, "容器不存在")
		return
	}

	ctx := context.Background()

	if version == "v4" {
		if ipv4.GlobalManager == nil {
			response.Error(c, 500, "IPv4功能未启用")
			return
		}
		ipv4Svc := service.NewIPv4Service()
		ips, err := ipv4Svc.AllocateIPv4(ctx, req.Name, container.UserID, req.Count)
		if err != nil {
			logger.Error("分配IPv4失败: %v", err)
			response.Error(c, 500, "分配IPv4失败")
			return
		}
		logger.OK("容器 %s 分配IPv4成功: %v", req.Name, ips)
		response.Success(c, gin.H{
			"container": req.Name,
			"ips":       ips,
			"count":     len(ips),
		})
	} else {
		if ipv6.GlobalManager == nil {
			response.Error(c, 500, "IPv6功能未启用")
			return
		}
		ipv6Svc := service.NewIPv6Service()
		ips, err := ipv6Svc.AllocateIPv6(ctx, req.Name, container.UserID, req.Count)
		if err != nil {
			logger.Error("分配IPv6失败: %v", err)
			response.Error(c, 500, "分配IPv6失败")
			return
		}
		logger.OK("容器 %s 分配IPv6成功: %v", req.Name, ips)
		response.Success(c, gin.H{
			"container": req.Name,
			"ips":       ips,
			"count":     len(ips),
		})
	}
}

// ReleaseIP 释放IP（统一接口）
func ReleaseIP(c *gin.Context) {
	version := c.Query("version")
	if version != "v4" && version != "v6" {
		response.Error(c, 400, "version参数必须是v4或v6")
		return
	}

	var req struct {
		Name string `json:"name" binding:"required"`
		IP   string `json:"ip" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}

	if version == "v4" {
		if ipv4.GlobalManager == nil {
			response.Error(c, 500, "IPv4功能未启用")
			return
		}
		ipv4Svc := service.NewIPv4Service()
		if err := ipv4Svc.ReleaseIPv4(req.Name, req.IP); err != nil {
			logger.Error("释放IPv4失败: %v", err)
			response.Error(c, 500, "释放IPv4失败")
			return
		}
		logger.OK("容器 %s 释放IPv4成功: %s", req.Name, req.IP)
		response.Success(c, "释放成功")
	} else {
		if ipv6.GlobalManager == nil {
			response.Error(c, 500, "IPv6功能未启用")
			return
		}
		ipv6Svc := service.NewIPv6Service()
		if err := ipv6Svc.ReleaseIPv6(req.Name, req.IP); err != nil {
			logger.Error("释放IPv6失败: %v", err)
			response.Error(c, 500, "释放IPv6失败")
			return
		}
		logger.OK("容器 %s 释放IPv6成功: %s", req.Name, req.IP)
		response.Success(c, "释放成功")
	}
}

// GetIPPool 获取IP地址池（统一接口）
func GetIPPool(c *gin.Context) {
	version := c.Query("version")
	if version != "v4" && version != "v6" {
		response.Error(c, 400, "version参数必须是v4或v6")
		return
	}

	if version == "v4" {
		var pools []models.IPv4Pool
		var total int64
		db.DB.Model(&models.IPv4Pool{}).Count(&total)
		db.DB.Order("ip_address").Find(&pools)
		response.Success(c, gin.H{
			"pools": pools,
			"total": total,
		})
	} else {
		var pools []models.IPv6Pool
		var total int64
		db.DB.Model(&models.IPv6Pool{}).Count(&total)
		db.DB.Order("ip_address").Find(&pools)
		response.Success(c, gin.H{
			"pools": pools,
			"total": total,
		})
	}
}

// AddIPToPool 添加IP到地址池（统一接口）
func AddIPToPool(c *gin.Context) {
	version := c.Query("version")
	if version != "v4" && version != "v6" {
		response.Error(c, 400, "version参数必须是v4或v6")
		return
	}

	var req struct {
		IPAddress string `json:"ip_address" binding:"required"`
		Interface string `json:"interface" binding:"required"`
		Note      string `json:"note"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}

	if !isValidInterfaceName(req.Interface) {
		response.Error(c, 400, "无效的网卡名称")
		return
	}

	if !isValidIPByVersion(req.IPAddress, version) {
		response.Error(c, 400, "无效的IP地址")
		return
	}

	if version == "v4" {
		var existing models.IPv4Pool
		if err := db.DB.Where("ip_address = ?", req.IPAddress).First(&existing).Error; err == nil {
			response.Error(c, 400, "IP地址已存在")
			return
		}

		pool := &models.IPv4Pool{
			IPAddress: req.IPAddress,
			Interface: req.Interface,
			Status:    "available",
			Note:      req.Note,
		}
		if err := db.DB.Create(pool).Error; err != nil {
			logger.Error("添加IPv4到池失败: %v", err)
			response.Error(c, 500, "添加失败")
			return
		}
		logger.OK("添加IPv4到池: %s (%s)", req.IPAddress, req.Interface)
		response.Success(c, pool)
	} else {
		var existing models.IPv6Pool
		if err := db.DB.Where("ip_address = ?", req.IPAddress).First(&existing).Error; err == nil {
			response.Error(c, 400, "IP地址已存在")
			return
		}

		pool := &models.IPv6Pool{
			IPAddress: req.IPAddress,
			Interface: req.Interface,
			Status:    "available",
			Note:      req.Note,
		}
		if err := db.DB.Create(pool).Error; err != nil {
			logger.Error("添加IPv6到池失败: %v", err)
			response.Error(c, 500, "添加失败")
			return
		}
		logger.OK("添加IPv6到池: %s (%s)", req.IPAddress, req.Interface)
		response.Success(c, pool)
	}
}

// UpdateIPPool 更新IP池备注（统一接口）
func UpdateIPPool(c *gin.Context) {
	version := c.Query("version")
	if version != "v4" && version != "v6" {
		response.Error(c, 400, "version参数必须是v4或v6")
		return
	}

	var req struct {
		IPAddress string `json:"ip_address" binding:"required"`
		Note      string `json:"note"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}

	if version == "v4" {
		var pool models.IPv4Pool
		if err := db.DB.Where("ip_address = ?", req.IPAddress).First(&pool).Error; err != nil {
			response.Error(c, 404, "IP不存在")
			return
		}
		pool.Note = req.Note
		if err := db.DB.Save(&pool).Error; err != nil {
			response.Error(c, 500, "更新失败")
			return
		}
		response.Success(c, pool)
	} else {
		var pool models.IPv6Pool
		if err := db.DB.Where("ip_address = ?", req.IPAddress).First(&pool).Error; err != nil {
			response.Error(c, 404, "IP不存在")
			return
		}
		pool.Note = req.Note
		if err := db.DB.Save(&pool).Error; err != nil {
			response.Error(c, 500, "更新失败")
			return
		}
		response.Success(c, pool)
	}
}

// DeleteIPFromPool 从地址池删除IP（统一接口）
func DeleteIPFromPool(c *gin.Context) {
	version := c.Query("version")
	if version != "v4" && version != "v6" {
		response.Error(c, 400, "version参数必须是v4或v6")
		return
	}

	var req struct {
		IPAddress string `json:"ip_address" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}

	if version == "v4" {
		var pool models.IPv4Pool
		if err := db.DB.Where("ip_address = ?", req.IPAddress).First(&pool).Error; err != nil {
			response.Error(c, 404, "IP不存在")
			return
		}
		if pool.Status == "used" {
			response.Error(c, 400, "IP正在使用中，无法删除")
			return
		}
		if err := db.DB.Unscoped().Delete(&pool).Error; err != nil {
			response.Error(c, 500, "删除失败")
			return
		}
		logger.OK("从IPv4池删除: %s", req.IPAddress)
		response.Success(c, "删除成功")
	} else {
		var pool models.IPv6Pool
		if err := db.DB.Where("ip_address = ?", req.IPAddress).First(&pool).Error; err != nil {
			response.Error(c, 404, "IP不存在")
			return
		}
		if pool.Status == "used" {
			response.Error(c, 400, "IP正在使用中，无法删除")
			return
		}
		if err := db.DB.Unscoped().Delete(&pool).Error; err != nil {
			response.Error(c, 500, "删除失败")
			return
		}
		logger.OK("从IPv6池删除: %s", req.IPAddress)
		response.Success(c, "删除成功")
	}
}

func BatchDeleteIPFromPool(c *gin.Context) {
	version := c.Query("version")
	if version != "v4" && version != "v6" {
		response.Error(c, 400, "version参数必须是v4或v6")
		return
	}

	var req struct {
		IPAddresses []string `json:"ip_addresses" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}

	if len(req.IPAddresses) == 0 {
		response.Error(c, 400, "请选择要删除的IP")
		return
	}

	var deleted, skipped int
	if version == "v4" {
		for _, ip := range req.IPAddresses {
			var pool models.IPv4Pool
			if err := db.DB.Where("ip_address = ? AND status != ?", ip, "used").First(&pool).Error; err == nil {
				db.DB.Unscoped().Delete(&pool)
				deleted++
			} else {
				skipped++
			}
		}
	} else {
		for _, ip := range req.IPAddresses {
			var pool models.IPv6Pool
			if err := db.DB.Where("ip_address = ? AND status != ?", ip, "used").First(&pool).Error; err == nil {
				db.DB.Unscoped().Delete(&pool)
				deleted++
			} else {
				skipped++
			}
		}
	}

	logger.OK("批量删除IP: 成功%d, 跳过%d", deleted, skipped)
	response.Success(c, gin.H{"deleted": deleted, "skipped": skipped})
}

// BatchAddIPToPool 批量添加IP到地址池（统一接口）
func BatchAddIPToPool(c *gin.Context) {
	version := c.Query("version")
	if version != "v4" && version != "v6" {
		response.Error(c, 400, "version参数必须是v4或v6")
		return
	}

	var req struct {
		StartIP   string `json:"start_ip" binding:"required"`
		EndIP     string `json:"end_ip"`
		Count     int    `json:"count"`
		Interface string `json:"interface" binding:"required"`
		Note      string `json:"note"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}

	if !isValidInterfaceName(req.Interface) {
		response.Error(c, 400, "无效的网卡名称")
		return
	}

	var ips []string
	var err error

	if version == "v4" {
		if req.EndIP == "" {
			response.Error(c, 400, "v4批量添加需要提供end_ip")
			return
		}
		ips, err = generateIPv4Range(req.StartIP, req.EndIP)
		if err != nil {
			logger.Error("生成IPv4范围失败: %v", err)
			response.Error(c, 400, "IP范围参数无效")
			return
		}
	} else {
		if req.Count <= 0 {
			response.Error(c, 400, "v6批量添加需要提供count")
			return
		}
		ips, err = generateIPv6List(req.StartIP, req.Count)
		if err != nil {
			logger.Error("生成IPv6列表失败: %v", err)
			response.Error(c, 400, "IP范围参数无效")
			return
		}
	}

	if len(ips) == 0 {
		response.Error(c, 400, "未生成有效的IP地址")
		return
	}

	added := 0
	skipped := 0

	if version == "v4" {
		for _, ip := range ips {
			var existing models.IPv4Pool
			if db.DB.Where("ip_address = ?", ip).First(&existing).Error == nil {
				skipped++
				continue
			}
			pool := &models.IPv4Pool{
				IPAddress: ip,
				Interface: req.Interface,
				Status:    "available",
				Note:      req.Note,
			}
			if err := db.DB.Create(pool).Error; err == nil {
				added++
			}
		}
	} else {
		for _, ip := range ips {
			var existing models.IPv6Pool
			if db.DB.Where("ip_address = ?", ip).First(&existing).Error == nil {
				skipped++
				continue
			}
			pool := &models.IPv6Pool{
				IPAddress: ip,
				Interface: req.Interface,
				Status:    "available",
				Note:      req.Note,
			}
			if err := db.DB.Create(pool).Error; err == nil {
				added++
			}
		}
	}

	logger.OK("批量添加IP完成: 添加 %d，跳过 %d", added, skipped)
	response.Success(c, gin.H{
		"added":   added,
		"skipped": skipped,
		"total":   len(ips),
		"message": "批量添加完成",
	})
}

func generateIPv4Range(startIP, endIP string) ([]string, error) {
	start := net.ParseIP(startIP).To4()
	end := net.ParseIP(endIP).To4()

	if start == nil || end == nil {
		return nil, fmt.Errorf("无效的IPv4地址")
	}

	startInt := ipv4ToInt(start)
	endInt := ipv4ToInt(end)

	if startInt > endInt {
		return nil, fmt.Errorf("起始IP不能大于结束IP")
	}

	if endInt-startInt > 10000 {
		return nil, fmt.Errorf("IP范围过大，最多支持10000个")
	}

	var ips []string
	for i := startInt; i <= endInt; i++ {
		ips = append(ips, intToIPv4(i).String())
	}
	return ips, nil
}

func ipv4ToInt(ip net.IP) uint32 {
	return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
}

func intToIPv4(n uint32) net.IP {
	return net.IPv4(byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
}

func generateIPv6List(startIP string, count int) ([]string, error) {
	ip := net.ParseIP(startIP)
	if ip == nil {
		return nil, fmt.Errorf("无效的IPv6地址")
	}

	ip = ip.To16()
	if ip == nil {
		return nil, fmt.Errorf("无效的IPv6地址")
	}

	if count > 10000 {
		return nil, fmt.Errorf("数量过大，最多支持10000个")
	}

	var ips []string
	for i := 0; i < count; i++ {
		ips = append(ips, ip.String())
		incrementIPv6(ip)
	}
	return ips, nil
}

func incrementIPv6(ip net.IP) {
	for i := 15; i >= 0; i-- {
		ip[i]++
		if ip[i] != 0 {
			break
		}
	}
}

func GetIPPoolSettings(c *gin.Context) {
	var settings models.IPPoolSettings
	result := db.DB.First(&settings)
	if result.Error != nil {
		settings = models.IPPoolSettings{
			RandomAssign: false,
			AutoAssign:   false,
		}
		db.DB.Create(&settings)
	}
	response.Success(c, settings)
}

func GetIPPoolSettingsPublic(c *gin.Context) {
	var settings models.IPPoolSettings
	result := db.DB.First(&settings)
	if result.Error != nil {
		settings = models.IPPoolSettings{}
	}
	response.Success(c, gin.H{
		"allow_user_release_ipv4":      settings.AllowUserReleaseIPv4,
		"allow_user_release_ipv6":      settings.AllowUserReleaseIPv6,
		"allow_container_release_ipv4": settings.AllowContainerReleaseIPv4,
		"allow_container_release_ipv6": settings.AllowContainerReleaseIPv6,
	})
}

func UpdateIPPoolSettings(c *gin.Context) {
	var req struct {
		RandomAssign              bool `json:"random_assign"`
		AutoAssign                bool `json:"auto_assign"`
		AllowUserReleaseIPv4      bool `json:"allow_user_release_ipv4"`
		AllowUserReleaseIPv6      bool `json:"allow_user_release_ipv6"`
		AllowContainerReleaseIPv4 bool `json:"allow_container_release_ipv4"`
		AllowContainerReleaseIPv6 bool `json:"allow_container_release_ipv6"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}

	var settings models.IPPoolSettings
	result := db.DB.First(&settings)
	if result.Error != nil {
		settings = models.IPPoolSettings{
			RandomAssign:              req.RandomAssign,
			AutoAssign:                req.AutoAssign,
			AllowUserReleaseIPv4:      req.AllowUserReleaseIPv4,
			AllowUserReleaseIPv6:      req.AllowUserReleaseIPv6,
			AllowContainerReleaseIPv4: req.AllowContainerReleaseIPv4,
			AllowContainerReleaseIPv6: req.AllowContainerReleaseIPv6,
		}
		db.DB.Create(&settings)
	} else {
		settings.RandomAssign = req.RandomAssign
		settings.AutoAssign = req.AutoAssign
		settings.AllowUserReleaseIPv4 = req.AllowUserReleaseIPv4
		settings.AllowUserReleaseIPv6 = req.AllowUserReleaseIPv6
		settings.AllowContainerReleaseIPv4 = req.AllowContainerReleaseIPv4
		settings.AllowContainerReleaseIPv6 = req.AllowContainerReleaseIPv6
		db.DB.Save(&settings)
	}

	logger.OK("IP池设置已更新")
	response.Success(c, settings)
}
