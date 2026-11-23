package service

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"lxdapi/internal/db"
	"lxdapi/models"
)

func generateContainerHash() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func CreateContainerCredential(containerName string) (*models.ContainerCredential, error) {
	var existing models.ContainerCredential
	if err := db.DB.Where("container_name = ?", containerName).First(&existing).Error; err == nil {
		return &existing, nil
	}

	hash, err := generateContainerHash()
	if err != nil {
		return nil, fmt.Errorf("生成容器Hash失败: %v", err)
	}

	credential := &models.ContainerCredential{
		ContainerName: containerName,
		Hash:          hash,
	}

	if err := db.DB.Create(credential).Error; err != nil {
		return nil, err
	}

	return credential, nil
}

func GetContainerCredential(containerName string) (*models.ContainerCredential, error) {
	var credential models.ContainerCredential
	if err := db.DB.Where("container_name = ?", containerName).First(&credential).Error; err != nil {
		return nil, fmt.Errorf("容器凭证不存在")
	}
	return &credential, nil
}

func GetContainerByHash(hash string) (*models.ContainerCredential, error) {
	var credential models.ContainerCredential
	if err := db.DB.Where("hash = ?", hash).First(&credential).Error; err != nil {
		return nil, fmt.Errorf("无效的容器Hash")
	}
	return &credential, nil
}

func DeleteContainerCredential(containerName string) error {
	result := db.DB.Unscoped().Where("container_name = ?", containerName).Delete(&models.ContainerCredential{})
	if result.Error != nil {
		return result.Error
	}
	return nil
}

func RegenerateContainerHash(containerName string) (*models.ContainerCredential, error) {
	var credential models.ContainerCredential
	if err := db.DB.Where("container_name = ?", containerName).First(&credential).Error; err != nil {
		return nil, fmt.Errorf("容器凭证不存在")
	}

	hash, err := generateContainerHash()
	if err != nil {
		return nil, fmt.Errorf("生成容器Hash失败: %v", err)
	}

	credential.Hash = hash
	if err := db.DB.Save(&credential).Error; err != nil {
		return nil, err
	}

	return &credential, nil
}

