package container

import (
	"fmt"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/mojocn/base64Captcha"
	"lxdapi/internal/core"
	"lxdapi/internal/service"
	"lxdapi/pkg/response"
	"net/http"
	"sync"
	"time"
)

var captchaStore = base64Captcha.DefaultMemStore

var containerVerifyAttempts = struct {
	sync.RWMutex
	attempts map[string]*ContainerVerifyAttempt
}{
	attempts: make(map[string]*ContainerVerifyAttempt),
}

type ContainerVerifyAttempt struct {
	Count      int
	LastAttempt time.Time
	LockedUntil time.Time
}

const (
	containerMaxAttempts      = 5
	containerLockDuration     = 15 * time.Minute
	containerResetDuration    = 5 * time.Minute
	CONTAINER_MAX_RATE_LIMIT  = 10000
)

func is_container_attempt_expired(attempt *ContainerVerifyAttempt, now time.Time) bool {
	if !attempt.LockedUntil.IsZero() && now.Before(attempt.LockedUntil) {
		return false
	}
	return now.Sub(attempt.LastAttempt) > containerResetDuration
}

func evict_container_attempts_locked(now time.Time) {
	if len(containerVerifyAttempts.attempts) < CONTAINER_MAX_RATE_LIMIT {
		return
	}
	for k, v := range containerVerifyAttempts.attempts {
		if is_container_attempt_expired(v, now) {
			delete(containerVerifyAttempts.attempts, k)
		}
	}
	if len(containerVerifyAttempts.attempts) < CONTAINER_MAX_RATE_LIMIT {
		return
	}
	oldest_key := ""
	var oldest_time time.Time
	for k, v := range containerVerifyAttempts.attempts {
		if oldest_key == "" || v.LastAttempt.Before(oldest_time) {
			oldest_key = k
			oldest_time = v.LastAttempt
		}
	}
	if oldest_key != "" {
		delete(containerVerifyAttempts.attempts, oldest_key)
	}
}

func check_container_locked(client_ip string, now time.Time) (bool, int) {
	containerVerifyAttempts.Lock()
	defer containerVerifyAttempts.Unlock()
	attempt, exists := containerVerifyAttempts.attempts[client_ip]
	if !exists {
		return false, 0
	}
	if !attempt.LockedUntil.IsZero() && now.Before(attempt.LockedUntil) {
		remaining := int(attempt.LockedUntil.Sub(now).Minutes()) + 1
		return true, remaining
	}
	if !attempt.LockedUntil.IsZero() && !now.Before(attempt.LockedUntil) {
		attempt.Count = 0
		attempt.LockedUntil = time.Time{}
	}
	if now.Sub(attempt.LastAttempt) > containerResetDuration {
		delete(containerVerifyAttempts.attempts, client_ip)
	}
	return false, 0
}

func record_container_failure(client_ip string, now time.Time) (bool, int) {
	containerVerifyAttempts.Lock()
	defer containerVerifyAttempts.Unlock()
	attempt, exists := containerVerifyAttempts.attempts[client_ip]
	if !exists {
		evict_container_attempts_locked(now)
		attempt = &ContainerVerifyAttempt{}
		containerVerifyAttempts.attempts[client_ip] = attempt
	} else if !attempt.LockedUntil.IsZero() && !now.Before(attempt.LockedUntil) {
		attempt.Count = 0
		attempt.LockedUntil = time.Time{}
	}
	if now.Sub(attempt.LastAttempt) > containerResetDuration {
		attempt.Count = 0
	}
	attempt.Count++
	attempt.LastAttempt = now
	if attempt.Count >= containerMaxAttempts {
		attempt.LockedUntil = now.Add(containerLockDuration)
		return true, int(containerLockDuration.Minutes())
	}
	return false, containerMaxAttempts - attempt.Count
}

func reset_container_attempts(client_ip string) {
	containerVerifyAttempts.Lock()
	defer containerVerifyAttempts.Unlock()
	delete(containerVerifyAttempts.attempts, client_ip)
}

// GetCaptcha 获取验证码
// @Summary 获取验证码
// @Description 获取容器登录验证码
// @Tags Container API - 认证
// @Accept json
// @Produce json
// @Success 200 {object} object{code=int,captcha_id=string,image=string} "生成成功"
// @Failure 500 {object} response.Response "生成失败"
// @Router /api/container/captcha [get]
func GetCaptcha(c *gin.Context) {
	driver := base64Captcha.NewDriverDigit(80, 240, 4, 0.7, 80)
	captcha := base64Captcha.NewCaptcha(driver, captchaStore)
	id, b64s, _, err := captcha.Generate()
	if err != nil {
		response.Error(c, 500, "生成验证码失败")
		return
	}

	session := sessions.Default(c)
	session.Options(sessions.Options{
		MaxAge:   3600,
		HttpOnly: true,
		Secure:   core.GlobalConfig.System.Server.TLS.Enabled,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	})
	session.Set("container_captcha_id", id)
	session.Save()

	c.JSON(200, gin.H{
		"code":       200,
		"captcha_id": id,
		"image":      b64s,
	})
}

// VerifyAccess 验证访问
// @Summary 验证访问
// @Description 使用Hash和验证码验证容器访问
// @Tags Container API - 认证
// @Accept json
// @Produce json
// @Param request body object{hash=string,captcha=string} true "验证信息"
// @Success 200 {object} response.Response "验证成功"
// @Failure 400 {object} response.Response "参数错误"
// @Failure 401 {object} response.Response "验证失败"
// @Router /api/container/verify [post]
func VerifyAccess(c *gin.Context) {
	var req struct {
		Hash    string `json:"hash" binding:"required"`
		Captcha string `json:"captcha" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}

	client_ip := c.ClientIP()
	now := time.Now()
	if locked, remaining := check_container_locked(client_ip, now); locked {
		response.Error(c, 429, fmt.Sprintf("验证已被锁定，请在%d分钟后重试", remaining))
		return
	}

	session := sessions.Default(c)
	captchaID := session.Get("container_captcha_id")
	if captchaID == nil {
		response.Error(c, 400, "验证码已过期，请刷新")
		return
	}

	if !captchaStore.Verify(captchaID.(string), req.Captcha, true) {
		response.Error(c, 400, "验证码错误")
		return
	}

	session.Delete("container_captcha_id")

	cred, err := service.GetContainerByHash(req.Hash)
	if err != nil {
		locked, remaining_or_mins := record_container_failure(client_ip, time.Now())
		if locked {
			response.Error(c, 429, fmt.Sprintf("验证失败次数过多，已被锁定%d分钟", remaining_or_mins))
			return
		}
		response.Error(c, 401, fmt.Sprintf("访问码无效，剩余尝试次数: %d", remaining_or_mins))
		return
	}

	reset_container_attempts(client_ip)

	response.Success(c, gin.H{
		"container_name": cred.ContainerName,
		"message":        "验证成功",
	})
}

