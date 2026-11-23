package lxc

import (
	"context"
	"encoding/base64"
	"fmt"
	"lxdapi/pkg/logger"
	"net"
	"regexp"
	"strings"
	"time"
	"unicode"
)

var containerNamePattern = regexp.MustCompile("^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$")

func ValidateContainerName(name string) error {
	if !containerNamePattern.MatchString(name) {
		return fmt.Errorf("容器名称不合法，仅支持1-63位字母、数字和连字符，且不能以连字符开头或结尾")
	}
	return nil
}

func validateLXCIdentifier(value, label string, allowColonAndSlash bool) error {
	if value == "" {
		return fmt.Errorf("%s不能为空", label)
	}
	if strings.HasPrefix(value, "-") || strings.HasPrefix(value, "/") {
		return fmt.Errorf("%s不能以连字符或斜杠开头", label)
	}
	if strings.Contains(value, "..") || strings.Contains(value, "\\") {
		return fmt.Errorf("%s包含非法路径字符", label)
	}
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return fmt.Errorf("%s不能包含空白或控制字符", label)
		}
		if !allowColonAndSlash && (r == '/' || r == ':') {
			return fmt.Errorf("%s包含非法字符", label)
		}
	}
	return nil
}

func ValidateImageName(image string) error {
	return validateLXCIdentifier(image, "镜像名称", true)
}

func ValidateStoragePoolName(pool string) error {
	return validateLXCIdentifier(pool, "存储池名称", false)
}

func (c *Client) SetContainerMAC(ctx context.Context, name, macAddress, ingress, egress string) error {
	if err := ValidateContainerName(name); err != nil {
		return err
	}
	if macAddress != "" {
		logger.Info("为容器设置MAC地址以固定IP: %s -> %s", name, macAddress)
		_, err := c.exec(ctx, "config", "device", "override", name, "eth0", fmt.Sprintf("hwaddr=%s", macAddress))
		if err != nil {
			return fmt.Errorf("设置MAC地址失败: %v", err)
		}
		logger.OK("MAC地址设置成功: %s", macAddress)
	}
	
	if ingress != "" {
		_, err := c.exec(ctx, "config", "device", "set", name, "eth0", "limits.ingress", ingress)
		if err != nil {
			logger.Warn("设置入站带宽失败: %v", err)
		}
	}
	
	if egress != "" {
		_, err := c.exec(ctx, "config", "device", "set", name, "eth0", "limits.egress", egress)
		if err != nil {
			logger.Warn("设置出站带宽失败: %v", err)
		}
	}
	
	return nil
}

func (c *Client) GetContainerMAC(ctx context.Context, name string) (string, error) {
	info, err := c.GetContainerInfo(ctx, name)
	if err != nil {
		return "", err
	}
	
	if info.State == nil || info.State.Network == nil {
		return "", fmt.Errorf("容器网络信息不可用")
	}
	
	eth0Data, exists := info.State.Network["eth0"]
	if !exists {
		return "", fmt.Errorf("未找到 eth0 网卡")
	}
	
	if iface, ok := eth0Data.(map[string]interface{}); ok {
		if hwaddr, ok := iface["hwaddr"].(string); ok {
			return hwaddr, nil
		}
	}
	
	return "", fmt.Errorf("未找到MAC地址")
}

func (c *Client) CreateContainerWithConfig(ctx context.Context, name, image, storagePool string, cpu int, memory, disk, ingress, egress string, allowNesting, memorySwap, privileged bool, macAddress, cpuAllowance, ioRead, ioWrite string, processesLimit int) error {
	if err := ValidateContainerName(name); err != nil {
		return err
	}
	if err := ValidateImageName(image); err != nil {
		return err
	}
	if err := ValidateStoragePoolName(storagePool); err != nil {
		return err
	}
	logger.Info("创建容器: %s, 镜像: %s, 存储池: %s", name, image, storagePool)
	args := []string{"init", image, name, "-s", storagePool}
	
	var configArgs []string
	
	if cpu > 0 {
		configArgs = append(configArgs, fmt.Sprintf("limits.cpu=%d", cpu))
	}
	
	if memory != "" {
		configArgs = append(configArgs, fmt.Sprintf("limits.memory=%s", memory))
	}
	
	if allowNesting {
		configArgs = append(configArgs, "security.nesting=true")
	} else {
		configArgs = append(configArgs, "security.nesting=false")
	}
	
	if memorySwap {
		configArgs = append(configArgs, "limits.memory.swap=true")
	} else {
		configArgs = append(configArgs, "limits.memory.swap=false")
	}
	
	if privileged {
		configArgs = append(configArgs, "security.privileged=true")
	} else {
		configArgs = append(configArgs, "security.privileged=false")
	}
	
	if cpuAllowance != "" {
		configArgs = append(configArgs, fmt.Sprintf("limits.cpu.allowance=%s", cpuAllowance))
	}
	
	if processesLimit > 0 {
		configArgs = append(configArgs, fmt.Sprintf("limits.processes=%d", processesLimit))
	}
	
	configArgs = append(configArgs, "limits.memory.swap.priority=0")
	
	if len(configArgs) > 0 {
		for _, config := range configArgs {
			args = append(args, "--config", config)
		}
	}
	
	logger.Info("执行LXC命令: lxc %s", strings.Join(args, " "))
	_, err := c.exec(ctx, args...)
	if err != nil {
		return fmt.Errorf("创建容器失败: %v", err)
	}
	
	logger.OK("容器初始化成功: %s", name)
	
	needCustomDevice := macAddress != "" || ingress != "" || egress != ""
	
	if needCustomDevice {
		deviceArgs := []string{"config", "device", "override", name, "eth0"}
		
		if macAddress != "" {
			deviceArgs = append(deviceArgs, fmt.Sprintf("hwaddr=%s", macAddress))
			logger.Info("设置固定MAC地址: %s", macAddress)
		}
		
		if ingress != "" {
			deviceArgs = append(deviceArgs, fmt.Sprintf("limits.ingress=%s", ingress))
		}
		if egress != "" {
			deviceArgs = append(deviceArgs, fmt.Sprintf("limits.egress=%s", egress))
		}
		
		logger.Info("配置网络设备: lxc %s", strings.Join(deviceArgs, " "))
		_, err = c.exec(ctx, deviceArgs...)
		if err != nil {
			return fmt.Errorf("配置网络设备失败: %v", err)
		}
		
		if macAddress != "" {
			logger.OK("MAC地址已固定: %s，重装后IP将保持不变", macAddress)
		}
	}
	
	if disk != "" {
		diskArgs := []string{"config", "device", "set", name, "root", "size", disk}
		logger.Info("设置磁盘大小: %s", disk)
		_, err = c.exec(ctx, diskArgs...)
		if err != nil {
			logger.Warn("设置磁盘大小失败: %v", err)
		}
	}
	
	if ioRead != "" {
		diskArgs := []string{"config", "device", "set", name, "root", "limits.read", ioRead}
		logger.Info("设置磁盘读取限制: %s", ioRead)
		_, err = c.exec(ctx, diskArgs...)
		if err != nil {
			logger.Warn("设置磁盘读取限制失败: %v", err)
		}
	}
	
	if ioWrite != "" {
		diskArgs := []string{"config", "device", "set", name, "root", "limits.write", ioWrite}
		logger.Info("设置磁盘写入限制: %s", ioWrite)
		_, err = c.exec(ctx, diskArgs...)
		if err != nil {
			logger.Warn("设置磁盘写入限制失败: %v", err)
		}
	}
	
	logger.OK("容器创建成功: %s", name)
	return nil
}

func (c *Client) CreateContainer(ctx context.Context, name, image string) error {
	return c.CreateContainerWithConfig(ctx, name, image, "default", 0, "", "", "", "", false, false, false, "", "", "", "", 0)
}

func (c *Client) StartContainer(ctx context.Context, name string) error {
	if err := ValidateContainerName(name); err != nil {
		return err
	}
	logger.Info("启动容器: %s", name)
	_, err := c.exec(ctx, "start", name)
	if err != nil {
		return fmt.Errorf("启动容器失败: %v", err)
	}
	logger.OK("容器启动成功: %s", name)
	return nil
}

func (c *Client) StopContainer(ctx context.Context, name string) error {
	if err := ValidateContainerName(name); err != nil {
		return err
	}
	logger.Info("停止容器: %s", name)
	_, err := c.exec(ctx, "stop", name, "--force")
	if err != nil {
		return fmt.Errorf("停止容器失败: %v", err)
	}
	logger.OK("容器停止成功: %s", name)
	return nil
}

func (c *Client) RestartContainer(ctx context.Context, name string) error {
	if err := ValidateContainerName(name); err != nil {
		return err
	}
	logger.Info("重启容器: %s", name)
	_, err := c.exec(ctx, "restart", name, "--force")
	if err != nil {
		return fmt.Errorf("重启容器失败: %v", err)
	}
	logger.OK("容器重启成功: %s", name)
	return nil
}

func (c *Client) PauseContainer(ctx context.Context, name string) error {
	if err := ValidateContainerName(name); err != nil {
		return err
	}
	logger.Info("暂停容器: %s", name)
	_, err := c.exec(ctx, "pause", name)
	if err != nil {
		return fmt.Errorf("暂停容器失败: %v", err)
	}
	logger.OK("容器暂停成功: %s", name)
	return nil
}

func (c *Client) ResumeContainer(ctx context.Context, name string) error {
	if err := ValidateContainerName(name); err != nil {
		return err
	}
	logger.Info("恢复容器: %s", name)
	_, err := c.exec(ctx, "start", name)
	if err != nil {
		return fmt.Errorf("恢复容器失败: %v", err)
	}
	logger.OK("容器恢复成功: %s", name)
	return nil
}

func (c *Client) DeleteContainer(ctx context.Context, name string) error {
	if err := ValidateContainerName(name); err != nil {
		return err
	}
	logger.Info("删除容器: %s", name)
	
	if !c.ContainerExists(ctx, name) {
		logger.Info("容器不存在，跳过LXD删除: %s", name)
		return nil
	}
	
	c.StopContainer(ctx, name)
	_, err := c.exec(ctx, "delete", name, "--force")
	if err != nil {
		return fmt.Errorf("删除容器失败: %v", err)
	}
	logger.OK("容器删除成功: %s", name)
	return nil
}

func (c *Client) RebuildContainer(ctx context.Context, name, image string) error {
	if err := ValidateContainerName(name); err != nil {
		return err
	}
	if err := ValidateImageName(image); err != nil {
		return err
	}
	logger.Info("重装容器: %s, 新镜像: %s", name, image)
	_, err := c.exec(ctx, "rebuild", image, name, "--force")
	if err != nil {
		return fmt.Errorf("重装容器失败: %v", err)
	}
	logger.OK("容器重装成功: %s", name)
	return nil
}

func (c *Client) GetContainerStatus(ctx context.Context, name string) (string, error) {
	if err := ValidateContainerName(name); err != nil {
		return "", err
	}
	type State struct {
		Status string `json:"status"`
	}
	var states []State
	err := c.execJSON(ctx, &states, "list", name)
	if err != nil {
		return "", err
	}
	if len(states) == 0 {
		return "", fmt.Errorf("容器不存在")
	}
	return states[0].Status, nil
}

func (c *Client) ContainerExists(ctx context.Context, name string) bool {
	if err := ValidateContainerName(name); err != nil {
		return false
	}
	_, err := c.exec(ctx, "info", name)
	return err == nil
}

func (c *Client) ExecInContainer(ctx context.Context, name string, cmd []string) (string, error) {
	if err := ValidateContainerName(name); err != nil {
		return "", err
	}
	args := append([]string{"exec", name, "--"}, cmd...)
	return c.exec(ctx, args...)
}

func (c *Client) ExecInContainerWithStdin(ctx context.Context, name, stdin string, cmd []string) (string, error) {
	if err := ValidateContainerName(name); err != nil {
		return "", err
	}
	args := append([]string{"exec", name, "--"}, cmd...)
	return c.execWithStdin(ctx, stdin, args...)
}

func (c *Client) SetContainerConfig(ctx context.Context, name, key, value string) error {
	if err := ValidateContainerName(name); err != nil {
		return err
	}
	_, err := c.exec(ctx, "config", "set", name, key, value)
	return err
}

func (c *Client) SetRootPassword(ctx context.Context, name, password string) error {
	if err := ValidateContainerName(name); err != nil {
		return err
	}
	logger.Info("设置容器 %s 的root密码", name)
	
	time.Sleep(2 * time.Second)
	
	encoded := base64.StdEncoding.EncodeToString([]byte("root:" + password))
	cmd := []string{"exec", name, "--", "sh", "-c", fmt.Sprintf("echo %s | base64 -d | chpasswd", encoded)}
	_, err := c.exec(ctx, cmd...)
	if err != nil {
		return fmt.Errorf("设置root密码失败: %v", err)
	}
	
	logger.OK("容器 %s 的root密码设置成功", name)
	return nil
}

func (c *Client) GetContainerIP(ctx context.Context, name string) (string, error) {
	info, err := c.GetContainerInfo(ctx, name)
	if err != nil {
		return "", err
	}
	
	if info.State == nil || info.State.Network == nil {
		return "", fmt.Errorf("容器网络信息不可用")
	}
	
	eth0Data, exists := info.State.Network["eth0"]
	if !exists {
		return "", fmt.Errorf("未找到 eth0 网卡")
	}
	
	if iface, ok := eth0Data.(map[string]interface{}); ok {
		if addresses, ok := iface["addresses"].([]interface{}); ok {
			for _, addr := range addresses {
				if addrMap, ok := addr.(map[string]interface{}); ok {
					if family, ok := addrMap["family"].(string); ok && family == "inet" {
						if address, ok := addrMap["address"].(string); ok {
							if !isInvalidIPv4(address) {
								logger.Info("从容器 %s eth0 网卡获取到IPv4: %s", name, address)
								return address, nil
							}
						}
					}
				}
			}
		}
	}
	
	return "", fmt.Errorf("eth0 网卡未找到有效的IPv4地址")
}

func isInvalidIPv4(ip string) bool {
	if ip == "127.0.0.1" {
		return true
	}
	if strings.HasPrefix(ip, "169.254.") {
		return true
	}
	return false
}

func (c *Client) UpdateContainerConfig(ctx context.Context, name string, cpu int, memory, disk, ingress, egress, cpuAllowance, ioRead, ioWrite string, processesLimit int, privileged, memorySwap, allowNesting *bool) error {
	if err := ValidateContainerName(name); err != nil {
		return err
	}
	logger.Info("更新容器配置: %s", name)

	if cpu > 0 {
		_, err := c.exec(ctx, "config", "set", name, fmt.Sprintf("limits.cpu=%d", cpu))
		if err != nil {
			return fmt.Errorf("设置CPU失败: %v", err)
		}
	}

	if memory != "" {
		_, err := c.exec(ctx, "config", "set", name, fmt.Sprintf("limits.memory=%s", memory))
		if err != nil {
			return fmt.Errorf("设置内存失败: %v", err)
		}
	}

	if cpuAllowance != "" {
		c.exec(ctx, "config", "set", name, fmt.Sprintf("limits.cpu.allowance=%s", cpuAllowance))
	}

	if processesLimit > 0 {
		c.exec(ctx, "config", "set", name, fmt.Sprintf("limits.processes=%d", processesLimit))
	}

	if privileged != nil {
		val := "false"
		if *privileged {
			val = "true"
		}
		c.exec(ctx, "config", "set", name, fmt.Sprintf("security.privileged=%s", val))
	}

	if memorySwap != nil {
		val := "true"
		if !*memorySwap {
			val = "false"
		}
		c.exec(ctx, "config", "set", name, fmt.Sprintf("limits.memory.swap=%s", val))
	}

	if allowNesting != nil {
		val := "false"
		if *allowNesting {
			val = "true"
		}
		c.exec(ctx, "config", "set", name, fmt.Sprintf("security.nesting=%s", val))
	}

	if ingress != "" {
		c.exec(ctx, "config", "device", "set", name, "eth0", "limits.ingress", ingress)
	}

	if egress != "" {
		c.exec(ctx, "config", "device", "set", name, "eth0", "limits.egress", egress)
	}

	if disk != "" {
		_, err := c.exec(ctx, "config", "device", "set", name, "root", "size", disk)
		if err != nil {
			return fmt.Errorf("设置磁盘大小失败: %v", err)
		}
	}

	if ioRead != "" {
		c.exec(ctx, "config", "device", "set", name, "root", "limits.read", ioRead)
	}

	if ioWrite != "" {
		c.exec(ctx, "config", "device", "set", name, "root", "limits.write", ioWrite)
	}

	logger.OK("容器配置更新成功: %s", name)
	return nil
}

// GetContainerIPv6 获取容器的内网IPv6地址
func (c *Client) GetContainerIPv6(ctx context.Context, name string) (string, error) {
	info, err := c.GetContainerInfo(ctx, name)
	if err != nil {
		return "", err
	}
	
	if info.State == nil || info.State.Network == nil {
		return "", fmt.Errorf("容器网络信息不可用")
	}
	
	eth0Data, exists := info.State.Network["eth0"]
	if !exists {
		return "", fmt.Errorf("未找到 eth0 网卡")
	}
	
	if iface, ok := eth0Data.(map[string]interface{}); ok {
		if addresses, ok := iface["addresses"].([]interface{}); ok {
			for _, addr := range addresses {
				if addrMap, ok := addr.(map[string]interface{}); ok {
					if family, ok := addrMap["family"].(string); ok && family == "inet6" {
						if address, ok := addrMap["address"].(string); ok {
							// 跳过链路本地地址 (fe80::)
							if !strings.HasPrefix(address, "fe80:") {
								logger.Info("从容器 %s eth0 网卡获取到IPv6: %s", name, address)
								return address, nil
							}
						}
					}
				}
			}
		}
	}
	
	return "", fmt.Errorf("eth0 网卡未找到有效的IPv6地址")
}

func (c *Client) SetContainerDNS(ctx context.Context, name string, dnsServers []string) error {
	if err := ValidateContainerName(name); err != nil {
		return err
	}
	if len(dnsServers) == 0 {
		return fmt.Errorf("DNS服务器列表不能为空")
	}

	var builder strings.Builder
	for _, dns := range dnsServers {
		trimmed := strings.TrimSpace(dns)
		if net.ParseIP(trimmed) == nil {
			return fmt.Errorf("无效的DNS地址: %s", dns)
		}
		builder.WriteString("nameserver ")
		builder.WriteString(trimmed)
		builder.WriteString("\n")
	}

	_, err := c.ExecInContainerWithStdin(ctx, name, builder.String(), []string{"tee", "/etc/resolv.conf"})
	if err != nil {
		return fmt.Errorf("设置DNS失败: %v", err)
	}

	logger.OK("容器 %s DNS设置成功: %v", name, dnsServers)
	return nil
}

func (c *Client) GetContainerDNS(ctx context.Context, name string) ([]string, error) {
	if err := ValidateContainerName(name); err != nil {
		return nil, err
	}
	output, err := c.ExecInContainer(ctx, name, []string{"cat", "/etc/resolv.conf"})
	if err != nil {
		return nil, fmt.Errorf("获取DNS失败: %v", err)
	}
	
	var dnsServers []string
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "nameserver ") {
			dns := strings.TrimPrefix(line, "nameserver ")
			dns = strings.TrimSpace(dns)
			if dns != "" {
				dnsServers = append(dnsServers, dns)
			}
		}
	}
	
	return dnsServers, nil
}
