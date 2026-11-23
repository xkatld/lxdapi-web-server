package acme

import (
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	DEFAULT_WEBROOT    = "/var/www/html"
	DEFAULT_RELOAD_CMD = "systemctl reload lxdapi || systemctl restart lxdapi"
	RENEWAL_THRESHOLD  = 72 * time.Hour
)

type Manager struct {
	certDir string
}

func NewManager(cert_dir string) (*Manager, error) {
	if strings.TrimSpace(cert_dir) == "" {
		cert_dir = "certs/acme"
	}
	abs_dir, err := filepath.Abs(cert_dir)
	if err != nil {
		return nil, fmt.Errorf("解析证书目录失败: %w", err)
	}
	if err := os.MkdirAll(abs_dir, 0755); err != nil {
		return nil, fmt.Errorf("创建证书目录失败: %w", err)
	}
	return &Manager{
		certDir: abs_dir,
	}, nil
}

func (m *Manager) GetCertDir() string {
	return m.certDir
}

func (m *Manager) GetCertPaths() (string, string) {
	return filepath.Join(m.certDir, "server.crt"), filepath.Join(m.certDir, "server.key")
}

func (m *Manager) acme_bin() string {
	home_dir, err := os.UserHomeDir()
	if err == nil {
		candidate := filepath.Join(home_dir, ".acme.sh", "acme.sh")
		if _, stat_err := os.Stat(candidate); stat_err == nil {
			return candidate
		}
	}
	root_candidate := "/root/.acme.sh/acme.sh"
	if _, stat_err := os.Stat(root_candidate); stat_err == nil {
		return root_candidate
	}
	if path_bin, look_err := exec.LookPath("acme.sh"); look_err == nil {
		return path_bin
	}
	return ""
}

func (m *Manager) IsInstalled() bool {
	return m.acme_bin() != ""
}

func random_install_email() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成随机安装邮箱失败: %w", err)
	}
	return fmt.Sprintf("lxdapi-%s@example.com", hex.EncodeToString(buf)), nil
}

func (m *Manager) Install(email string) error {
	trimmed_email := strings.TrimSpace(email)
	if err := validate_email(trimmed_email); err != nil {
		return err
	}
	if m.IsInstalled() {
		if trimmed_email != "" {
			return m.register_account(m.acme_bin(), trimmed_email)
		}
		return nil
	}

	install_email, err := random_install_email()
	if err != nil {
		return err
	}

	if err := m.download_and_run_installer(install_email); err != nil {
		return err
	}
	bin := m.acme_bin()
	if bin == "" {
		return fmt.Errorf("acme.sh 安装完成但未找到可执行文件")
	}
	ca_cmd := exec.Command(bin, "--set-default-ca", "--server", "letsencrypt")
	if ca_out, ca_err := ca_cmd.CombinedOutput(); ca_err != nil {
		return fmt.Errorf("设置默认 CA 失败: %v, 输出: %s", ca_err, strings.TrimSpace(string(ca_out)))
	}
	if trimmed_email != "" {
		if reg_err := m.register_account(bin, trimmed_email); reg_err != nil {
			return reg_err
		}
	}
	return nil
}

func (m *Manager) GetPublicIP() (string, error) {
	endpoints := []string{
		"https://api.ipify.org",
		"https://ifconfig.me/ip",
		"https://icanhazip.com",
	}
	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	for _, endpoint := range endpoints {
		resp, err := client.Get(endpoint)
		if err != nil {
			continue
		}
		body, read_err := io.ReadAll(io.LimitReader(resp.Body, 128))
		resp.Body.Close()
		if read_err != nil {
			continue
		}
		ip_str := strings.TrimSpace(string(body))
		if net.ParseIP(ip_str) != nil {
			return ip_str, nil
		}
	}
	return "", fmt.Errorf("无法获取服务器公网 IP")
}

func (m *Manager) CertExists() bool {
	cert_path, key_path := m.GetCertPaths()
	if _, err := os.Stat(cert_path); err != nil {
		return false
	}
	if _, err := os.Stat(key_path); err != nil {
		return false
	}
	return true
}

func (m *Manager) GetCertExpiry() (time.Time, error) {
	cert_path, _ := m.GetCertPaths()
	cert_pem, err := os.ReadFile(cert_path)
	if err != nil {
		return time.Time{}, fmt.Errorf("读取证书失败: %w", err)
	}
	block, _ := pem.Decode(cert_pem)
	if block == nil {
		return time.Time{}, fmt.Errorf("解析证书 PEM 失败")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, fmt.Errorf("解析证书内容失败: %w", err)
	}
	return cert.NotAfter, nil
}

func (m *Manager) NeedsRenewal() bool {
	if !m.CertExists() {
		return true
	}
	expiry, err := m.GetCertExpiry()
	if err != nil {
		return true
	}
	return time.Until(expiry) <= RENEWAL_THRESHOLD
}

func (m *Manager) register_account(bin, email string) error {
	trimmed_email := strings.TrimSpace(email)
	if trimmed_email == "" {
		return nil
	}
	if err := validate_email(trimmed_email); err != nil {
		return err
	}
	cmd := exec.Command(bin, "--register-account", "-m", trimmed_email, "--server", "letsencrypt")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("注册 ACME 账号失败: %w, 输出: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (m *Manager) install_cert_to_disk(bin, target string) error {
	cert_path, key_path := m.GetCertPaths()
	abs_cert, err := filepath.Abs(cert_path)
	if err != nil {
		return fmt.Errorf("解析证书绝对路径失败: %w", err)
	}
	abs_key, err := filepath.Abs(key_path)
	if err != nil {
		return fmt.Errorf("解析私钥绝对路径失败: %w", err)
	}
	if err := os.MkdirAll(m.certDir, 0755); err != nil {
		return fmt.Errorf("创建证书目录失败: %w", err)
	}
	args := []string{
		"--install-cert",
		"-d", target,
		"--key-file", abs_key,
		"--fullchain-file", abs_cert,
		"--reloadcmd", DEFAULT_RELOAD_CMD,
	}
	cmd := exec.Command(bin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("安装证书失败: %v, 输出: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (m *Manager) prepare_execution(server_ip, email, webroot string) (string, string, string, error) {
	if err := validate_acme_params(server_ip, email, webroot); err != nil {
		return "", "", "", err
	}
	bin := m.acme_bin()
	if bin == "" {
		return "", "", "", fmt.Errorf("acme.sh 未安装")
	}
	target := strings.TrimSpace(server_ip)
	if target == "" {
		detected_ip, err := m.GetPublicIP()
		if err != nil {
			return "", "", "", err
		}
		target = detected_ip
	}
	root_dir := strings.TrimSpace(webroot)
	if root_dir == "" {
		root_dir = DEFAULT_WEBROOT
	}
	if err := validate_acme_params(target, email, root_dir); err != nil {
		return "", "", "", err
	}
	if err := os.MkdirAll(root_dir, 0755); err != nil {
		return "", "", "", fmt.Errorf("创建 webroot 目录失败: %w", err)
	}
	if err := m.register_account(bin, email); err != nil {
		return "", "", "", err
	}
	return bin, target, root_dir, nil
}

func (m *Manager) run_acme_cert(action_label, bin, target string, args []string) error {
	cmd := exec.Command(bin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s失败: %v, 输出: %s", action_label, err, strings.TrimSpace(string(output)))
	}
	return m.install_cert_to_disk(bin, target)
}

func (m *Manager) Issue(server_ip, email, webroot string) error {
	bin, target, root_dir, err := m.prepare_execution(server_ip, email, webroot)
	if err != nil {
		return err
	}
	args := []string{
		"--issue",
		"-d", target,
		"-w", root_dir,
		"--server", "letsencrypt",
	}
	if net.ParseIP(target) != nil {
		args = append(args, "--certificate-profile", "shortlived", "--days", "5")
	}
	return m.run_acme_cert("签发证书", bin, target, args)
}

func (m *Manager) Renew(server_ip, email, webroot string) error {
	bin, target, root_dir, err := m.prepare_execution(server_ip, email, webroot)
	if err != nil {
		return err
	}
	args := []string{
		"--renew",
		"-d", target,
		"-w", root_dir,
	}
	return m.run_acme_cert("续签证书", bin, target, args)
}

func (m *Manager) download_and_run_installer(install_email string) error {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	resp, err := client.Get("https://get.acme.sh")
	if err != nil {
		return fmt.Errorf("下载 acme.sh 安装脚本失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载 acme.sh 安装脚本返回异常状态码: %d", resp.StatusCode)
	}

	script_bytes, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return fmt.Errorf("读取 acme.sh 安装脚本失败: %w", err)
	}
	script_str := string(script_bytes)
	if len(script_bytes) == 0 || !strings.Contains(script_str, "acme.sh") || (!strings.HasPrefix(script_str, "#!/bin/sh") && !strings.HasPrefix(script_str, "#!/usr/bin/env sh")) {
		return fmt.Errorf("下载的 acme.sh 安装脚本校验未通过")
	}

	tmp_file, err := os.CreateTemp("", "acme-install-*.sh")
	if err != nil {
		return fmt.Errorf("创建临时安装脚本失败: %w", err)
	}
	tmp_path := tmp_file.Name()
	defer os.Remove(tmp_path)

	if _, err := tmp_file.Write(script_bytes); err != nil {
		tmp_file.Close()
		return fmt.Errorf("写入临时安装脚本失败: %w", err)
	}
	if err := tmp_file.Chmod(0700); err != nil {
		tmp_file.Close()
		return fmt.Errorf("设置临时安装脚本权限失败: %w", err)
	}
	if err := tmp_file.Close(); err != nil {
		return fmt.Errorf("关闭临时安装脚本失败: %w", err)
	}

	cmd := exec.Command("sh", tmp_path, "-s", "email="+install_email)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("安装 acme.sh 失败: %v, 输出: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
