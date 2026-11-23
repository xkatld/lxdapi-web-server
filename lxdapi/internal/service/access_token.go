package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"gorm.io/gorm"
	"lxdapi/internal/db"
	"lxdapi/models"
	"time"
)

func generateToken() string {
	bytes := make([]byte, 32)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func CreateAccessToken(tokenType, target string, duration time.Duration) (*models.AccessToken, error) {
	db.DB.Unscoped().Where("type = ? AND target = ?", tokenType, target).Delete(&models.AccessToken{})

	token := &models.AccessToken{
		Token:     generateToken(),
		Type:      tokenType,
		Target:    target,
		ExpiresAt: time.Now().Add(duration),
	}

	if err := db.DB.Create(token).Error; err != nil {
		return nil, err
	}

	return token, nil
}

func ValidateAccessToken(token string) (*models.AccessToken, error) {
	var accessToken models.AccessToken
	if err := db.DB.Where("token = ? AND expires_at > ?", token, time.Now()).First(&accessToken).Error; err != nil {
		return nil, err
	}

	return &accessToken, nil
}

func ConsumeAccessToken(token, expectedType string) (*models.AccessToken, error) {
	if token == "" {
		return nil, errors.New("令牌不能为空")
	}
	var accessToken models.AccessToken
	err := db.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("token = ? AND type = ? AND expires_at > ?", token, expectedType, time.Now()).First(&accessToken).Error; err != nil {
			return err
		}
		res := tx.Unscoped().Where("id = ?", accessToken.ID).Delete(&models.AccessToken{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return errors.New("令牌已被消费")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &accessToken, nil
}

func CleanExpiredTokens() {
	db.DB.Unscoped().Where("expires_at < ?", time.Now()).Delete(&models.AccessToken{})
}
