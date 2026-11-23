package admin

import (
	"fmt"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/mojocn/base64Captcha"
	"lxdapi/internal/core"
	"lxdapi/pkg/response"
	"sync"
	"time"
)

var captchaStore = base64Captcha.DefaultMemStore

var loginAttempts = struct {
	sync.RWMutex
	attempts map[string]*LoginAttempt
}{
	attempts: make(map[string]*LoginAttempt),
}

type LoginAttempt struct {
	Count      int
	LastAttempt time.Time
	LockedUntil time.Time
}

const (
	maxAttempts           = 5
	lockDuration          = 15 * time.Minute
	resetDuration         = 5 * time.Minute
	ADMIN_MAX_RATE_LIMIT  = 10000
)

func is_admin_attempt_expired(attempt *LoginAttempt, now time.Time) bool {
	if !attempt.LockedUntil.IsZero() && now.Before(attempt.LockedUntil) {
		return false
	}
	return now.Sub(attempt.LastAttempt) > resetDuration
}

func evict_admin_attempts_locked(now time.Time) {
	if len(loginAttempts.attempts) < ADMIN_MAX_RATE_LIMIT {
		return
	}
	for k, v := range loginAttempts.attempts {
		if is_admin_attempt_expired(v, now) {
			delete(loginAttempts.attempts, k)
		}
	}
	if len(loginAttempts.attempts) < ADMIN_MAX_RATE_LIMIT {
		return
	}
	oldest_key := ""
	var oldest_time time.Time
	for k, v := range loginAttempts.attempts {
		if oldest_key == "" || v.LastAttempt.Before(oldest_time) {
			oldest_key = k
			oldest_time = v.LastAttempt
		}
	}
	if oldest_key != "" {
		delete(loginAttempts.attempts, oldest_key)
	}
}

func check_admin_locked(client_ip string, now time.Time) (bool, int) {
	loginAttempts.Lock()
	defer loginAttempts.Unlock()
	attempt, exists := loginAttempts.attempts[client_ip]
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
	if now.Sub(attempt.LastAttempt) > resetDuration {
		delete(loginAttempts.attempts, client_ip)
	}
	return false, 0
}

func record_admin_failure(client_ip string, now time.Time) (bool, int) {
	loginAttempts.Lock()
	defer loginAttempts.Unlock()
	attempt, exists := loginAttempts.attempts[client_ip]
	if !exists {
		evict_admin_attempts_locked(now)
		attempt = &LoginAttempt{}
		loginAttempts.attempts[client_ip] = attempt
	} else if !attempt.LockedUntil.IsZero() && !now.Before(attempt.LockedUntil) {
		attempt.Count = 0
		attempt.LockedUntil = time.Time{}
	}
	if now.Sub(attempt.LastAttempt) > resetDuration {
		attempt.Count = 0
	}
	attempt.Count++
	attempt.LastAttempt = now
	if attempt.Count >= maxAttempts {
		attempt.LockedUntil = now.Add(lockDuration)
		return true, int(lockDuration.Minutes())
	}
	return false, maxAttempts - attempt.Count
}

func reset_admin_attempts(client_ip string) {
	loginAttempts.Lock()
	defer loginAttempts.Unlock()
	delete(loginAttempts.attempts, client_ip)
}

// GetCaptcha 获取验证码
func GetCaptcha(c *gin.Context) {
	driver := base64Captcha.NewDriverDigit(80, 240, 4, 0.7, 80)
	captcha := base64Captcha.NewCaptcha(driver, captchaStore)
	id, b64s, _, err := captcha.Generate()
	if err != nil {
		response.Error(c, 500, "生成验证码失败")
		return
	}

	session := sessions.Default(c)
	session.Set("captcha_id", id)
	session.Save()

	c.JSON(200, gin.H{
		"code":       200,
		"captcha_id": id,
		"image":      b64s,
	})
}

// Login 管理员登录
func Login(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
		Captcha  string `json:"captcha" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}

	client_ip := c.ClientIP()
	now := time.Now()
	if locked, remaining := check_admin_locked(client_ip, now); locked {
		response.Error(c, 429, fmt.Sprintf("登录已被锁定，请在%d分钟后重试", remaining))
		return
	}

	session := sessions.Default(c)
	captchaID := session.Get("captcha_id")
	if captchaID == nil {
		response.Error(c, 400, "验证码已过期，请刷新")
		return
	}

	if !captchaStore.Verify(captchaID.(string), req.Captcha, true) {
		response.Error(c, 400, "验证码错误")
		return
	}

	session.Delete("captcha_id")
	session.Save()

	if req.Username != core.GlobalConfig.Admin.Username || req.Password != core.GlobalConfig.Admin.Password {
		locked, remaining_or_mins := record_admin_failure(client_ip, time.Now())
		if locked {
			response.Error(c, 429, fmt.Sprintf("登录失败次数过多，账户已被锁定%d分钟", remaining_or_mins))
			return
		}
		response.Error(c, 401, fmt.Sprintf("用户名或密码错误，剩余尝试次数: %d", remaining_or_mins))
		return
	}

	reset_admin_attempts(client_ip)

	session.Options(sessions.Options{
		MaxAge:   86400,
		HttpOnly: true,
		Secure:   core.GlobalConfig.System.Server.TLS.Enabled,
		SameSite: 3,
		Path:     "/",
	})
	
	session.Set("admin_logged_in", true)
	session.Set("admin_username", req.Username)
	session.Save()

	response.Success(c, gin.H{
		"username": req.Username,
		"redirect": "/admin/dashboard",
	})
}

// Logout 管理员登出
func Logout(c *gin.Context) {
	session := sessions.Default(c)
	session.Clear()
	session.Save()
	
	response.Success(c, gin.H{
		"redirect": "/admin/login",
	})
}

