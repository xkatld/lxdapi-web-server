package nginx

import (
	"bytes"
	"crypto/rand"
	"net"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"lxdapi/models"
	"lxdapi/pkg/logger"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"
)

type ConfigGenerator struct {
	confDir  string
	sitesDir string
	sslDir   string
	template *template.Template
}

func NewConfigGenerator(confDir, sitesDir, sslDir string) *ConfigGenerator {
	tmplPath := filepath.Join(filepath.Dir(confDir), "nginx-default.tmpl")
	tmpl, err := template.ParseFiles(tmplPath)
	if err != nil {
		logger.Error("加载模板失败: %v", err)
		return nil
	}
	
	return &ConfigGenerator{
		confDir:  confDir,
		sitesDir: sitesDir,
		sslDir:   sslDir,
		template: tmpl,
	}
}

func (g *ConfigGenerator) Generate(config *models.NginxConfig, proxies []models.ReverseProxy) error {
	if err := g.cleanupOldConfigs(); err != nil {
		logger.Warn("清理旧配置失败: %v", err)
	}
	
	for _, proxy := range proxies {
		if err := g.generateProxyConfig(&proxy); err != nil {
			logger.Error("生成反向代理配置失败 [%s]: %v", proxy.Domain, err)
			continue
		}
	}
	
	return nil
}

type TemplateData struct {
	GeneratedAt   string
	ContainerName string
	Domain        string
	ContainerIP   string
	ContainerPort int
	SSLEnabled    bool
	SSLCertPath   string
	SSLKeyPath    string
	IPv6Enabled   bool
}

func (g *ConfigGenerator) generateProxyConfig(proxy *models.ReverseProxy) error {
	if err := validateDomain(proxy.Domain); err != nil {
		return err
	}
	if err := validateTargetIP(proxy.TargetIP); err != nil {
		return err
	}
	if err := validateTargetPort(proxy.TargetPort); err != nil {
		return err
	}

	safeDomain := sanitizeDomain(proxy.Domain)
	if safeDomain == "" || strings.Contains(safeDomain, "..") {
		return fmt.Errorf("域名包含非法路径字符")
	}

	var certPath, keyPath string
	var sslEnabled bool
	
	if proxy.Protocol == "https" && proxy.EnableSSL {
		sslEnabled = true
		sslBase, err := filepath.Abs(g.sslDir)
		if err != nil {
			return fmt.Errorf("解析SSL目录失败: %v", err)
		}
		sslBase = filepath.Clean(sslBase)
		certPath = filepath.Clean(filepath.Join(sslBase, fmt.Sprintf("%s.crt", safeDomain)))
		keyPath = filepath.Clean(filepath.Join(sslBase, fmt.Sprintf("%s.key", safeDomain)))
		if !isPathWithinDir(certPath, sslBase) || !isPathWithinDir(keyPath, sslBase) {
			return fmt.Errorf("证书路径超出允许范围")
		}
		
		if proxy.SSLCert != "" && proxy.SSLKey != "" {
			if err := os.WriteFile(certPath, []byte(proxy.SSLCert), 0644); err != nil {
				return fmt.Errorf("写入SSL证书失败: %v", err)
			}
			if err := os.WriteFile(keyPath, []byte(proxy.SSLKey), 0600); err != nil {
				return fmt.Errorf("写入SSL密钥失败: %v", err)
			}
		} else {
			if err := g.generateSelfSignedCert(proxy.Domain, certPath, keyPath); err != nil {
				return fmt.Errorf("生成自签名证书失败: %v", err)
			}
		}
	}
	
	ipv6Enabled := g.hasIPv6()
	data := TemplateData{
		GeneratedAt:   time.Now().Format(time.RFC3339),
		ContainerName: proxy.ContainerName,
		Domain:        proxy.Domain,
		ContainerIP:   proxy.TargetIP,
		ContainerPort: proxy.TargetPort,
		SSLEnabled:    sslEnabled,
		SSLCertPath:   certPath,
		SSLKeyPath:    keyPath,
		IPv6Enabled:   ipv6Enabled,
	}
	
	var buf bytes.Buffer
	if err := g.template.Execute(&buf, data); err != nil {
		return fmt.Errorf("生成配置失败: %v", err)
	}
	config := buf.String()
	
	sitesBase, err := filepath.Abs(g.sitesDir)
	if err != nil {
		return fmt.Errorf("解析站点配置目录失败: %v", err)
	}
	sitesBase = filepath.Clean(sitesBase)
	configPath := filepath.Clean(filepath.Join(sitesBase, fmt.Sprintf("%s.conf", safeDomain)))
	if !isPathWithinDir(configPath, sitesBase) {
		return fmt.Errorf("配置文件路径超出允许范围")
	}
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		return fmt.Errorf("写入配置文件失败: %v", err)
	}
	
	logger.Info("已生成反向代理配置: %s -> %s:%d", proxy.Domain, proxy.TargetIP, proxy.TargetPort)
	return nil
}

func (g *ConfigGenerator) generateSelfSignedCert(domain, certPath, keyPath string) error {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("生成私钥失败: %v", err)
	}
	
	serialNumber, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"lxdapi"},
			CommonName:   domain,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(10, 0, 0), // 10年有效期
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{domain},
	}
	
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return fmt.Errorf("生成证书失败: %v", err)
	}
	
	certFile, err := os.Create(certPath)
	if err != nil {
		return fmt.Errorf("创建证书文件失败: %v", err)
	}
	defer certFile.Close()
	
	if err := pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return fmt.Errorf("编码证书失败: %v", err)
	}
	
	keyFile, err := os.Create(keyPath)
	if err != nil {
		return fmt.Errorf("创建密钥文件失败: %v", err)
	}
	defer keyFile.Close()
	
	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	if err := pem.Encode(keyFile, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: privateKeyBytes}); err != nil {
		return fmt.Errorf("编码私钥失败: %v", err)
	}
	
	if err := os.Chmod(keyPath, 0600); err != nil {
		return fmt.Errorf("设置私钥权限失败: %v", err)
	}
	
	logger.Info("已为 %s 生成自签名证书（10年有效期）", domain)
	return nil
}

func (g *ConfigGenerator) hasIPv6() bool {
	ifaces, err := net.Interfaces()
	if err != nil {
		return false
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			if ipNet.IP.IsGlobalUnicast() && len(ipNet.IP) == net.IPv6len {
				return true
			}
		}
	}
	return false
}

func (g *ConfigGenerator) cleanupOldConfigs() error {
	files, err := filepath.Glob(filepath.Join(g.sitesDir, "*.conf"))
	if err != nil {
		return err
	}
	
	for _, file := range files {
		if err := os.Remove(file); err != nil {
			logger.Warn("删除旧配置失败 %s: %v", file, err)
		}
	}
	
	return nil
}

func isPathWithinDir(targetPath, baseDir string) bool {
	rel, err := filepath.Rel(baseDir, targetPath)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func sanitizeDomain(domain string) string {
	domain = strings.TrimSpace(domain)
	domain = strings.ReplaceAll(domain, "..", "_")
	replacer := strings.NewReplacer(
		":", "_",
		"/", "_",
		"\\", "_",
		" ", "_",
		"\t", "_",
		"\r", "_",
		"\n", "_",
		";", "_",
		"{", "_",
		"}", "_",
	)
	return replacer.Replace(domain)
}
