package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"lxdapi/internal/db"
	"lxdapi/models"
)

func generateAPIKey() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func CreateUser(username, password string, cpuQuota, memoryQuota, diskQuota, maxCPUPerContainer, trafficLimit,
	ipv4PoolLimit, ipv4MappingLimit, ipv6PoolLimit, ipv6MappingLimit, reverseProxyLimit,
	ingress, egress, cpuAllowance, ioRead, ioWrite, processesLimit int,
	allowNesting, memorySwap bool) (*models.User, error) {
	var existing models.User
	if err := db.DB.Where("username = ?", username).First(&existing).Error; err == nil {
		return nil, fmt.Errorf("用户名已存在")
	}

	apiKey := password
	if apiKey == "" {
		var err error
		apiKey, err = generateAPIKey()
		if err != nil {
			return nil, fmt.Errorf("生成密码失败: %v", err)
		}
	}

	user := &models.User{
		Username:           username,
		APIKey:             apiKey,
		Status:             "active",
		CPUQuota:           cpuQuota,
		MemoryQuota:        memoryQuota,
		DiskQuota:          diskQuota,
		MaxCPUPerContainer: maxCPUPerContainer,
		TrafficLimit:       trafficLimit,
		IPv4PoolLimit:      ipv4PoolLimit,
		IPv4MappingLimit:   ipv4MappingLimit,
		IPv6PoolLimit:      ipv6PoolLimit,
		IPv6MappingLimit:   ipv6MappingLimit,
		ReverseProxyLimit:  reverseProxyLimit,
		Ingress:            ingress,
		Egress:             egress,
		CPUAllowance:       cpuAllowance,
		IORead:             ioRead,
		IOWrite:            ioWrite,
		ProcessesLimit:     processesLimit,
		AllowNesting:       allowNesting,
		MemorySwap:         memorySwap,
	}

	if err := db.DB.Create(user).Error; err != nil {
		return nil, err
	}

	return user, nil
}

func GetUser(userID string) (*models.User, error) {
	var user models.User
	if err := db.DB.Where("id = ?", userID).First(&user).Error; err != nil {
		return nil, fmt.Errorf("用户不存在")
	}
	return &user, nil
}

func GetUserByUsername(username string) (*models.User, error) {
	var user models.User
	if err := db.DB.Where("username = ?", username).First(&user).Error; err != nil {
		return nil, fmt.Errorf("用户不存在")
	}
	return &user, nil
}

func GetUserByAPIKey(apiKey string) (*models.User, error) {
	var user models.User
	if err := db.DB.Where("api_key = ? AND status = ?", apiKey, "active").First(&user).Error; err != nil {
		return nil, fmt.Errorf("无效的密码")
	}
	return &user, nil
}

func GetUserByUsernameAndAPIKey(username, apiKey string) (*models.User, error) {
	var user models.User
	if err := db.DB.Where("username = ? AND api_key = ?", username, apiKey).First(&user).Error; err != nil {
		return nil, fmt.Errorf("用户名或密码无效")
	}
	if user.Status != "active" {
		return nil, fmt.Errorf("账户已被禁用，请联系管理员")
	}
	return &user, nil
}

func ListUsers() ([]models.User, error) {
	var users []models.User
	if err := db.DB.Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

func GetUserContainerCount(username string) int64 {
	var count int64
	db.DB.Model(&models.Container{}).Where("user_id = ?", username).Count(&count)
	return count
}

type UserResourceStats struct {
	ContainerCount int64  `json:"container_count"`
	UsedCPU        int    `json:"used_cpu"`
	UsedMemory     int    `json:"used_memory"`
	UsedDisk       int    `json:"used_disk"`
	UsedTraffic    uint64 `json:"used_traffic"`
	UsedIPv4Pool   int64  `json:"used_ipv4_pool"`
	UsedIPv4Map    int64  `json:"used_ipv4_map"`
	UsedIPv6Pool   int64  `json:"used_ipv6_pool"`
	UsedIPv6Map    int64  `json:"used_ipv6_map"`
	UsedProxy      int64  `json:"used_proxy"`
}

func GetUserContainerStats(username string) (count int64, usedCPU, usedMemory, usedDisk int) {
	var containers []models.Container
	db.DB.Where("user_id = ?", username).Find(&containers)
	count = int64(len(containers))
	for _, c := range containers {
		usedCPU += c.CPU
		usedMemory += c.Memory
		usedDisk += c.Disk
	}
	return
}

func GetUserFullStats(username string) UserResourceStats {
	var stats UserResourceStats
	
	var user models.User
	if err := db.DB.Where("username = ?", username).First(&user).Error; err == nil {
		stats.UsedTraffic = uint64(user.TrafficUsed * 1024 * 1024 * 1024)
	}
	
	var containers []models.Container
	db.DB.Where("user_id = ?", username).Find(&containers)
	stats.ContainerCount = int64(len(containers))
	
	containerNames := make([]string, len(containers))
	for i, c := range containers {
		stats.UsedCPU += c.CPU
		stats.UsedMemory += c.Memory
		stats.UsedDisk += c.Disk
		containerNames[i] = c.Name
	}
	
	if len(containerNames) > 0 {
		var traffics []models.Traffic
		db.DB.Where("container_name IN ?", containerNames).Find(&traffics)
		for _, t := range traffics {
			stats.UsedTraffic += uint64(t.TotalGB * 1024 * 1024 * 1024)
		}
		
		var ipv4Count, ipv6Count, proxyCount int64
		db.DB.Model(&models.IPv4Binding{}).Where("container_name IN ?", containerNames).Count(&ipv4Count)
		db.DB.Model(&models.IPv6Binding{}).Where("container_name IN ?", containerNames).Count(&ipv6Count)
		db.DB.Model(&models.ReverseProxy{}).Where("container_name IN ?", containerNames).Count(&proxyCount)
		
		var ipv4Mappings []models.PortMappingV4
		db.DB.Where("container_name IN ?", containerNames).Find(&ipv4Mappings)
		ipv4RuleMap := make(map[string]int)
		for _, m := range ipv4Mappings {
			key := fmt.Sprintf("%d-%d-%d-%s", m.PublicPort, m.PublicPortEnd, m.ContainerPort, m.Protocol)
			if _, exists := ipv4RuleMap[key]; !exists {
				if m.PublicPortEnd > 0 && m.PublicPortEnd != m.PublicPort {
					ipv4RuleMap[key] = m.PublicPortEnd - m.PublicPort + 1
				} else {
					ipv4RuleMap[key] = 1
				}
			}
		}
		ipv4MapCount := 0
		for _, count := range ipv4RuleMap {
			ipv4MapCount += count
		}
		
		var ipv6Mappings []models.PortMappingV6
		db.DB.Where("container_name IN ?", containerNames).Find(&ipv6Mappings)
		ipv6RuleMap := make(map[string]int)
		for _, m := range ipv6Mappings {
			key := fmt.Sprintf("%d-%d-%d-%s", m.PublicPort, m.PublicPortEnd, m.ContainerPort, m.Protocol)
			if _, exists := ipv6RuleMap[key]; !exists {
				if m.PublicPortEnd > 0 && m.PublicPortEnd != m.PublicPort {
					ipv6RuleMap[key] = m.PublicPortEnd - m.PublicPort + 1
				} else {
					ipv6RuleMap[key] = 1
				}
			}
		}
		ipv6MapCount := 0
		for _, count := range ipv6RuleMap {
			ipv6MapCount += count
		}
		
		stats.UsedIPv4Pool = ipv4Count
		stats.UsedIPv6Pool = ipv6Count
		stats.UsedProxy = proxyCount
		stats.UsedIPv4Map = int64(ipv4MapCount)
		stats.UsedIPv6Map = int64(ipv6MapCount)
	}
	
	return stats
}

func GetUserWithContainers(userID string) (*models.User, []models.Container, error) {
	var user models.User
	if err := db.DB.Where("id = ?", userID).First(&user).Error; err != nil {
		return nil, nil, fmt.Errorf("用户不存在")
	}
	
	var containers []models.Container
	if err := db.DB.Where("user_id = ?", user.Username).Find(&containers).Error; err != nil {
		return nil, nil, fmt.Errorf("查询容器失败: %v", err)
	}
	
	return &user, containers, nil
}

func UpdateUser(userID string, updates map[string]interface{}) error {
	var user models.User
	if err := db.DB.Where("id = ?", userID).First(&user).Error; err != nil {
		return fmt.Errorf("用户不存在")
	}

	if newStatus, ok := updates["status"]; ok {
		oldStatus := user.Status
		statusStr := newStatus.(string)

		if oldStatus != statusStr {
			var containers []models.Container
			db.DB.Where("user_id = ?", user.Username).Find(&containers)

			containerService := NewContainerService()
			ctx := context.Background()

			if statusStr == "disabled" {
				for _, c := range containers {
					if c.Status == "running" {
						containerService.PauseWithSource(ctx, c.Name, "system")
					} else if c.Status == "frozen" && c.FreezeSource == "user" {
						db.DB.Model(&models.Container{}).Where("name = ?", c.Name).Update("freeze_source", "system")
					}
				}
			} else if statusStr == "active" && oldStatus == "disabled" {
				for _, c := range containers {
					if c.Status == "frozen" && c.FreezeSource == "system" {
						containerService.Resume(ctx, c.Name)
					}
				}
			}
		}
	}

	if err := db.DB.Model(&user).Updates(updates).Error; err != nil {
		return fmt.Errorf("更新用户失败: %v", err)
	}

	return nil
}

func DeleteUser(userID string) error {
	var user models.User
	if err := db.DB.Where("id = ?", userID).First(&user).Error; err != nil {
		return fmt.Errorf("用户不存在")
	}
	
	var count int64
	db.DB.Model(&models.Container{}).Where("user_id = ?", user.Username).Count(&count)
	if count > 0 {
		return fmt.Errorf("该用户有 %d 个容器，无法删除。请先删除或转移该用户的所有容器", count)
	}
	
	result := db.DB.Unscoped().Where("id = ?", userID).Delete(&models.User{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("用户不存在")
	}
	return nil
}

func GetOrCreateUser(username string) (*models.User, error) {
	var user models.User
	
	err := db.DB.Where("username = ?", username).First(&user).Error
	if err == nil {
		return &user, nil
	}
	
	apiKey, err := generateAPIKey()
	if err != nil {
		return nil, fmt.Errorf("生成密码失败: %v", err)
	}

	user = models.User{
		Username: username,
		APIKey:   apiKey,
		Status:   "active",
	}

	if err := db.DB.Create(&user).Error; err != nil {
		return nil, fmt.Errorf("创建用户失败: %v", err)
	}

	return &user, nil
}

func RegenerateAPIKey(userID, password string) (*models.User, error) {
	var user models.User
	if err := db.DB.Where("id = ?", userID).First(&user).Error; err != nil {
		return nil, fmt.Errorf("用户不存在")
	}

	apiKey := password
	if apiKey == "" {
		var err error
		apiKey, err = generateAPIKey()
		if err != nil {
			return nil, fmt.Errorf("生成密码失败: %v", err)
		}
	}

	user.APIKey = apiKey
	if err := db.DB.Save(&user).Error; err != nil {
		return nil, err
	}

	return &user, nil
}

