package service

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"lxdapi/internal/cache"
	"lxdapi/internal/db"
	"lxdapi/internal/ipv4"
	"lxdapi/internal/ipv6"
	"lxdapi/internal/lxc"
	"lxdapi/models"
	"lxdapi/pkg/logger"
	"lxdapi/pkg/plugin"
	"math/big"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

type ContainerService struct {
	lxcClient *lxc.Client
}

func NewContainerService() *ContainerService {
	return &ContainerService{
		lxcClient: lxc.NewClient(),
	}
}

func (s *ContainerService) GetLXCClient() *lxc.Client {
	return s.lxcClient
}

func generatePassword() string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*"
	const length = 16
	
	password := make([]byte, length)
	
	upper := "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	lower := "abcdefghijklmnopqrstuvwxyz"
	digits := "0123456789"
	special := "!@#$%^&*"
	
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(upper))))
	password[0] = upper[n.Int64()]
	
	n, _ = rand.Int(rand.Reader, big.NewInt(int64(len(lower))))
	password[1] = lower[n.Int64()]
	
	n, _ = rand.Int(rand.Reader, big.NewInt(int64(len(digits))))
	password[2] = digits[n.Int64()]
	
	n, _ = rand.Int(rand.Reader, big.NewInt(int64(len(special))))
	password[3] = special[n.Int64()]
	
	for i := 4; i < length; i++ {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		password[i] = charset[n.Int64()]
	}
	
	for i := length - 1; i > 0; i-- {
		j, _ := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		password[i], password[j.Int64()] = password[j.Int64()], password[i]
	}
	
	return string(password)
}

func checkTemplateUserPermission(template *models.Template, username string) (int, string) {
	if template.AllowedUsers != "" && template.AllowedUsers != "[]" {
		var allowedUsers []string
		if err := json.Unmarshal([]byte(template.AllowedUsers), &allowedUsers); err == nil && len(allowedUsers) > 0 {
			for _, u := range allowedUsers {
				if u == username {
					return 0, ""
				}
			}
			return 403, "无权使用该模板"
		}
	}
	return 0, ""
}

func preferredTemplateIdentifier(template *models.Template, fallback string) string {
	if template != nil {
		if template.Alias != "" && lxc.ValidateImageName(template.Alias) == nil {
			return template.Alias
		}
		if template.Fingerprint != "" && lxc.ValidateImageName(template.Fingerprint) == nil {
			return template.Fingerprint
		}
	}
	return fallback
}

func resolveStoredImageIdentifier(image string) string {
	var template models.Template
	if err := db.DB.Where("fingerprint = ? OR alias = ?", image, image).First(&template).Error; err == nil {
		return preferredTemplateIdentifier(&template, image)
	}
	return image
}

func resolveHistoricalImageIdentifier(image string) string {
	var template models.Template
	if err := db.DB.Where("fingerprint = ? OR alias = ? OR description = ?", image, image, image).First(&template).Error; err == nil {
		return preferredTemplateIdentifier(&template, image)
	}
	return image
}

func CheckTemplateAccess(image, username string) (int, string) {
	if image == "" {
		return 400, "镜像不能为空"
	}
	var template models.Template
	if err := db.DB.Where("fingerprint = ? OR alias = ?", image, image).First(&template).Error; err != nil {
		return 404, "模板不存在"
	}
	return checkTemplateUserPermission(&template, username)
}

func ResolveReinstallTemplateAccess(reqImage, fallbackImage, username string) (string, int, string) {
	var template models.Template
	if reqImage != "" {
		if err := db.DB.Where("fingerprint = ? OR alias = ?", reqImage, reqImage).First(&template).Error; err != nil {
			return "", 404, "模板不存在"
		}
		if code, msg := checkTemplateUserPermission(&template, username); code != 0 {
			return "", code, msg
		}
		return preferredTemplateIdentifier(&template, reqImage), 0, ""
	}
	if fallbackImage == "" {
		return "", 400, "镜像不能为空"
	}
	if err := db.DB.Where("fingerprint = ? OR alias = ? OR description = ?", fallbackImage, fallbackImage, fallbackImage).First(&template).Error; err != nil {
		return "", 404, "模板不存在"
	}
	if code, msg := checkTemplateUserPermission(&template, username); code != 0 {
		return "", code, msg
	}
	resolved := preferredTemplateIdentifier(&template, fallbackImage)
	if err := lxc.ValidateImageName(resolved); err != nil {
		return "", 400, err.Error()
	}
	return resolved, 0, ""
}

func (s *ContainerService) setContainerHostname(ctx context.Context, name string) {
	if _, err := s.lxcClient.ExecInContainer(ctx, name, []string{"hostnamectl", "set-hostname", name}); err != nil {
		logger.Warn("使用hostnamectl设置主机名失败，尝试直接修改文件: %v", err)
		if _, err := s.lxcClient.ExecInContainerWithStdin(ctx, name, name+"\n", []string{"tee", "/etc/hostname"}); err != nil {
			logger.Warn("写入/etc/hostname失败: %v", err)
			return
		}
		if _, err := s.lxcClient.ExecInContainer(ctx, name, []string{"hostname", name}); err != nil {
			logger.Warn("设置主机名失败: %v", err)
			return
		}
	}
	logger.Info("主机名已设置: %s", name)
}

func (s *ContainerService) Create(ctx context.Context, req *models.CreateContainerRequest) error {
	if err := lxc.ValidateContainerName(req.Name); err != nil {
		return err
	}
	if err := lxc.ValidateImageName(req.Image); err != nil {
		return err
	}
	if s.lxcClient.ContainerExists(ctx, req.Name) {
		return fmt.Errorf("容器已存在: %s", req.Name)
	}
	
	user, err := GetOrCreateUser(req.Username)
	if err != nil {
		return fmt.Errorf("处理用户失败: %v", err)
	}
	
	if user.TrafficLocked {
		return fmt.Errorf("用户流量已超限，无法创建容器")
	}
	
	logger.Info("用户确认: %s (ID: %d)", user.Username, user.ID)
	
	logger.Info("开始创建容器: %s, 镜像: %s", req.Name, req.Image)
	
	memory := ""
	if req.Memory > 0 {
		memory = fmt.Sprintf("%dMB", req.Memory)
	}
	disk := ""
	if req.Disk > 0 {
		disk = fmt.Sprintf("%dMB", req.Disk)
	}
	ingress := ""
	if req.Ingress > 0 {
		ingress = fmt.Sprintf("%dMbit", req.Ingress)
	}
	egress := ""
	if req.Egress > 0 {
		egress = fmt.Sprintf("%dMbit", req.Egress)
	}
	cpuAllowance := ""
	if req.CPUAllowance > 0 {
		cpuAllowance = fmt.Sprintf("%d%%", req.CPUAllowance)
	}
	ioRead := ""
	if req.IORead > 0 {
		ioRead = fmt.Sprintf("%dMB", req.IORead)
	}
	ioWrite := ""
	if req.IOWrite > 0 {
		ioWrite = fmt.Sprintf("%dMB", req.IOWrite)
	}
	
	storagePool := NewStorageService().GetDefault()
	if err := lxc.ValidateStoragePoolName(storagePool); err != nil {
		return err
	}
	logger.Info("使用存储池: %s", storagePool)
	
	if err := s.lxcClient.CreateContainerWithConfig(ctx, req.Name, req.Image, storagePool, req.CPU, memory, disk, ingress, egress, req.AllowNesting, req.MemorySwap, req.Privileged, "", cpuAllowance, ioRead, ioWrite, req.ProcessesLimit); err != nil {
		return fmt.Errorf("创建容器失败: %v", err)
	}
	
	if err := s.lxcClient.StartContainer(ctx, req.Name); err != nil {
		logger.Warn("启动容器失败: %v", err)
	}
	
	imageAlias := resolveStoredImageIdentifier(req.Image)
	
	s.setContainerHostname(ctx, req.Name)
	
	actualPassword := req.Password
	if actualPassword == "" {
		actualPassword = generatePassword()
		logger.Info("未指定密码，已自动生成密码")
	}
	
	if err := s.lxcClient.SetRootPassword(ctx, req.Name, actualPassword); err != nil {
		logger.Warn("设置root密码失败: %v", err)
	} else {
		logger.Info("root密码已设置")
	}
	
	macAddress, err := s.lxcClient.GetContainerMAC(ctx, req.Name)
	if err != nil {
		logger.Warn("获取容器MAC地址失败: %v", err)
		macAddress = ""
	} else {
		logger.Info("容器MAC地址: %s", macAddress)
	}
	
	time.Sleep(3 * time.Second)
	
	privateIP := s.getContainerIP(ctx, req.Name)
	privateIPv6 := s.getContainerIPv6(ctx, req.Name)
	
	if privateIP != "" {
		logger.Info("容器内网IPv4: %s", privateIP)
	} else {
		logger.Warn("首次未获取到容器内网IPv4，将在后台任务中重试")
	}
	
	if privateIPv6 != "" {
		logger.Info("容器内网IPv6: %s", privateIPv6)
	} else {
		logger.Warn("首次未获取到容器内网IPv6，将在后台任务中重试")
	}
	
	container := &models.Container{
		Name:         req.Name,
		UserID:       req.Username,
		Image:        imageAlias,
		Password:     actualPassword,
		Status:       "stopped",
		CPU:          req.CPU,
		Memory:       req.Memory,
		Disk:         req.Disk,
		Ingress:          req.Ingress,
		Egress:           req.Egress,
		TrafficLimit:     req.TrafficLimit,
		IPv4PoolLimit:    req.IPv4PoolLimit,
		IPv4MappingLimit: req.IPv4MappingLimit,
		IPv6PoolLimit:    req.IPv6PoolLimit,
		IPv6MappingLimit: req.IPv6MappingLimit,
		ReverseProxyLimit: req.ReverseProxyLimit,
		CPUAllowance:     req.CPUAllowance,
		IORead:           req.IORead,
		IOWrite:          req.IOWrite,
		ProcessesLimit:   req.ProcessesLimit,
		PrivateIP:        privateIP,
		PrivateIPv6:  privateIPv6,
		MacAddress:   macAddress,
		AllowNesting: req.AllowNesting,
		MemorySwap:   req.MemorySwap,
		Privileged:   req.Privileged,
		Remark:       req.Remark,
	}
	
	if err := db.DB.Create(container).Error; err != nil {
		logger.Error("保存容器记录失败: %v", err)
		s.lxcClient.DeleteContainer(ctx, req.Name)
		return fmt.Errorf("保存容器记录失败: %v", err)
	}
	
	needRetryIPv4 := privateIP == ""
	needRetryIPv6 := privateIPv6 == ""
	if needRetryIPv4 || needRetryIPv6 {
		logger.Info("启动后台任务获取容器IP (IPv4: %v, IPv6: %v)", needRetryIPv4, needRetryIPv6)
		go s.retryGetContainerIPs(req.Name, needRetryIPv4, needRetryIPv6, "", "")
	}
	
	if req.TrafficLimit > 0 {
		traffic := &models.Traffic{
			ContainerName: req.Name,
			LimitGB:       req.TrafficLimit,
		}
		if err := db.DB.Create(traffic).Error; err != nil {
			logger.Warn("创建流量记录失败: %v", err)
		}
	}
	
	if _, err := CreateContainerCredential(req.Name); err != nil {
		logger.Warn("创建容器访问码失败: %v", err)
	} else {
		logger.OK("容器访问码已生成: %s", req.Name)
	}
	
	logger.OK("容器创建完成: %s (IPv4: %s, IPv6: %s)", req.Name,
		func() string { if privateIP != "" { return privateIP } else { return "等待获取" } }(),
		func() string { if privateIPv6 != "" { return privateIPv6 } else { return "等待获取" } }())
	
	var ipSettings models.IPPoolSettings
	db.DB.First(&ipSettings)
	if ipSettings.AutoAssign {
		if container.IPv4PoolLimit > 0 && ipv4.GlobalManager != nil && privateIP != "" {
			ipv4Svc := NewIPv4Service()
			if ips, err := ipv4Svc.AllocateIPv4(ctx, req.Name, req.Username, container.IPv4PoolLimit); err != nil {
				logger.Warn("自动分配IPv4失败: %v", err)
			} else if len(ips) > 0 {
				logger.OK("自动分配IPv4成功: %s -> %v", req.Name, ips)
			}
		}
		if container.IPv6PoolLimit > 0 && ipv6.GlobalManager != nil && privateIPv6 != "" {
			ipv6Svc := NewIPv6Service()
			if ips, err := ipv6Svc.AllocateIPv6(ctx, req.Name, req.Username, container.IPv6PoolLimit); err != nil {
				logger.Warn("自动分配IPv6失败: %v", err)
			} else if len(ips) > 0 {
				logger.OK("自动分配IPv6成功: %s -> %v", req.Name, ips)
			}
		}
	}
	
	var portRangeConfig models.PortRangeConfig
	db.DB.First(&portRangeConfig)
	
	if portRangeConfig.V4AutoAllocate22 && container.IPv4MappingLimit >= 1 && privateIP != "" {
		if err := s.autoAllocateSSHPortV4(ctx, req.Name, req.Username, privateIP, portRangeConfig); err != nil {
			logger.Warn("自动分配IPv4 SSH端口失败: %v", err)
		}
	}
	
	if portRangeConfig.V6AutoAllocate22 && container.IPv6MappingLimit >= 1 && privateIPv6 != "" {
		if err := s.autoAllocateSSHPortV6(ctx, req.Name, req.Username, privateIPv6, portRangeConfig); err != nil {
			logger.Warn("自动分配IPv6 SSH端口失败: %v", err)
		}
	}
	
	if mgr := plugin.GetManager(); mgr != nil {
		mgr.GetHookManager().TriggerAsync(plugin.HookAfterContainerCreate, ctx, map[string]interface{}{
			"name":     req.Name,
			"username": req.Username,
			"image":    req.Image,
		})
	}
	
	cache.RefreshContainerCache(ctx, req.Name)
	
	return nil
}

func (s *ContainerService) Start(ctx context.Context, name string) error {
	if err := s.lxcClient.StartContainer(ctx, name); err != nil {
		return err
	}
	db.DB.Model(&models.Container{}).Where("name = ?", name).Updates(map[string]interface{}{
		"status":        "running",
		"freeze_source": "",
	})
	cache.RefreshContainerCache(ctx, name)
	return nil
}

func (s *ContainerService) Stop(ctx context.Context, name string) error {
	if err := s.lxcClient.StopContainer(ctx, name); err != nil {
		return err
	}
	db.DB.Model(&models.Container{}).Where("name = ?", name).Updates(map[string]interface{}{
		"status":        "stopped",
		"freeze_source": "",
	})
	cache.RefreshContainerCache(ctx, name)
	return nil
}

func (s *ContainerService) Restart(ctx context.Context, name string) error {
	if err := s.lxcClient.RestartContainer(ctx, name); err != nil {
		return err
	}
	db.DB.Model(&models.Container{}).Where("name = ?", name).Updates(map[string]interface{}{
		"status":        "running",
		"freeze_source": "",
	})
	cache.RefreshContainerCache(ctx, name)
	return nil
}

func (s *ContainerService) Pause(ctx context.Context, name string) error {
	return s.PauseWithSource(ctx, name, "admin")
}

func (s *ContainerService) PauseWithSource(ctx context.Context, name, source string) error {
	if err := s.lxcClient.PauseContainer(ctx, name); err != nil {
		return err
	}
	db.DB.Model(&models.Container{}).Where("name = ?", name).Updates(map[string]interface{}{
		"status":        "frozen",
		"freeze_source": source,
	})
	cache.RefreshContainerCache(ctx, name)
	return nil
}

func (s *ContainerService) Resume(ctx context.Context, name string) error {
	if err := s.lxcClient.ResumeContainer(ctx, name); err != nil {
		return err
	}
	db.DB.Model(&models.Container{}).Where("name = ?", name).Updates(map[string]interface{}{
		"status":        "running",
		"freeze_source": "",
	})
	cache.RefreshContainerCache(ctx, name)
	return nil
}

func IsContainerTrafficLocked(container *models.Container) bool {
	if container == nil {
		return false
	}
	locked, _ := cache.CheckContainerTrafficLock(*container)
	return locked
}

func CheckContainerOperationAllowed(container *models.Container, is_user_portal bool, action string) (int, string) {
	if container == nil {
		return 404, "容器不存在"
	}
	if locked, msg := cache.CheckContainerTrafficLock(*container); locked {
		return 403, msg
	}
	if strings.ToLower(container.Status) == "frozen" {
		if container.FreezeSource == "user" {
			if is_user_portal && action == "unsuspend" {
				return 0, ""
			}
			return 403, "容器已暂停，请先恢复运行后再执行此操作"
		}
		if container.FreezeSource == "system" {
			return 403, "容器已被系统冻结，请联系管理员处理"
		}
		return 403, "容器已被管理员冻结，请联系管理员解冻"
	}
	return 0, ""
}

func CheckContainerOperationByName(name string, is_user_portal bool, action string) (int, string) {
	var container models.Container
	if err := db.DB.Where("name = ?", name).First(&container).Error; err != nil {
		return 404, "容器不存在"
	}
	return CheckContainerOperationAllowed(&container, is_user_portal, action)
}

func (s *ContainerService) Reinstall(ctx context.Context, name, image, password string) error {
	if err := lxc.ValidateContainerName(name); err != nil {
		return err
	}
	var container models.Container
	if err := db.DB.Where("name = ?", name).First(&container).Error; err != nil {
		return fmt.Errorf("容器不存在: %v", err)
	}

	logger.Info("开始重装容器: %s", name)

	if image == "" {
		image = resolveHistoricalImageIdentifier(container.Image)
	} else {
		image = resolveStoredImageIdentifier(image)
	}
	if err := lxc.ValidateImageName(image); err != nil {
		return err
	}

	needCreate := false
	if s.lxcClient.ContainerExists(ctx, name) {
		if err := s.lxcClient.RebuildContainer(ctx, name, image); err != nil {
			logger.Warn("rebuild 失败，尝试删除后重建: %v", err)
			s.lxcClient.DeleteContainer(ctx, name)
			needCreate = true
		}
	} else {
		needCreate = true
	}

	if needCreate {
		logger.Info("使用创建方式重装: %s", name)
		memory := ""
		if container.Memory > 0 {
			memory = fmt.Sprintf("%dMB", container.Memory)
		}
		disk := ""
		if container.Disk > 0 {
			disk = fmt.Sprintf("%dMB", container.Disk)
		}
		ingress := ""
		if container.Ingress > 0 {
			ingress = fmt.Sprintf("%dMbit", container.Ingress)
		}
		egress := ""
		if container.Egress > 0 {
			egress = fmt.Sprintf("%dMbit", container.Egress)
		}
		cpuAllowance := ""
		if container.CPUAllowance > 0 {
			cpuAllowance = fmt.Sprintf("%d%%", container.CPUAllowance)
		}
		ioRead := ""
		if container.IORead > 0 {
			ioRead = fmt.Sprintf("%dMB", container.IORead)
		}
		ioWrite := ""
		if container.IOWrite > 0 {
			ioWrite = fmt.Sprintf("%dMB", container.IOWrite)
		}
		storagePool := NewStorageService().GetDefault()
		if err := lxc.ValidateStoragePoolName(storagePool); err != nil {
			return err
		}
		if err := s.lxcClient.CreateContainerWithConfig(ctx, name, image, storagePool,
			container.CPU, memory, disk, ingress, egress,
			container.AllowNesting, container.MemorySwap, container.Privileged,
			"", cpuAllowance, ioRead, ioWrite, container.ProcessesLimit); err != nil {
			return fmt.Errorf("重新创建容器失败: %v", err)
		}
	}

	time.Sleep(3 * time.Second)

	if err := s.lxcClient.StartContainer(ctx, name); err != nil {
		if !strings.Contains(err.Error(), "already running") {
			return fmt.Errorf("启动容器失败: %v", err)
		}
		logger.Info("容器已在运行中，跳过启动")
	}

	imageAlias := resolveStoredImageIdentifier(image)

	s.setContainerHostname(ctx, name)

	finalPassword := password
	if finalPassword == "" {
		finalPassword = container.Password
	}
	
	if finalPassword != "" {
		if err := s.lxcClient.SetRootPassword(ctx, name, finalPassword); err != nil {
			logger.Warn("设置root密码失败: %v", err)
		} else {
			if password != "" {
				logger.Info("root密码已更新为新密码")
				db.DB.Model(&models.Container{}).Where("name = ?", name).Update("password", password)
			} else {
				logger.Info("root密码已恢复为原密码")
			}
		}
	}

	actualPrivateIP := s.getContainerIP(ctx, name)
	actualPrivateIPv6 := s.getContainerIPv6(ctx, name)
	
	updateFields := map[string]interface{}{
		"image":         imageAlias,
		"status":        "running",
		"freeze_source": "",
	}
	
	if actualPrivateIP != "" {
		if actualPrivateIP != container.PrivateIP {
			logger.Warn("容器重装后内网IPv4发生变化: %s -> %s", container.PrivateIP, actualPrivateIP)
			updateFields["private_ip"] = actualPrivateIP
			s.updateContainerIPv4Rules(name, container.PrivateIP, actualPrivateIP)
		} else {
			logger.OK("容器内网IPv4保持不变: %s", actualPrivateIP)
		}
	}
	
	if actualPrivateIPv6 != "" {
		if actualPrivateIPv6 != container.PrivateIPv6 {
			logger.Warn("容器重装后内网IPv6发生变化: %s -> %s", container.PrivateIPv6, actualPrivateIPv6)
			updateFields["private_ipv6"] = actualPrivateIPv6
			s.updateContainerIPv6Rules(name, container.PrivateIPv6, actualPrivateIPv6)
		} else {
			logger.OK("容器内网IPv6保持不变: %s", actualPrivateIPv6)
		}
	}
	
	db.DB.Model(&models.Container{}).Where("name = ?", name).Updates(updateFields)
	
	needRetryIPv4 := actualPrivateIP == ""
	needRetryIPv6 := actualPrivateIPv6 == ""
	if needRetryIPv4 || needRetryIPv6 {
		logger.Info("启动后台任务获取容器IP (IPv4: %v, IPv6: %v)", needRetryIPv4, needRetryIPv6)
		go s.retryGetContainerIPs(name, needRetryIPv4, needRetryIPv6, container.PrivateIP, container.PrivateIPv6)
	}

	logger.OK("容器重装完成: %s (IPv4: %s, IPv6: %s)", name, 
		func() string { if actualPrivateIP != "" { return actualPrivateIP } else { return "等待获取" } }(),
		func() string { if actualPrivateIPv6 != "" { return actualPrivateIPv6 } else { return "等待获取" } }())
	return nil
}

func (s *ContainerService) Delete(ctx context.Context, name string) error {
	var container models.Container
	if err := db.DB.Where("name = ?", name).First(&container).Error; err == nil {
		var traffic models.Traffic
		if err := db.DB.Where("container_name = ?", name).First(&traffic).Error; err == nil && traffic.TotalGB > 0 {
			db.DB.Model(&models.User{}).Where("username = ?", container.UserID).
				Update("traffic_used", gorm.Expr("traffic_used + ?", traffic.TotalGB))
		}
	}

	var mappingsV4 []models.PortMappingV4
	if err := db.DB.Where("container_name = ?", name).Find(&mappingsV4).Error; err == nil {
		pmService := NewPortMappingService()
		for _, mapping := range mappingsV4 {
			if err := pmService.ReleaseV4Mapping(mapping.ID); err != nil {
				logger.Warn("释放IPv4端口映射失败 ID=%d: %v", mapping.ID, err)
			}
		}
	}
	
	var mappingsV6 []models.PortMappingV6
	if err := db.DB.Where("container_name = ?", name).Find(&mappingsV6).Error; err == nil {
		pmService := NewPortMappingService()
		for _, mapping := range mappingsV6 {
			if err := pmService.ReleaseV6Mapping(mapping.ID); err != nil {
				logger.Warn("释放IPv6端口映射失败 ID=%d: %v", mapping.ID, err)
			}
		}
	}
	
	if ipv4.GlobalManager != nil {
		if err := ipv4.GlobalManager.ReleaseIPs(name); err != nil {
			logger.Warn("释放IPv4地址池失败: %v", err)
		}
	} else {
		db.DB.Unscoped().Where("container_name = ?", name).Delete(&models.IPv4Binding{})
	}
	
	if ipv6.GlobalManager != nil {
		if err := ipv6.GlobalManager.ReleaseIPs(name); err != nil {
			logger.Warn("释放IPv6地址池失败: %v", err)
		}
	} else {
		db.DB.Unscoped().Where("container_name = ?", name).Delete(&models.IPv6Binding{})
	}
	
	if err := s.lxcClient.DeleteContainer(ctx, name); err != nil {
		logger.Warn("删除LXD容器失败: %v，继续清理数据库和缓存", err)
	}
	
	db.DB.Unscoped().Where("name = ?", name).Delete(&models.Container{})
	db.DB.Unscoped().Where("container_name = ?", name).Delete(&models.Traffic{})
	
	if err := DeleteContainerCredential(name); err != nil {
		logger.Warn("删除容器凭证失败: %v", err)
	}
	
	cache.DeleteContainerCache(name)
	
	logger.OK("容器及相关数据已清理: %s", name)
	
	if mgr := plugin.GetManager(); mgr != nil {
		mgr.GetHookManager().TriggerAsync(plugin.HookAfterContainerDelete, ctx, map[string]interface{}{
			"container_name": name,
		})
	}
	
	return nil
}

func (s *ContainerService) GetStatus(ctx context.Context, name string) (string, error) {
	return s.lxcClient.GetContainerStatus(ctx, name)
}

func (s *ContainerService) Get(name string) (*models.Container, error) {
	var container models.Container
	if err := db.DB.Where("name = ?", name).First(&container).Error; err != nil {
		return nil, fmt.Errorf("容器不存在: %s", name)
	}
	return &container, nil
}

func (s *ContainerService) List(userID string) ([]models.Container, error) {
	var containers []models.Container
	query := db.DB
	if userID != "" {
		query = query.Where("user_id = ?", userID)
	}
	if err := query.Find(&containers).Error; err != nil {
		return nil, err
	}
	for i := range containers {
		containers[i].TrafficLocked = IsContainerTrafficLocked(&containers[i])
	}
	return containers, nil
}

func ListContainersByUser(userID string) ([]models.Container, error) {
	var containers []models.Container
	query := db.DB
	if userID != "" {
		query = query.Where("user_id = ?", userID)
	}
	if err := query.Find(&containers).Error; err != nil {
		return nil, err
	}
	for i := range containers {
		containers[i].TrafficLocked = IsContainerTrafficLocked(&containers[i])
	}
	return containers, nil
}

func (s *ContainerService) ResetPassword(ctx context.Context, name, password string) error {
	if !s.lxcClient.ContainerExists(ctx, name) {
		return fmt.Errorf("容器不存在: %s", name)
	}
	
	if err := s.lxcClient.SetRootPassword(ctx, name, password); err != nil {
		return fmt.Errorf("设置密码失败: %v", err)
	}
	
	db.DB.Model(&models.Container{}).Where("name = ?", name).Update("password", password)
	
	logger.OK("容器 %s 密码重置成功", name)
	return nil
}

func GetContainerFromCache(name string) (interface{}, bool) {
	cacheData, ok := cache.GetContainerCache(name)
	return cacheData, ok
}

func (s *ContainerService) GetInfo(ctx context.Context, name string) (map[string]interface{}, error) {
	info, err := s.lxcClient.GetContainerInfo(ctx, name)
	if err != nil {
		return nil, err
	}

	var container models.Container
	if err := db.DB.Where("name = ?", name).First(&container).Error; err != nil {
		return nil, fmt.Errorf("容器不存在于数据库: %s", name)
	}

	result := map[string]interface{}{
		"name":                name,
		"status":              info.Status,
		"freeze_source":       container.FreezeSource,
		"traffic_locked":      IsContainerTrafficLocked(&container),
		"image":               container.Image,
		"password":           container.Password,
		"cpu":                0,
		"memory":             "",
		"disk":               "",
		"cpu_usage":          0.0,
		"memory_usage":       "",
		"memory_usage_raw":   0,
		"disk_usage":         "",
		"disk_usage_raw":     0,
		"traffic_usage":       "",
		"traffic_usage_raw":   0,
		"traffic_limit":       container.TrafficLimit,
		"ipv4_pool_limit":     container.IPv4PoolLimit,
		"ipv4_mapping_limit":  container.IPv4MappingLimit,
		"ipv6_pool_limit":     container.IPv6PoolLimit,
		"ipv6_mapping_limit":  container.IPv6MappingLimit,
		"reverse_proxy_limit": container.ReverseProxyLimit,
		"ipv4":                []string{},
		"ipv6":               []string{},
		"config":             info.Config,
		"created_at":         info.Created,
		"last_sync":          "",
	}

	if container.Image == "" {
		if imageDesc, ok := info.Config["image.description"].(string); ok {
			result["image"] = imageDesc
		}
	}

	if cpuLimit, ok := info.Config["limits.cpu"].(string); ok {
		if cpu, err := strconv.Atoi(cpuLimit); err == nil {
			result["cpu"] = cpu
		}
	}

	if memLimit, ok := info.Config["limits.memory"].(string); ok {
		result["memory"] = memLimit
	}

	for _, devConfig := range info.Devices {
		if devMap, ok := devConfig.(map[string]interface{}); ok {
			if devType, ok := devMap["type"].(string); ok && devType == "disk" {
				if path, ok := devMap["path"].(string); ok && path == "/" {
					if size, ok := devMap["size"].(string); ok {
						result["disk"] = size
					}
				}
			}
		}
	}

	var traffic models.Traffic
	if err := db.DB.Where("container_name = ?", name).First(&traffic).Error; err == nil {
		result["traffic_usage_raw"] = traffic.TotalGB
		result["traffic_usage"] = fmt.Sprintf("%.2f GB", traffic.TotalGB)
	}

	if info.State != nil && info.Status == "Running" {
		cpuUsage := getContainerCPUUsagePercent(ctx, name)
		result["cpu_usage"] = cpuUsage

		if memUsage, ok := info.State.Memory["usage"].(float64); ok {
			result["memory_usage_raw"] = uint64(memUsage)
			result["memory_usage"] = formatBytes(uint64(memUsage))
		}

		if diskUsage, ok := info.State.Disk["root"].(map[string]interface{}); ok {
			if usage, ok := diskUsage["usage"].(float64); ok {
				result["disk_usage_raw"] = uint64(usage)
				result["disk_usage"] = formatBytes(uint64(usage))
			}
		}

		ipv4List := []string{}
		ipv6List := []string{}

		if eth0Data, exists := info.State.Network["eth0"]; exists {
			if iface, ok := eth0Data.(map[string]interface{}); ok {
				if addresses, ok := iface["addresses"].([]interface{}); ok {
					for _, addr := range addresses {
						if addrMap, ok := addr.(map[string]interface{}); ok {
							if family, ok := addrMap["family"].(string); ok {
								if address, ok := addrMap["address"].(string); ok {
									if family == "inet" {
										if !isLoopbackOrLocalIP(address) {
											ipv4List = append(ipv4List, address)
										}
									} else if family == "inet6" {
										if !isLoopbackOrLocalIP(address) {
											ipv6List = append(ipv6List, address)
										}
									}
								}
							}
						}
					}
				}
			}
		}

		result["ipv4"] = ipv4List
		result["ipv6"] = ipv6List
	}

	return result, nil
}

func (s *ContainerService) getContainerIP(ctx context.Context, name string) string {
	ip, err := s.lxcClient.GetContainerIP(ctx, name)
	if err != nil {
		logger.Warn("获取容器 %s 内网IPv4失败: %v", name, err)
		return ""
	}
	return ip
}

func (s *ContainerService) getContainerIPv6(ctx context.Context, name string) string {
	ip, err := s.lxcClient.GetContainerIPv6(ctx, name)
	if err != nil {
		logger.Warn("获取容器 %s 内网IPv6失败: %v", name, err)
		return ""
	}
	return ip
}

func isLoopbackOrLocalIP(ip string) bool {
	if ip == "127.0.0.1" || ip == "::1" {
		return true
	}
	if strings.HasPrefix(ip, "fe80:") {
		return true
	}
	if strings.HasPrefix(ip, "169.254.") {
		return true
	}
	return false
}

func (s *ContainerService) retryGetContainerIPs(containerName string, needIPv4, needIPv6 bool, oldIPv4, oldIPv6 string) {
	ctx := context.Background()
	maxRetries := 10
	retryInterval := 2
	
	logger.Info("启动后台任务获取容器 %s 的IP地址 (IPv4: %v, IPv6: %v)", containerName, needIPv4, needIPv6)
	
	for i := 0; i < maxRetries; i++ {
		time.Sleep(time.Duration(retryInterval) * time.Second)
		
		updateFields := make(map[string]interface{})
		updated := false
		
		if needIPv4 {
			if ipv4 := s.getContainerIP(ctx, containerName); ipv4 != "" {
				updateFields["private_ip"] = ipv4
				logger.OK("后台任务成功获取容器 %s 内网IPv4: %s", containerName, ipv4)
				if oldIPv4 != "" && ipv4 != oldIPv4 {
					s.updateContainerIPv4Rules(containerName, oldIPv4, ipv4)
				}
				needIPv4 = false
				updated = true
			}
		}
		
		if needIPv6 {
			if ipv6 := s.getContainerIPv6(ctx, containerName); ipv6 != "" {
				updateFields["private_ipv6"] = ipv6
				logger.OK("后台任务成功获取容器 %s 内网IPv6: %s", containerName, ipv6)
				if oldIPv6 != "" && ipv6 != oldIPv6 {
					s.updateContainerIPv6Rules(containerName, oldIPv6, ipv6)
				}
				needIPv6 = false
				updated = true
			}
		}
		
		if updated {
			db.DB.Model(&models.Container{}).Where("name = ?", containerName).Updates(updateFields)
		}
		
		if !needIPv4 && !needIPv6 {
			logger.OK("后台任务完成：容器 %s 的所有IP地址已获取", containerName)
			return
		}
	}
	
	if needIPv4 {
		logger.Warn("后台任务超时：容器 %s 的IPv4地址获取失败（已重试%d次）", containerName, maxRetries)
	}
	if needIPv6 {
		logger.Warn("后台任务超时：容器 %s 的IPv6地址获取失败（已重试%d次）", containerName, maxRetries)
	}
}

func (s *ContainerService) updateContainerIPv4Rules(containerName, oldIP, newIP string) {
	if ipv4.GlobalManager == nil {
		return
	}

	var bindings []models.IPv4Binding
	if err := db.DB.Where("container_name = ?", containerName).Find(&bindings).Error; err == nil {
		for _, binding := range bindings {
			var pool models.IPv4Pool
			if err := db.DB.Where("ip_address = ?", binding.IPAddress).First(&pool).Error; err != nil {
				continue
			}

			ipv4.GlobalManager.RemovePortMapping(binding.IPAddress, 0, 0, oldIP, 0, 0, "", pool.Interface)

			if err := ipv4.GlobalManager.BindIP(containerName, binding.IPAddress, newIP); err != nil {
				logger.Warn("重装后重新绑定IPv4失败 %s: %v", binding.IPAddress, err)
			}
		}
	}

	var mappings []models.PortMappingV4
	if err := db.DB.Where("container_name = ?", containerName).Find(&mappings).Error; err == nil {
		for _, mapping := range mappings {
			publicPortEnd := mapping.PublicPortEnd
			if publicPortEnd == 0 {
				publicPortEnd = mapping.PublicPort
			}
			containerPortEnd := mapping.ContainerPortEnd
			if containerPortEnd == 0 {
				containerPortEnd = mapping.ContainerPort
			}

			if err := ipv4.GlobalManager.RemovePortMapping(mapping.ForwardIP, mapping.PublicPort, publicPortEnd, oldIP, mapping.ContainerPort, containerPortEnd, mapping.Protocol, mapping.Interface); err != nil {
				logger.Warn("重装后删除旧IPv4端口映射失败 %s:%d: %v", mapping.ForwardIP, mapping.PublicPort, err)
				continue
			}

			if err := ipv4.GlobalManager.AddPortMapping(mapping.ForwardIP, mapping.PublicPort, publicPortEnd, newIP, mapping.ContainerPort, containerPortEnd, mapping.Protocol, mapping.Interface); err != nil {
				logger.Warn("重装后重新添加IPv4端口映射失败 %s:%d: %v", mapping.ForwardIP, mapping.PublicPort, err)
				continue
			}

			db.DB.Model(&mapping).Update("container_ip", newIP)
		}
	}

	logger.OK("重装后IPv4规则已更新: %s", containerName)
}

func (s *ContainerService) updateContainerIPv6Rules(containerName, oldIP, newIP string) {
	if ipv6.GlobalManager == nil {
		return
	}

	var bindings []models.IPv6Binding
	if err := db.DB.Where("container_name = ?", containerName).Find(&bindings).Error; err == nil {
		for _, binding := range bindings {
			var pool models.IPv6Pool
			if err := db.DB.Where("ip_address = ?", binding.IPAddress).First(&pool).Error; err != nil {
				continue
			}

			ipv6.GlobalManager.RemovePortMapping(binding.IPAddress, 0, 0, oldIP, 0, 0, "", pool.Interface)

			if err := ipv6.GlobalManager.BindIP(containerName, binding.IPAddress, newIP); err != nil {
				logger.Warn("重装后重新绑定IPv6失败 %s: %v", binding.IPAddress, err)
			}
		}
	}

	var mappings []models.PortMappingV6
	if err := db.DB.Where("container_name = ?", containerName).Find(&mappings).Error; err == nil {
		for _, mapping := range mappings {
			publicPortEnd := mapping.PublicPortEnd
			if publicPortEnd == 0 {
				publicPortEnd = mapping.PublicPort
			}
			containerPortEnd := mapping.ContainerPortEnd
			if containerPortEnd == 0 {
				containerPortEnd = mapping.ContainerPort
			}

			if err := ipv6.GlobalManager.RemovePortMapping(mapping.ForwardIP, mapping.PublicPort, publicPortEnd, oldIP, mapping.ContainerPort, containerPortEnd, mapping.Protocol, mapping.Interface); err != nil {
				logger.Warn("重装后删除旧IPv6端口映射失败 %s:%d: %v", mapping.ForwardIP, mapping.PublicPort, err)
				continue
			}

			if err := ipv6.GlobalManager.AddPortMapping(mapping.ForwardIP, mapping.PublicPort, publicPortEnd, newIP, mapping.ContainerPort, containerPortEnd, mapping.Protocol, mapping.Interface); err != nil {
				logger.Warn("重装后重新添加IPv6端口映射失败 %s:%d: %v", mapping.ForwardIP, mapping.PublicPort, err)
				continue
			}

			db.DB.Model(&mapping).Update("container_ip", newIP)
		}
	}

	logger.OK("重装后IPv6规则已更新: %s", containerName)
}

func getContainerCPUUsagePercent(ctx context.Context, containerName string) float64 {
	lxcClient := lxc.NewClient()
	
	output, err := lxcClient.ExecInContainer(ctx, containerName, []string{"sh", "-c", "vmstat 1 2 | tail -1 | awk '{print $15}'"})
	if err != nil {
		return 0
	}
	
	idleStr := strings.TrimSpace(output)
	idlePercent, err := strconv.ParseFloat(idleStr, 64)
	if err != nil {
		return 0
	}
	
	cpuUsage := 100 - idlePercent
	if cpuUsage < 0 {
		cpuUsage = 0
	}
	if cpuUsage > 100 {
		cpuUsage = 100
	}
	
	return cpuUsage
}

func formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func (s *ContainerService) UpdateConfig(ctx context.Context, name string, req *models.UpdateContainerConfigRequest) ([]string, error) {
	container, err := s.Get(name)
	if err != nil {
		return nil, fmt.Errorf("容器不存在: %s", name)
	}

	if !s.lxcClient.ContainerExists(ctx, name) {
		return nil, fmt.Errorf("容器在LXD中不存在: %s", name)
	}

	var updated []string
	dbUpdates := make(map[string]interface{})

	if req.Disk != nil && *req.Disk > 0 {
		if *req.Disk < container.Disk {
			return nil, fmt.Errorf("磁盘只能扩容，不能缩小: 当前 %dMB，目标 %dMB", container.Disk, *req.Disk)
		}
	}

	cpu := 0
	memory := ""
	disk := ""
	ingress := ""
	egress := ""
	cpuAllowance := ""
	ioRead := ""
	ioWrite := ""
	processesLimit := 0

	if req.CPU != nil && *req.CPU > 0 {
		cpu = *req.CPU
		dbUpdates["cpu"] = *req.CPU
		updated = append(updated, "cpu")
	}

	if req.Memory != nil && *req.Memory > 0 {
		memory = fmt.Sprintf("%dMB", *req.Memory)
		dbUpdates["memory"] = *req.Memory
		updated = append(updated, "memory")
	}

	if req.Disk != nil && *req.Disk > 0 && *req.Disk != container.Disk {
		disk = fmt.Sprintf("%dMB", *req.Disk)
		dbUpdates["disk"] = *req.Disk
		updated = append(updated, "disk")
	}

	if req.Ingress != nil && *req.Ingress >= 0 {
		ingress = fmt.Sprintf("%dMbit", *req.Ingress)
		dbUpdates["ingress"] = *req.Ingress
		updated = append(updated, "ingress")
	}

	if req.Egress != nil && *req.Egress >= 0 {
		egress = fmt.Sprintf("%dMbit", *req.Egress)
		dbUpdates["egress"] = *req.Egress
		updated = append(updated, "egress")
	}

	if req.CPUAllowance != nil && *req.CPUAllowance > 0 {
		cpuAllowance = fmt.Sprintf("%d%%", *req.CPUAllowance)
		dbUpdates["cpu_allowance"] = *req.CPUAllowance
		updated = append(updated, "cpu_allowance")
	}

	if req.IORead != nil && *req.IORead >= 0 {
		ioRead = fmt.Sprintf("%dMB", *req.IORead)
		dbUpdates["io_read"] = *req.IORead
		updated = append(updated, "io_read")
	}

	if req.IOWrite != nil && *req.IOWrite >= 0 {
		ioWrite = fmt.Sprintf("%dMB", *req.IOWrite)
		dbUpdates["io_write"] = *req.IOWrite
		updated = append(updated, "io_write")
	}

	if req.ProcessesLimit != nil && *req.ProcessesLimit > 0 {
		processesLimit = *req.ProcessesLimit
		dbUpdates["processes_limit"] = *req.ProcessesLimit
		updated = append(updated, "processes_limit")
	}

	if cpu > 0 || memory != "" || disk != "" || ingress != "" || egress != "" || cpuAllowance != "" || ioRead != "" || ioWrite != "" || processesLimit > 0 || req.Privileged != nil || req.MemorySwap != nil || req.AllowNesting != nil {
		if err := s.lxcClient.UpdateContainerConfig(ctx, name, cpu, memory, disk, ingress, egress, cpuAllowance, ioRead, ioWrite, processesLimit, req.Privileged, req.MemorySwap, req.AllowNesting); err != nil {
			return nil, fmt.Errorf("更新LXD配置失败: %v", err)
		}
	}

	if req.TrafficLimit != nil && *req.TrafficLimit >= 0 {
		dbUpdates["traffic_limit"] = *req.TrafficLimit
		updated = append(updated, "traffic_limit")
		db.DB.Model(&models.Traffic{}).Where("container_name = ?", name).Update("limit_gb", *req.TrafficLimit)
	}

	if req.IPv4PoolLimit != nil && *req.IPv4PoolLimit >= 0 {
		var existingBindings []models.IPv4Binding
		db.DB.Where("container_name = ?", name).Find(&existingBindings)
		usedCount := len(existingBindings)
		
		if *req.IPv4PoolLimit < usedCount {
			return nil, fmt.Errorf("IPv4地址池限制不能小于已用数量，已用%d个地址", usedCount)
		}
		
		dbUpdates["ipv4_pool_limit"] = *req.IPv4PoolLimit
		updated = append(updated, "ipv4_pool_limit")
	}

	if req.IPv4MappingLimit != nil && *req.IPv4MappingLimit >= 0 {
		var existingMappings []models.PortMappingV4
		db.DB.Where("container_name = ?", name).Find(&existingMappings)
		
		ruleMap := make(map[string]int)
		for _, m := range existingMappings {
			key := fmt.Sprintf("%d-%d-%d-%s", m.PublicPort, m.PublicPortEnd, m.ContainerPort, m.Protocol)
			if _, exists := ruleMap[key]; !exists {
				if m.PublicPortEnd > 0 && m.PublicPortEnd != m.PublicPort {
					ruleMap[key] = m.PublicPortEnd - m.PublicPort + 1
				} else {
					ruleMap[key] = 1
				}
			}
		}
		
		usedPorts := 0
		for _, count := range ruleMap {
			usedPorts += count
		}
		
		if *req.IPv4MappingLimit < usedPorts {
			return nil, fmt.Errorf("IPv4端口映射限制不能小于已用端口数，已用%d个端口", usedPorts)
		}
		
		dbUpdates["ipv4_mapping_limit"] = *req.IPv4MappingLimit
		updated = append(updated, "ipv4_mapping_limit")
	}

	if req.IPv6PoolLimit != nil && *req.IPv6PoolLimit >= 0 {
		var existingBindings []models.IPv6Binding
		db.DB.Where("container_name = ?", name).Find(&existingBindings)
		usedCount := len(existingBindings)
		
		if *req.IPv6PoolLimit < usedCount {
			return nil, fmt.Errorf("IPv6地址池限制不能小于已用数量，已用%d个地址", usedCount)
		}
		
		dbUpdates["ipv6_pool_limit"] = *req.IPv6PoolLimit
		updated = append(updated, "ipv6_pool_limit")
	}

	if req.IPv6MappingLimit != nil && *req.IPv6MappingLimit >= 0 {
		var existingMappings []models.PortMappingV6
		db.DB.Where("container_name = ?", name).Find(&existingMappings)
		
		ruleMap := make(map[string]int)
		for _, m := range existingMappings {
			key := fmt.Sprintf("%d-%d-%d-%s", m.PublicPort, m.PublicPortEnd, m.ContainerPort, m.Protocol)
			if _, exists := ruleMap[key]; !exists {
				if m.PublicPortEnd > 0 && m.PublicPortEnd != m.PublicPort {
					ruleMap[key] = m.PublicPortEnd - m.PublicPort + 1
				} else {
					ruleMap[key] = 1
				}
			}
		}
		
		usedPorts := 0
		for _, count := range ruleMap {
			usedPorts += count
		}
		
		if *req.IPv6MappingLimit < usedPorts {
			return nil, fmt.Errorf("IPv6端口映射限制不能小于已用端口数，已用%d个端口", usedPorts)
		}
		
		dbUpdates["ipv6_mapping_limit"] = *req.IPv6MappingLimit
		updated = append(updated, "ipv6_mapping_limit")
	}

	if req.ReverseProxyLimit != nil && *req.ReverseProxyLimit >= 0 {
		var existingProxies []models.ReverseProxy
		db.DB.Where("container_name = ?", name).Find(&existingProxies)
		usedCount := len(existingProxies)
		
		if *req.ReverseProxyLimit < usedCount {
			return nil, fmt.Errorf("反向代理限制不能小于已用数量，已用%d个代理", usedCount)
		}
		
		dbUpdates["reverse_proxy_limit"] = *req.ReverseProxyLimit
		updated = append(updated, "reverse_proxy_limit")
	}

	if req.Remark != nil {
		if len(*req.Remark) > 20 {
			return nil, fmt.Errorf("备注长度不能超过20个字符")
		}
		dbUpdates["remark"] = *req.Remark
		updated = append(updated, "remark")
	}

	if req.Privileged != nil {
		dbUpdates["privileged"] = *req.Privileged
		updated = append(updated, "privileged")
	}

	if req.MemorySwap != nil {
		dbUpdates["memory_swap"] = *req.MemorySwap
		updated = append(updated, "memory_swap")
	}

	if req.AllowNesting != nil {
		dbUpdates["allow_nesting"] = *req.AllowNesting
		updated = append(updated, "allow_nesting")
	}

	if len(dbUpdates) > 0 {
		if err := db.DB.Model(&models.Container{}).Where("name = ?", name).Updates(dbUpdates).Error; err != nil {
			return nil, fmt.Errorf("更新数据库失败: %v", err)
		}
	}

	logger.OK("容器 %s 配置更新成功: %v", name, updated)
	return updated, nil
}

func (s *ContainerService) GetConfig(name string) (map[string]interface{}, error) {
	var container models.Container
	if err := db.DB.Where("name = ?", name).First(&container).Error; err != nil {
		return nil, fmt.Errorf("容器不存在: %s", name)
	}

	return map[string]interface{}{
		"name":               container.Name,
		"cpu":                container.CPU,
		"memory":             container.Memory,
		"disk":               container.Disk,
		"ingress":            container.Ingress,
		"egress":             container.Egress,
		"cpu_allowance":      container.CPUAllowance,
		"io_read":            container.IORead,
		"io_write":           container.IOWrite,
		"processes_limit":    container.ProcessesLimit,
		"traffic_limit":      container.TrafficLimit,
		"ipv4_pool_limit":    container.IPv4PoolLimit,
		"ipv4_mapping_limit": container.IPv4MappingLimit,
		"ipv6_pool_limit":    container.IPv6PoolLimit,
		"ipv6_mapping_limit": container.IPv6MappingLimit,
		"reverse_proxy_limit": container.ReverseProxyLimit,
		"remark":             container.Remark,
		"privileged":         container.Privileged,
		"memory_swap":        container.MemorySwap,
		"allow_nesting":      container.AllowNesting,
	}, nil
}

func (s *ContainerService) autoAllocateSSHPortV4(ctx context.Context, containerName, userID, containerIP string, config models.PortRangeConfig) error {
	var container models.Container
	if err := db.DB.Where("name = ?", containerName).First(&container).Error; err == nil {
		if container.PrivateIP != containerIP {
			logger.Warn("自动分配SSH端口前检测到IP不一致，强制修正: DB=%s -> Real=%s", container.PrivateIP, containerIP)
			db.DB.Model(&models.Container{}).Where("name = ?", containerName).Update("private_ip", containerIP)
		}
	}

	var natConfigs []models.NATConfigV4
	if err := db.DB.Find(&natConfigs).Error; err != nil || len(natConfigs) == 0 {
		return fmt.Errorf("未配置IPv4 NAT")
	}
	
	natConfig := natConfigs[0]
	displayIP := natConfig.DisplayIP
	if displayIP == "" {
		displayIP = natConfig.IP
	}

	portMappingSvc := NewPortMappingService()
	publicPort, err := portMappingSvc.FindAvailableV4Port(config.V4PortStart, config.V4PortEnd, 1, "tcp")
	if err != nil {
		return err
	}
	
	_, err = portMappingSvc.AllocateV4Mapping(ctx, containerName, userID, natConfig.IP, displayIP, publicPort, publicPort, 22, 22, "tcp", natConfig.Interface, "Auto-allocated SSH port")
	if err != nil {
		return err
	}
	
	logger.OK("自动分配IPv4 SSH端口: %s -> %s:%d", containerName, displayIP, publicPort)
	return nil
}

func (s *ContainerService) autoAllocateSSHPortV6(ctx context.Context, containerName, userID, containerIP string, config models.PortRangeConfig) error {
	var container models.Container
	if err := db.DB.Where("name = ?", containerName).First(&container).Error; err == nil {
		if container.PrivateIPv6 != containerIP {
			logger.Warn("自动分配IPv6 SSH端口前检测到IP不一致，强制修正: DB=%s -> Real=%s", container.PrivateIPv6, containerIP)
			db.DB.Model(&models.Container{}).Where("name = ?", containerName).Update("private_ipv6", containerIP)
		}
	}

	var natConfigs []models.NATConfigV6
	if err := db.DB.Find(&natConfigs).Error; err != nil || len(natConfigs) == 0 {
		return fmt.Errorf("未配置IPv6 NAT")
	}
	
	natConfig := natConfigs[0]
	displayIP := natConfig.DisplayIP
	if displayIP == "" {
		displayIP = natConfig.IP
	}

	portMappingSvc := NewPortMappingService()
	publicPort, err := portMappingSvc.FindAvailableV6Port(config.V6PortStart, config.V6PortEnd, 1, "tcp")
	if err != nil {
		return err
	}
	
	_, err = portMappingSvc.AllocateV6Mapping(ctx, containerName, userID, natConfig.IP, displayIP, publicPort, publicPort, 22, 22, "tcp", natConfig.Interface, "Auto-allocated SSH port")
	if err != nil {
		return err
	}
	
	logger.OK("自动分配IPv6 SSH端口: %s -> %s:%d", containerName, displayIP, publicPort)
	return nil
}
