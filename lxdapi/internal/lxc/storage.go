package lxc

import (
	"context"
	"encoding/json"
	"fmt"
	"lxdapi/pkg/logger"
)

type StoragePoolInfo struct {
	Name        string                 `json:"name"`
	Driver      string                 `json:"driver"`
	Description string                 `json:"description"`
	Status      string                 `json:"status"`
	Config      map[string]interface{} `json:"config"`
	UsedBy      []string               `json:"used_by"`
}

type StoragePoolResources struct {
	Space struct {
		Total int64 `json:"total"`
		Used  int64 `json:"used"`
	} `json:"space"`
}

func (c *Client) ListStoragePools(ctx context.Context) ([]StoragePoolInfo, error) {
	logger.Info("获取LXD存储池列表")

	output, err := c.exec(ctx, "storage", "list", "--format", "json")
	if err != nil {
		return nil, fmt.Errorf("获取存储池列表失败: %v", err)
	}

	var pools []StoragePoolInfo
	if err := json.Unmarshal([]byte(output), &pools); err != nil {
		return nil, fmt.Errorf("解析存储池列表失败: %v", err)
	}

	logger.OK("获取到 %d 个存储池", len(pools))
	return pools, nil
}

func (c *Client) GetStoragePoolResources(ctx context.Context, name string) (*StoragePoolResources, error) {
	if err := ValidateStoragePoolName(name); err != nil {
		return nil, err
	}
	output, err := c.exec(ctx, "query", "/1.0/storage-pools/"+name+"/resources")
	if err != nil {
		return nil, fmt.Errorf("获取存储池资源失败: %v", err)
	}

	var resources StoragePoolResources
	if err := json.Unmarshal([]byte(output), &resources); err != nil {
		return nil, fmt.Errorf("解析存储池资源失败: %v", err)
	}

	return &resources, nil
}
