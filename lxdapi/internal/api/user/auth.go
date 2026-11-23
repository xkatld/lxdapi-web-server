package user

import (
	"fmt"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/mojocn/base64Captcha"
	"lxdapi/internal/core"
	"lxdapi/internal/service"
	"lxdapi/pkg/logger"
	"lxdapi/pkg/response"
	"net/http"
	"sync"
	"time"
)

var captchaStore = base64Captcha.DefaultMemStore

var userLoginAttempts = struct {
	sync.RWMutex
	attempts map[string]*UserLoginAttempt
}{
	attempts: make(map[string]*UserLoginAttempt),
}

type UserLoginAttempt struct {
	Count      int
	LastAttempt time.Time
	LockedUntil time.Time
}

const (
	userMaxAttempts       = 5
	userLockDuration      = 15 * time.Minute
	userResetDuration     = 5 * time.Minute
	USER_MAX_RATE_LIMIT   = 10000
)

func is_user_attempt_expired(attempt *UserLoginAttempt, now time.Time) bool {
	if !attempt.LockedUntil.IsZero() && now.Before(attempt.LockedUntil) {
		return false
	}
	return now.Sub(attempt.LastAttempt) > userResetDuration
}

func evict_user_attempts_locked(now time.Time) {
	if len(userLoginAttempts.attempts) < USER_MAX_RATE_LIMIT {
		return
	}
	for k, v := range userLoginAttempts.attempts {
		if is_user_attempt_expired(v, now) {
			delete(userLoginAttempts.attempts, k)
		}
	}
	if len(userLoginAttempts.attempts) < USER_MAX_RATE_LIMIT {
		return
	}
	oldest_key := ""
	var oldest_time time.Time
	for k, v := range userLoginAttempts.attempts {
		if oldest_key == "" || v.LastAttempt.Before(oldest_time) {
			oldest_key = k
			oldest_time = v.LastAttempt
		}
	}
	if oldest_key != "" {
		delete(userLoginAttempts.attempts, oldest_key)
	}
}

func check_user_locked(client_ip string, now time.Time) (bool, int) {
	userLoginAttempts.Lock()
	defer userLoginAttempts.Unlock()
	attempt, exists := userLoginAttempts.attempts[client_ip]
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
	if now.Sub(attempt.LastAttempt) > userResetDuration {
		delete(userLoginAttempts.attempts, client_ip)
	}
	return false, 0
}

func record_user_failure(client_ip string, now time.Time) (bool, int) {
	userLoginAttempts.Lock()
	defer userLoginAttempts.Unlock()
	attempt, exists := userLoginAttempts.attempts[client_ip]
	if !exists {
		evict_user_attempts_locked(now)
		attempt = &UserLoginAttempt{}
		userLoginAttempts.attempts[client_ip] = attempt
	} else if !attempt.LockedUntil.IsZero() && !now.Before(attempt.LockedUntil) {
		attempt.Count = 0
		attempt.LockedUntil = time.Time{}
	}
	if now.Sub(attempt.LastAttempt) > userResetDuration {
		attempt.Count = 0
	}
	attempt.Count++
	attempt.LastAttempt = now
	if attempt.Count >= userMaxAttempts {
		attempt.LockedUntil = now.Add(userLockDuration)
		return true, int(userLockDuration.Minutes())
	}
	return false, userMaxAttempts - attempt.Count
}

func reset_user_attempts(client_ip string) {
	userLoginAttempts.Lock()
	defer userLoginAttempts.Unlock()
	delete(userLoginAttempts.attempts, client_ip)
}

// GetCaptcha 获取验证码
// @Summary 获取验证码
// @Description 获取用户登录验证码
// @Tags User API - 认证
// @Accept json
// @Produce json
// @Success 200 {object} object{code=int,captcha_id=string,image=string} "生成成功"
// @Failure 500 {object} response.Response "生成失败"
// @Router /api/user/captcha [get]
func GetCaptcha(c *gin.Context) {
	driver := base64Captcha.NewDriverDigit(80, 240, 4, 0.7, 80)
	captcha := base64Captcha.NewCaptcha(driver, captchaStore)
	id, b64s, _, err := captcha.Generate()
	if err != nil {
		response.Error(c, 500, "生成验证码失败")
		return
	}

	session := sessions.Default(c)
	session.Set("user_captcha_id", id)
	session.Save()

	c.JSON(200, gin.H{
		"code":       200,
		"captcha_id": id,
		"image":      b64s,
	})
}

// Login 用户登录
// @Summary 用户登录
// @Description 使用用户名、密码和验证码登录
// @Tags User API - 认证
// @Accept json
// @Produce json
// @Param request body object{username=string,password=string,captcha=string} true "登录信息"
// @Success 200 {object} response.Response "登录成功"
// @Failure 400 {object} response.Response "参数错误"
// @Failure 401 {object} response.Response "认证失败"
// @Failure 500 {object} response.Response "登录失败"
// @Router /api/user/login [post]
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
	if locked, remaining := check_user_locked(client_ip, now); locked {
		response.Error(c, 429, fmt.Sprintf("登录已被锁定，请在%d分钟后重试", remaining))
		return
	}

	session := sessions.Default(c)
	captchaID := session.Get("user_captcha_id")
	if captchaID == nil {
		response.Error(c, 400, "验证码已过期，请刷新")
		return
	}

	if !captchaStore.Verify(captchaID.(string), req.Captcha, true) {
		response.Error(c, 400, "验证码错误")
		return
	}

	session.Delete("user_captcha_id")

	user, err := service.GetUserByUsernameAndAPIKey(req.Username, req.Password)
	if err != nil {
		err_msg := err.Error()
		if err_msg == "账户已被禁用，请联系管理员" {
			response.Error(c, 403, err_msg)
			return
		}
		
		locked, remaining_or_mins := record_user_failure(client_ip, time.Now())
		if locked {
			logger.Warn("用户登录失败次数过多: %s, IP: %s", req.Username, client_ip)
			response.Error(c, 429, fmt.Sprintf("登录失败次数过多，账户已被锁定%d分钟", remaining_or_mins))
			return
		}
		
		logger.Warn("用户登录失败: %s", req.Username)
		response.Error(c, 401, fmt.Sprintf("用户名或密码无效，剩余尝试次数: %d", remaining_or_mins))
		return
	}

	reset_user_attempts(client_ip)

	session.Options(sessions.Options{
		MaxAge:   86400,
		HttpOnly: true,
		Secure:   core.GlobalConfig.System.Server.TLS.Enabled,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	})
	session.Set("user_logged_in", true)
	session.Set("user_username", user.Username)
	session.Set("user_id", user.ID)
	if err := session.Save(); err != nil {
		logger.Error("保存Session失败: %v", err)
		response.Error(c, 500, "登录失败")
		return
	}

	logger.OK("用户登录成功: %s", user.Username)
	response.Success(c, gin.H{
		"username": user.Username,
		"user_id":  user.ID,
	})
}

// Logout 用户登出
// @Summary 用户登出
// @Description 退出用户中心登录
// @Tags User API - 认证
// @Accept json
// @Produce json
// @Success 200 {object} response.Response "登出成功"
// @Security UserSession
// @Router /api/user/logout [post]
func Logout(c *gin.Context) {
	session := sessions.Default(c)
	session.Clear()
	session.Save()
	response.Success(c, nil)
}

// GetUserInfo 获取当前用户信息和配额
// @Summary 获取当前用户信息
// @Description 获取当前登录用户的信息和配额统计
// @Tags User API - 认证
// @Accept json
// @Produce json
// @Success 200 {object} response.Response "获取成功"
// @Security UserSession
// @Router /api/user/info [get]
func GetUserInfo(c *gin.Context) {
	username, _ := c.Get("username")
	
	user, err := service.GetUserByUsername(username.(string))
	if err != nil {
		response.Error(c, 404, "用户不存在")
		return
	}
	
	stats := service.GetUserFullStats(username.(string))
	
	response.Success(c, gin.H{
		"user": gin.H{
			"id":                    user.ID,
			"username":              user.Username,
			"status":                user.Status,
			"cpu_quota":             user.CPUQuota,
			"max_cpu_per_container": user.MaxCPUPerContainer,
			"memory_quota":          user.MemoryQuota,
			"disk_quota":            user.DiskQuota,
			"traffic_limit":         user.TrafficLimit,
			"traffic_used":          user.TrafficUsed,
			"traffic_locked":        user.TrafficLocked,
			"ipv4_pool_limit":       user.IPv4PoolLimit,
			"ipv4_mapping_limit":    user.IPv4MappingLimit,
			"ipv6_pool_limit":       user.IPv6PoolLimit,
			"ipv6_mapping_limit":    user.IPv6MappingLimit,
			"reverse_proxy_limit":   user.ReverseProxyLimit,
		},
		"stats": stats,
	})
}

