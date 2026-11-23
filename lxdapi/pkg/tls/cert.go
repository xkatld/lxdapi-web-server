package tls

import (
	"crypto/rand"
	"crypto/rsa"
	cryptotls "crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type CertificateManager struct {
	certFile string
	keyFile  string
}

func NewCertificateManager(certFile, keyFile string) *CertificateManager {
	return &CertificateManager{
		certFile: certFile,
		keyFile:  keyFile,
	}
}

func (cm *CertificateManager) EnsureDirectories() error {
	certDir := filepath.Dir(cm.certFile)
	if err := os.MkdirAll(certDir, 0755); err != nil {
		return fmt.Errorf("创建证书目录失败: %v", err)
	}
	
	keyDir := filepath.Dir(cm.keyFile)
	if err := os.MkdirAll(keyDir, 0755); err != nil {
		return fmt.Errorf("创建密钥目录失败: %v", err)
	}
	
	return nil
}

func (cm *CertificateManager) CertificateExists() bool {
	if _, err := os.Stat(cm.certFile); os.IsNotExist(err) {
		return false
	}
	if _, err := os.Stat(cm.keyFile); os.IsNotExist(err) {
		return false
	}
	return true
}

func read_and_parse_cert(cert_file string) (*x509.Certificate, error) {
	cert_pem, err := os.ReadFile(cert_file)
	if err != nil {
		return nil, fmt.Errorf("读取证书文件失败: %v", err)
	}
	block, _ := pem.Decode(cert_pem)
	if block == nil {
		return nil, fmt.Errorf("解析证书PEM失败")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("解析证书失败: %v", err)
	}
	return cert, nil
}

func (cm *CertificateManager) ValidateCertificate() bool {
	if !cm.CertificateExists() {
		return false
	}
	cert, err := read_and_parse_cert(cm.certFile)
	if err != nil {
		return false
	}
	now := time.Now()
	if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
		return false
	}
	return true
}

type GenerateOptions struct {
	Organization  string
	Country       string
	Province      string
	Locality      string
	ServerIPs     []string
	ServerDomains []string
	ValidityDays  int
}

func (cm *CertificateManager) GenerateSelfSignedCert(opts GenerateOptions) error {
	if err := cm.EnsureDirectories(); err != nil {
		return err
	}
	
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("生成私钥失败: %v", err)
	}
	
	notBefore := time.Now()
	notAfter := notBefore.Add(time.Duration(opts.ValidityDays) * 24 * time.Hour)
	
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return fmt.Errorf("生成序列号失败: %v", err)
	}
	
	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{opts.Organization},
			Country:      []string{opts.Country},
			Province:     []string{opts.Province},
			Locality:     []string{opts.Locality},
			CommonName:   "lxdapi Self-Signed Certificate",
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	
	for _, ip := range opts.ServerIPs {
		if parsedIP := net.ParseIP(ip); parsedIP != nil {
			template.IPAddresses = append(template.IPAddresses, parsedIP)
		}
	}
	
	for _, domain := range opts.ServerDomains {
		template.DNSNames = append(template.DNSNames, domain)
	}
	
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return fmt.Errorf("创建证书失败: %v", err)
	}
	
	certOut, err := os.Create(cm.certFile)
	if err != nil {
		return fmt.Errorf("创建证书文件失败: %v", err)
	}
	defer certOut.Close()
	
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		return fmt.Errorf("编码证书失败: %v", err)
	}
	
	keyOut, err := os.OpenFile(cm.keyFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("创建私钥文件失败: %v", err)
	}
	defer keyOut.Close()
	
	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return fmt.Errorf("编码私钥失败: %v", err)
	}
	
	if err := pem.Encode(keyOut, &pem.Block{Type: "PRIVATE KEY", Bytes: privBytes}); err != nil {
		return fmt.Errorf("写入私钥失败: %v", err)
	}
	
	return nil
}

func (cm *CertificateManager) GetCertificateInfo() (map[string]interface{}, error) {
	if !cm.CertificateExists() {
		return nil, fmt.Errorf("证书文件不存在")
	}
	cert, err := read_and_parse_cert(cm.certFile)
	if err != nil {
		return nil, err
	}
	
	info := map[string]interface{}{
		"subject":      cert.Subject.String(),
		"issuer":       cert.Issuer.String(),
		"not_before":   cert.NotBefore.Format(time.RFC3339),
		"not_after":    cert.NotAfter.Format(time.RFC3339),
		"dns_names":    cert.DNSNames,
		"ip_addresses": make([]string, 0),
		"serial":       cert.SerialNumber.String(),
	}
	
	for _, ip := range cert.IPAddresses {
		info["ip_addresses"] = append(info["ip_addresses"].([]string), ip.String())
	}
	
	return info, nil
}

type DynamicCertLoader struct {
	mu   sync.RWMutex
	cert *cryptotls.Certificate
}

var GlobalCertLoader = NewDynamicCertLoader()

func NewDynamicCertLoader() *DynamicCertLoader {
	return &DynamicCertLoader{}
}

func validate_tls_cert(tls_cert cryptotls.Certificate) error {
	if len(tls_cert.Certificate) == 0 {
		return fmt.Errorf("未找到有效的证书数据")
	}
	if _, err := x509.ParseCertificate(tls_cert.Certificate[0]); err != nil {
		return fmt.Errorf("解析证书失败: %w", err)
	}
	return nil
}

func ValidateKeyPair(certPEM, keyPEM string) error {
	tls_cert, err := cryptotls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return fmt.Errorf("证书与私钥不匹配或格式无效: %w", err)
	}
	return validate_tls_cert(tls_cert)
}

func (d *DynamicCertLoader) LoadFromFile(certFile, keyFile string) error {
	tls_cert, err := cryptotls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return fmt.Errorf("加载证书与私钥失败: %w", err)
	}
	if err := validate_tls_cert(tls_cert); err != nil {
		return err
	}

	d.mu.Lock()
	d.cert = &tls_cert
	d.mu.Unlock()
	return nil
}

func (d *DynamicCertLoader) GetCertificate(hello *cryptotls.ClientHelloInfo) (*cryptotls.Certificate, error) {
	d.mu.RLock()
	cert := d.cert
	d.mu.RUnlock()
	if cert == nil {
		return nil, fmt.Errorf("尚未加载可用 TLS 证书")
	}
	return cert, nil
}

