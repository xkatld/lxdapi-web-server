package models

import "gorm.io/gorm"

type Container struct {
	gorm.Model
	Name         string `gorm:"uniqueIndex;size:255"`
	UserID       string `gorm:"index;size:255"`
	Image        string `gorm:"size:255"`
	Password     string `gorm:"size:255"`
	Status        string `gorm:"size:50"`
	FreezeSource  string `gorm:"size:20;column:freeze_source" json:"freeze_source"`
	TrafficLocked bool   `gorm:"-" json:"traffic_locked"`
	CPU          int
	Memory       int
	Disk         int
	Ingress      int
	Egress       int
	TrafficLimit int
	PrivateIP    string `gorm:"size:50"`
	PrivateIPv6  string `gorm:"size:100"`
	MacAddress   string `gorm:"size:50"`
	Remark       string `gorm:"size:20"`
	
	AllowNesting bool
	MemorySwap   bool
	Privileged   bool
	
	CPUAllowance    int
	IORead          int
	IOWrite         int
	ProcessesLimit  int
	
	CPUUsage        float64 `gorm:"type:decimal(5,2)"`
	MemoryUsage     string  `gorm:"size:50"`
	MemoryUsageRaw  uint64
	DiskUsage       string  `gorm:"size:50"`
	DiskUsageRaw    uint64
	TrafficUsage    string  `gorm:"size:50"`
	TrafficUsageRaw uint64
	IPv4            string  `gorm:"type:text"`
	IPv4PoolLimit   int
	IPv4MappingLimit int
	IPv6            string  `gorm:"type:text"`
	IPv6PoolLimit   int
	IPv6MappingLimit int
	ReverseProxyLimit int
	NetworkMode     string  `gorm:"size:50"`
	ConfigJSON      string  `gorm:"type:text"`
	CreatedAtLXD    string  `gorm:"size:100"`
	LastSync        string  `gorm:"size:100"`
}

type CreateContainerRequest struct {
	Name            string `json:"name" binding:"required"`
	Password        string `json:"password"`
	Image           string `json:"image" binding:"required"`
	CPU             int    `json:"cpu"`
	Memory          int    `json:"memory"`
	Disk            int    `json:"disk"`
	Ingress         int    `json:"ingress"`
	Egress          int    `json:"egress"`
	TrafficLimit    int    `json:"traffic_limit"`
	AllowNesting    bool   `json:"allow_nesting"`
	MemorySwap      bool   `json:"memory_swap"`
	Privileged      bool   `json:"privileged"`
	Username         string `json:"username"`
	Remark          string `json:"remark"`
	IPv4PoolLimit    int    `json:"ipv4_pool_limit"`
	IPv4MappingLimit int    `json:"ipv4_mapping_limit"`
	IPv6PoolLimit    int    `json:"ipv6_pool_limit"`
	IPv6MappingLimit int    `json:"ipv6_mapping_limit"`
	ReverseProxyLimit int   `json:"reverse_proxy_limit"`
	CPUAllowance     int    `json:"cpu_allowance"`
	IORead          int    `json:"io_read"`
	IOWrite         int    `json:"io_write"`
	ProcessesLimit  int    `json:"processes_limit"`
}

type UpdateContainerConfigRequest struct {
	CPU               *int    `json:"cpu"`
	Memory            *int    `json:"memory"`
	Disk              *int    `json:"disk"`
	Ingress           *int    `json:"ingress"`
	Egress            *int    `json:"egress"`
	CPUAllowance      *int    `json:"cpu_allowance"`
	IORead            *int    `json:"io_read"`
	IOWrite           *int    `json:"io_write"`
	ProcessesLimit    *int    `json:"processes_limit"`
	TrafficLimit      *int    `json:"traffic_limit"`
	IPv4PoolLimit     *int    `json:"ipv4_pool_limit"`
	IPv4MappingLimit  *int    `json:"ipv4_mapping_limit"`
	IPv6PoolLimit     *int    `json:"ipv6_pool_limit"`
	IPv6MappingLimit  *int    `json:"ipv6_mapping_limit"`
	ReverseProxyLimit *int    `json:"reverse_proxy_limit"`
	Remark            *string `json:"remark"`
	Privileged        *bool   `json:"privileged"`
	MemorySwap        *bool   `json:"memory_swap"`
	AllowNesting      *bool   `json:"allow_nesting"`
}

