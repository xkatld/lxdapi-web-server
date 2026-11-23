package core

import (
	"gopkg.in/yaml.v3"
	"os"
)

type Config struct {
	System   SystemConfig   `yaml:"system"`
	LXC      LXCConfig      `yaml:"lxc"`
	Traffic  TrafficConfig  `yaml:"traffic"`
	Task     TaskConfig     `yaml:"task"`
	Admin    AdminConfig    `yaml:"admin"`
	Plugins  PluginsConfig  `yaml:"plugins"`
}

type SystemConfig struct {
	Server   ServerConfig   `yaml:"server"`
	Security SecurityConfig `yaml:"security"`
	Logger   LoggerConfig   `yaml:"logger"`
	Database DatabaseConfig `yaml:"database"`
}

type ServerConfig struct {
	Host string    `yaml:"host"`
	Port int       `yaml:"port"`
	Mode string    `yaml:"mode"`
	TLS  TLSConfig `yaml:"tls"`
}

type TLSConfig struct {
	Enabled      bool   `yaml:"enabled"`
	CertFile     string `yaml:"cert_file"`
	KeyFile      string `yaml:"key_file"`
	AutoGenerate bool   `yaml:"auto_generate"`
}

type SecurityConfig struct {
	APIHash string `yaml:"api_hash"`
}

type LoggerConfig struct {
	Level    string `yaml:"level"`
	Colorful bool   `yaml:"colorful"`
}

type LXCConfig struct {
	Socket         string `yaml:"socket"`
	Timeout        int    `yaml:"timeout"`
	DefaultStorage string `yaml:"default_storage"`
}

type TrafficConfig struct {
	Enabled   bool `yaml:"enabled"`
	Interval  int  `yaml:"interval"`
	BatchSize int  `yaml:"batch_size"`
}

type TaskConfig struct {
	Enabled         bool        `yaml:"enabled"`
	Backend         string      `yaml:"backend"`
	Workers         int         `yaml:"workers"`
	QueueSize       int         `yaml:"queue_size"`
	Timeout         int         `yaml:"timeout"`
	AutoCleanupDays int         `yaml:"auto_cleanup_days"`
	Redis           RedisConfig `yaml:"redis"`
}

type RedisConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

type DatabaseConfig struct {
	Type     string `yaml:"type"`
	SQLite   SQLiteConfig   `yaml:"sqlite"`
	MySQL    MySQLConfig    `yaml:"mysql"`
	Postgres PostgresConfig `yaml:"postgres"`
}

type SQLiteConfig struct {
	Path string `yaml:"path"`
}

type MySQLConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Database string `yaml:"database"`
}

type PostgresConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Database string `yaml:"database"`
	SSLMode  string `yaml:"sslmode"`
}

type PluginsConfig struct {
	Nginx PluginConfig     `yaml:"nginx"`
	Acme  AcmePluginConfig `yaml:"acme"`
}

type PluginConfig struct {
	Enabled bool `yaml:"enabled"`
}

type AcmePluginConfig struct {
	Enabled bool   `yaml:"enabled"`
	CertDir string `yaml:"cert_dir"`
}

type AdminConfig struct {
	Enabled       bool   `yaml:"enabled"`
	Username      string `yaml:"username"`
	Password      string `yaml:"password"`
	SessionSecret string `yaml:"session_secret"`
}

var GlobalConfig *Config

func LoadConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	GlobalConfig = &Config{}
	return yaml.Unmarshal(data, GlobalConfig)
}

