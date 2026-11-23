package user

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"lxdapi/internal/db"
	"lxdapi/models"
	"lxdapi/pkg/logger"
	"lxdapi/pkg/response"
)

func GetTemplateList(c *gin.Context) {
	username, _ := c.Get("username")
	
	var templates []models.Template
	if err := db.DB.Find(&templates).Error; err != nil {
		logger.Error("获取模板列表失败: %v", err)
		response.Error(c, 500, "获取模板列表失败: "+err.Error())
		return
	}
	
	var result []map[string]interface{}
	for _, t := range templates {
		if t.AllowedUsers != "" && t.AllowedUsers != "[]" {
			var allowedUsers []string
			if err := json.Unmarshal([]byte(t.AllowedUsers), &allowedUsers); err == nil && len(allowedUsers) > 0 {
				allowed := false
				for _, u := range allowedUsers {
					if u == username.(string) {
						allowed = true
						break
					}
				}
				if !allowed {
					continue
				}
			}
		}
		
		result = append(result, map[string]interface{}{
			"fingerprint":  t.Fingerprint,
			"alias":        t.Alias,
			"architecture": t.Architecture,
			"description":  t.Description,
			"os":           t.OS,
			"release":      t.Release,
			"size":         t.Size,
			"public":       t.Public,
			"auto_update":  t.AutoUpdate,
			"uploaded_at":  t.UploadedAt,
		})
	}
	
	response.Success(c, result)
}

