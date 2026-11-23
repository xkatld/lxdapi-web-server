package service

import (
	"context"
	"fmt"
	"math/rand"
	"lxdapi/internal/db"
	"lxdapi/internal/ipv4"
	"lxdapi/internal/ipv6"
	"lxdapi/internal/lxc"
	"lxdapi/models"
	"lxdapi/pkg/logger"
)

const MaxPortMappingCount = 65535

type PortMappingService struct {
	lxcClient *lxc.Client
}

func NewPortMappingService() *PortMappingService {
	return &PortMappingService{
		lxcClient: lxc.NewClient(),
	}
}

func ValidatePortMappingProtocol(protocol string) error {
	if protocol != "tcp" && protocol != "udp" && protocol != "both" {
		return fmt.Errorf("协议必须是tcp、udp或both")
	}
	return nil
}

func NormalizeAndValidatePortCount(portCount int) (int, error) {
	if portCount < 0 {
		return 0, fmt.Errorf("端口数量不能小于1")
	}
	if portCount == 0 {
		portCount = 1
	}
	if portCount > MaxPortMappingCount {
		return 0, fmt.Errorf("端口数量不能超过%d", MaxPortMappingCount)
	}
	return portCount, nil
}

func ValidatePortRange(port, portCount int, label string) (int, error) {
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("%s必须在 1-65535 范围内", label)
	}
	if portCount < 1 || portCount > MaxPortMappingCount {
		return 0, fmt.Errorf("端口数量必须在 1-%d 范围内", MaxPortMappingCount)
	}
	endPort := port + portCount - 1
	if endPort < 1 || endPort > 65535 {
		return 0, fmt.Errorf("%s结束端口必须在 1-65535 范围内", label)
	}
	return endPort, nil
}

func ValidatePublicPortRange(publicPort, portCount, rangeStart, rangeEnd int) (int, error) {
	publicPortEnd, err := ValidatePortRange(publicPort, portCount, "公网端口")
	if err != nil {
		return 0, fmt.Errorf("公网端口范围必须在 %d-%d 且在 1-65535 范围内", rangeStart, rangeEnd)
	}
	if publicPort < rangeStart || publicPortEnd > rangeEnd {
		return 0, fmt.Errorf("公网端口范围必须在 %d-%d 且在 1-65535 范围内", rangeStart, rangeEnd)
	}
	return publicPortEnd, nil
}

func (s *PortMappingService) CheckV4PortRangeAvailable(publicIP string, publicPort, publicPortEnd int, protocol string) error {
	var existingMappings []models.PortMappingV4
	
	protocols := []string{}
	if protocol == "tcp" || protocol == "both" {
		protocols = append(protocols, "tcp", "both")
	}
	if protocol == "udp" || protocol == "both" {
		protocols = append(protocols, "udp", "both")
	}
	
	if err := db.DB.Where("public_ip = ? AND protocol IN (?)", publicIP, protocols).Find(&existingMappings).Error; err != nil {
		return fmt.Errorf("查询端口映射失败: %v", err)
	}
	
	for _, existing := range existingMappings {
		existingEnd := existing.PublicPortEnd
		if existingEnd == 0 {
			existingEnd = existing.PublicPort
		}
		
		if publicPort <= existingEnd && publicPortEnd >= existing.PublicPort {
			if existingEnd == existing.PublicPort {
				return fmt.Errorf("端口 %s:%d 已被占用", publicIP, existing.PublicPort)
			} else {
				return fmt.Errorf("端口段 %s:%d-%d 与已有端口段 %d-%d 冲突", publicIP, publicPort, publicPortEnd, existing.PublicPort, existingEnd)
			}
		}
	}
	
	return nil
}

func (s *PortMappingService) FindAvailableV4Port(portStart, portEnd, portCount int, protocol string) (int, error) {
	var existingMappings []models.PortMappingV4
	protocols := []string{}
	if protocol == "tcp" || protocol == "both" {
		protocols = append(protocols, "tcp", "both")
	}
	if protocol == "udp" || protocol == "both" {
		protocols = append(protocols, "udp", "both")
	}
	
	if err := db.DB.Where("protocol IN (?)", protocols).Find(&existingMappings).Error; err != nil {
		return 0, fmt.Errorf("查询端口映射失败: %v", err)
	}
	
	usedPorts := make(map[int]bool)
	for _, m := range existingMappings {
		end := m.PublicPortEnd
		if end == 0 {
			end = m.PublicPort
		}
		for p := m.PublicPort; p <= end; p++ {
			usedPorts[p] = true
		}
	}
	
	rangeSize := portEnd - portStart - portCount + 2
	if rangeSize <= 0 {
		return 0, fmt.Errorf("端口范围不足")
	}
	
	for attempt := 0; attempt < 100; attempt++ {
		startPort := portStart + rand.Intn(rangeSize)
		available := true
		for p := startPort; p < startPort+portCount; p++ {
			if usedPorts[p] {
				available = false
				break
			}
		}
		if available {
			return startPort, nil
		}
	}
	
	return 0, fmt.Errorf("无法找到可用的连续端口段")
}

func (s *PortMappingService) FindAvailableV6Port(portStart, portEnd, portCount int, protocol string) (int, error) {
	var existingMappings []models.PortMappingV6
	protocols := []string{}
	if protocol == "tcp" || protocol == "both" {
		protocols = append(protocols, "tcp", "both")
	}
	if protocol == "udp" || protocol == "both" {
		protocols = append(protocols, "udp", "both")
	}
	
	if err := db.DB.Where("protocol IN (?)", protocols).Find(&existingMappings).Error; err != nil {
		return 0, fmt.Errorf("查询端口映射失败: %v", err)
	}
	
	usedPorts := make(map[int]bool)
	for _, m := range existingMappings {
		end := m.PublicPortEnd
		if end == 0 {
			end = m.PublicPort
		}
		for p := m.PublicPort; p <= end; p++ {
			usedPorts[p] = true
		}
	}
	
	rangeSize := portEnd - portStart - portCount + 2
	if rangeSize <= 0 {
		return 0, fmt.Errorf("端口范围不足")
	}
	
	for attempt := 0; attempt < 100; attempt++ {
		startPort := portStart + rand.Intn(rangeSize)
		available := true
		for p := startPort; p < startPort+portCount; p++ {
			if usedPorts[p] {
				available = false
				break
			}
		}
		if available {
			return startPort, nil
		}
	}
	
	return 0, fmt.Errorf("无法找到可用的连续端口段")
}

func (s *PortMappingService) CheckV6PortRangeAvailable(publicIP string, publicPort, publicPortEnd int, protocol string) error {
	var existingMappings []models.PortMappingV6
	
	protocols := []string{}
	if protocol == "tcp" || protocol == "both" {
		protocols = append(protocols, "tcp", "both")
	}
	if protocol == "udp" || protocol == "both" {
		protocols = append(protocols, "udp", "both")
	}
	
	if err := db.DB.Where("public_ip = ? AND protocol IN (?)", publicIP, protocols).Find(&existingMappings).Error; err != nil {
		return fmt.Errorf("查询端口映射失败: %v", err)
	}
	
	for _, existing := range existingMappings {
		existingEnd := existing.PublicPortEnd
		if existingEnd == 0 {
			existingEnd = existing.PublicPort
		}
		
		if publicPort <= existingEnd && publicPortEnd >= existing.PublicPort {
			if existingEnd == existing.PublicPort {
				return fmt.Errorf("端口 %s:%d 已被占用", publicIP, existing.PublicPort)
			} else {
				return fmt.Errorf("端口段 %s:%d-%d 与已有端口段 %d-%d 冲突", publicIP, publicPort, publicPortEnd, existing.PublicPort, existingEnd)
			}
		}
	}
	
	return nil
}

func (s *PortMappingService) AllocateV4Mapping(ctx context.Context, containerName, userID string, forwardIP string, displayIP string, publicPort, publicPortEnd, containerPort, containerPortEnd int, protocol, iface, description string) (*models.PortMappingV4, error) {
	if err := ValidatePortMappingProtocol(protocol); err != nil {
		return nil, err
	}
	if _, err := ValidatePortRange(publicPort, publicPortEnd-publicPort+1, "公网端口"); err != nil {
		return nil, err
	}
	if _, err := ValidatePortRange(containerPort, containerPortEnd-containerPort+1, "容器端口"); err != nil {
		return nil, err
	}
	if iface == "" {
		return nil, fmt.Errorf("必须指定网卡")
	}
	
	if forwardIP == "" {
		return nil, fmt.Errorf("必须指定转发IP")
	}
	
	if displayIP == "" {
		return nil, fmt.Errorf("必须指定公示IP")
	}
	
	var container models.Container
	if err := db.DB.Where("name = ?", containerName).First(&container).Error; err != nil {
		return nil, fmt.Errorf("容器不存在: %v", err)
	}
	
	containerIP := container.PrivateIP
	if containerIP == "" {
		return nil, fmt.Errorf("容器IP为空，请等待容器启动完成")
	}
	
	mapping := &models.PortMappingV4{
		ForwardIP:        forwardIP,
		PublicIP:         displayIP,
		PublicPort:       publicPort,
		PublicPortEnd:    publicPortEnd,
		ContainerName:    containerName,
		ContainerIP:      containerIP,
		ContainerPort:    containerPort,
		ContainerPortEnd: containerPortEnd,
		Protocol:         protocol,
		Status:           "active",
		UserID:           userID,
		Interface:        iface,
		Description:      description,
	}
	
	if err := db.DB.Create(mapping).Error; err != nil {
		return nil, fmt.Errorf("保存端口映射失败: %v", err)
	}
	
	if ipv4.GlobalManager != nil {
		if err := ipv4.GlobalManager.AddPortMapping(forwardIP, publicPort, publicPortEnd, containerIP, containerPort, containerPortEnd, protocol, iface); err != nil {
			db.DB.Unscoped().Delete(mapping)
			return nil, fmt.Errorf("添加nftables规则失败: %v", err)
		}
	}
	
	logger.OK("IPv4端口映射创建成功: %s:%d -> %s:%d (%s)", displayIP, publicPort, containerIP, containerPort, protocol)
	return mapping, nil
}

func (s *PortMappingService) AllocateV6Mapping(ctx context.Context, containerName, userID string, forwardIP string, displayIP string, publicPort, publicPortEnd, containerPort, containerPortEnd int, protocol, iface, description string) (*models.PortMappingV6, error) {
	if err := ValidatePortMappingProtocol(protocol); err != nil {
		return nil, err
	}
	if _, err := ValidatePortRange(publicPort, publicPortEnd-publicPort+1, "公网端口"); err != nil {
		return nil, err
	}
	if _, err := ValidatePortRange(containerPort, containerPortEnd-containerPort+1, "容器端口"); err != nil {
		return nil, err
	}
	if iface == "" {
		return nil, fmt.Errorf("必须指定网卡")
	}
	
	if forwardIP == "" {
		return nil, fmt.Errorf("必须指定转发IPv6")
	}
	
	if displayIP == "" {
		return nil, fmt.Errorf("必须指定公示IPv6")
	}
	
	var container models.Container
	if err := db.DB.Where("name = ?", containerName).First(&container).Error; err != nil {
		return nil, fmt.Errorf("容器不存在: %v", err)
	}
	
	containerIP := container.PrivateIPv6
	if containerIP == "" {
		return nil, fmt.Errorf("容器IPv6为空，请等待容器启动完成")
	}
	
	mapping := &models.PortMappingV6{
		ForwardIP:        forwardIP,
		PublicIP:         displayIP,
		PublicPort:       publicPort,
		PublicPortEnd:    publicPortEnd,
		ContainerName:    containerName,
		ContainerIP:      containerIP,
		ContainerPort:    containerPort,
		ContainerPortEnd: containerPortEnd,
		Protocol:         protocol,
		Status:           "active",
		UserID:           userID,
		Interface:        iface,
		Description:      description,
	}
	
	if err := db.DB.Create(mapping).Error; err != nil {
		return nil, fmt.Errorf("保存IPv6端口映射失败: %v", err)
	}
	
	if ipv6.GlobalManager != nil {
		if err := ipv6.GlobalManager.AddPortMapping(forwardIP, publicPort, publicPortEnd, containerIP, containerPort, containerPortEnd, protocol, iface); err != nil {
			db.DB.Unscoped().Delete(mapping)
			return nil, fmt.Errorf("添加nftables规则失败: %v", err)
		}
	}
	
	logger.OK("IPv6端口映射创建成功: %s:%d -> %s:%d (%s)", displayIP, publicPort, containerIP, containerPort, protocol)
	return mapping, nil
}

func (s *PortMappingService) ReleaseV4Mapping(id uint) error {
	var mapping models.PortMappingV4
	if err := db.DB.First(&mapping, id).Error; err != nil {
		return fmt.Errorf("端口映射不存在: %v", err)
	}
	
	if ipv4.GlobalManager != nil {
		forwardIP := mapping.ForwardIP
		if forwardIP == "" {
			forwardIP = mapping.PublicIP
		}
		publicPortEnd := mapping.PublicPortEnd
		if publicPortEnd == 0 {
			publicPortEnd = mapping.PublicPort
		}
		containerPortEnd := mapping.ContainerPortEnd
		if containerPortEnd == 0 {
			containerPortEnd = mapping.ContainerPort
		}
		if err := ipv4.GlobalManager.RemovePortMapping(forwardIP, mapping.PublicPort, publicPortEnd, mapping.ContainerIP, mapping.ContainerPort, containerPortEnd, mapping.Protocol, mapping.Interface); err != nil {
			return fmt.Errorf("删除nftables规则失败: %v", err)
		}
	}
	
	if err := db.DB.Unscoped().Delete(&mapping).Error; err != nil {
		return fmt.Errorf("删除端口映射记录失败: %v", err)
	}
	
	logger.OK("IPv4端口映射释放成功: %s:%d -> %s:%d", mapping.PublicIP, mapping.PublicPort, mapping.ContainerIP, mapping.ContainerPort)
	return nil
}

func (s *PortMappingService) ReleaseV6Mapping(id uint) error {
	var mapping models.PortMappingV6
	if err := db.DB.First(&mapping, id).Error; err != nil {
		return fmt.Errorf("端口映射不存在: %v", err)
	}
	
	if ipv6.GlobalManager != nil {
		forwardIP := mapping.ForwardIP
		if forwardIP == "" {
			forwardIP = mapping.PublicIP
		}
		publicPortEnd := mapping.PublicPortEnd
		if publicPortEnd == 0 {
			publicPortEnd = mapping.PublicPort
		}
		containerPortEnd := mapping.ContainerPortEnd
		if containerPortEnd == 0 {
			containerPortEnd = mapping.ContainerPort
		}
		if err := ipv6.GlobalManager.RemovePortMapping(forwardIP, mapping.PublicPort, publicPortEnd, mapping.ContainerIP, mapping.ContainerPort, containerPortEnd, mapping.Protocol, mapping.Interface); err != nil {
			return fmt.Errorf("删除nftables规则失败: %v", err)
		}
	}
	
	if err := db.DB.Unscoped().Delete(&mapping).Error; err != nil {
		return fmt.Errorf("删除IPv6端口映射记录失败: %v", err)
	}
	
	logger.OK("IPv6端口映射释放成功: %s:%d -> %s:%d", mapping.PublicIP, mapping.PublicPort, mapping.ContainerIP, mapping.ContainerPort)
	return nil
}

func (s *PortMappingService) ListV4Mappings(containerName string) ([]models.PortMappingV4, error) {
	var mappings []models.PortMappingV4
	query := db.DB.Order("created_at DESC")
	
	if containerName != "" {
		query = query.Where("container_name = ?", containerName)
	}
	
	if err := query.Find(&mappings).Error; err != nil {
		return nil, err
	}
	
	return mappings, nil
}

func (s *PortMappingService) ListV6Mappings(containerName string) ([]models.PortMappingV6, error) {
	var mappings []models.PortMappingV6
	query := db.DB.Order("created_at DESC")
	
	if containerName != "" {
		query = query.Where("container_name = ?", containerName)
	}
	
	if err := query.Find(&mappings).Error; err != nil {
		return nil, err
	}
	
	return mappings, nil
}
