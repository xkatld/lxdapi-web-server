package system

import (
	"os/exec"
	"runtime"
	"strings"
	"os"
)

type SystemInfo struct {
	Name         string `json:"name"`
	Version      string `json:"version"`
	Description  string `json:"description"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	LXDVersion   string `json:"lxd_version,omitempty"`
	Distribution string `json:"distribution,omitempty"`
	Kernel       string `json:"kernel,omitempty"`
}

func GetSystemInfo() *SystemInfo {
	info := &SystemInfo{
		Name:        "lxdapi",
		Version:     "v2.1.3",
		Description: "LXD容器管理后端API服务",
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
	}
	
	if lxdVersion := getLXDVersion(); lxdVersion != "" {
		info.LXDVersion = lxdVersion
	}
	
	if kernel := getKernelVersion(); kernel != "" {
		info.Kernel = kernel
	}
	
	if distro := getDistribution(); distro != "" {
		info.Distribution = distro
	}
	
	return info
}

func getLXDVersion() string {
	cmd := exec.Command("lxc", "version")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func getKernelVersion() string {
	cmd := exec.Command("uname", "-r")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func getDistribution() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			name := strings.TrimPrefix(line, "PRETTY_NAME=")
			name = strings.Trim(name, "\"")
			return name
		}
	}
	
	return ""
}
