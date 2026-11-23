package middleware

import (
	"crypto/subtle"
	"fmt"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"lxdapi/internal/core"
	"lxdapi/internal/service"
	"lxdapi/pkg/auth"
	"lxdapi/pkg/response"
	"net/http"
	"sync"
	"time"
)

type authAttempt struct {
	count       int
	lastAttempt time.Time
	lockedUntil time.Time
}

type authRateLimiter struct {
	mu       sync.Mutex
	attempts map[string]*authAttempt
}

func newAuthRateLimiter() *authRateLimiter {
	return &authRateLimiter{
		attempts: make(map[string]*authAttempt),
	}
}

const (
	authMaxAttempts   = 10
	authLockDuration  = 15 * time.Minute
	authResetDuration = 5 * time.Minute
	AUTH_MAX_ENTRIES  = 10000
)

func (l *authRateLimiter) isEntryExpired(attempt *authAttempt, now time.Time) bool {
	if !attempt.lockedUntil.IsZero() && now.Before(attempt.lockedUntil) {
		return false
	}
	return now.Sub(attempt.lastAttempt) > authResetDuration
}

func (l *authRateLimiter) evictIfNeeded(now time.Time) {
	if len(l.attempts) < AUTH_MAX_ENTRIES {
		return
	}
	for k, v := range l.attempts {
		if l.isEntryExpired(v, now) {
			delete(l.attempts, k)
		}
	}
	if len(l.attempts) < AUTH_MAX_ENTRIES {
		return
	}
	oldest_key := ""
	var oldest_time time.Time
	for k, v := range l.attempts {
		if oldest_key == "" || v.lastAttempt.Before(oldest_time) {
			oldest_key = k
			oldest_time = v.lastAttempt
		}
	}
	if oldest_key != "" {
		delete(l.attempts, oldest_key)
	}
}

func (l *authRateLimiter) isLocked(key string) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	attempt, exists := l.attempts[key]
	if !exists {
		return false, 0
	}
	now := time.Now()
	if !attempt.lockedUntil.IsZero() && now.Before(attempt.lockedUntil) {
		remaining := int(attempt.lockedUntil.Sub(now).Minutes()) + 1
		return true, remaining
	}
	if !attempt.lockedUntil.IsZero() && !now.Before(attempt.lockedUntil) {
		attempt.count = 0
		attempt.lockedUntil = time.Time{}
	}
	if now.Sub(attempt.lastAttempt) > authResetDuration {
		delete(l.attempts, key)
		return false, 0
	}
	return false, 0
}

func (l *authRateLimiter) recordFailure(key string) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	attempt, exists := l.attempts[key]
	if !exists {
		l.evictIfNeeded(now)
		attempt = &authAttempt{}
		l.attempts[key] = attempt
	} else if !attempt.lockedUntil.IsZero() && !now.Before(attempt.lockedUntil) {
		attempt.count = 0
		attempt.lockedUntil = time.Time{}
	}
	if now.Sub(attempt.lastAttempt) > authResetDuration {
		attempt.count = 0
	}
	attempt.count++
	attempt.lastAttempt = now
	if attempt.count >= authMaxAttempts {
		attempt.lockedUntil = now.Add(authLockDuration)
		return true, int(authLockDuration.Minutes())
	}
	return false, authMaxAttempts - attempt.count
}

func (l *authRateLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

var (
	systemAuthLimiter    = newAuthRateLimiter()
	userBasicAuthLimiter = newAuthRateLimiter()
	containerAuthLimiter = newAuthRateLimiter()
)

func secureSessionOptions(maxAge int) sessions.Options {
	isSecure := false
	if core.GlobalConfig != nil {
		isSecure = core.GlobalConfig.System.Server.TLS.Enabled
	}
	return sessions.Options{
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: http.SameSiteLaxMode,
	}
}

func SystemAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		clientIP := c.ClientIP()
		if locked, remaining := systemAuthLimiter.isLocked(clientIP); locked {
			response.Error(c, 429, fmt.Sprintf("认证失败次数过多，请在%d分钟后重试", remaining))
			c.Abort()
			return
		}

		expectedHash := ""
		if core.GlobalConfig != nil {
			expectedHash = core.GlobalConfig.System.Security.APIHash
		}
		apiHash := c.GetHeader("X-API-Hash")
		if expectedHash == "" || apiHash == "" || subtle.ConstantTimeCompare([]byte(apiHash), []byte(expectedHash)) != 1 {
			if locked, lockMins := systemAuthLimiter.recordFailure(clientIP); locked {
				response.Error(c, 429, fmt.Sprintf("认证失败次数过多，已被锁定%d分钟", lockMins))
			} else {
				response.Error(c, 401, "系统级认证失败")
			}
			c.Abort()
			return
		}
		systemAuthLimiter.reset(clientIP)
		c.Set("auth_level", auth.LevelSystem)
		c.Next()
	}
}

func AdminAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		loggedIn := session.Get("admin_logged_in")
		
		if loggedIn == nil || loggedIn != true {
			response.Error(c, 401, "未登录或登录已过期，请重新登录")
			c.Abort()
			return
		}
		
		username := session.Get("admin_username")
		c.Set("auth_level", auth.LevelAdmin)
		c.Set("admin_username", username)
		c.Next()
	}
}

func AdminPageAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if token := c.Query("token"); token != "" {
			accessToken, err := service.ConsumeAccessToken(token, "admin")
			if err != nil {
				c.Redirect(302, "/admin/login")
				c.Abort()
				return
			}
			session := sessions.Default(c)
			session.Options(secureSessionOptions(86400))
			session.Set("admin_logged_in", true)
			session.Set("admin_username", accessToken.Target)
			if err := session.Save(); err != nil {
				c.Redirect(302, "/admin/login")
				c.Abort()
				return
			}
			c.Redirect(302, c.Request.URL.Path)
			c.Abort()
			return
		}
		
		session := sessions.Default(c)
		loggedIn := session.Get("admin_logged_in")
		
		if loggedIn == nil || loggedIn != true {
			c.Redirect(302, "/admin/login")
			c.Abort()
			return
		}
		
		c.Next()
	}
}

func UserAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		loggedIn := session.Get("user_logged_in")
		
		if loggedIn == nil || loggedIn != true {
			response.Error(c, 401, "未登录或登录已过期，请重新登录")
			c.Abort()
			return
		}
		
		username := session.Get("user_username")
		if username == nil {
			response.Error(c, 401, "会话无效，请重新登录")
			c.Abort()
			return
		}
		
		user, err := service.GetUserByUsername(username.(string))
		if err != nil || user.Status == "disabled" {
			session.Clear()
			session.Save()
			response.Error(c, 403, "账户已被禁用")
			c.Abort()
			return
		}
		
		c.Set("auth_level", auth.LevelUser)
		c.Set("username", username.(string))
		c.Next()
	}
}

func UserBasicAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		clientIP := c.ClientIP()
		if locked, remaining := userBasicAuthLimiter.isLocked(clientIP); locked {
			response.Error(c, 429, fmt.Sprintf("认证失败次数过多，请在%d分钟后重试", remaining))
			c.Abort()
			return
		}

		username := c.GetHeader("X-User-Username")
		password := c.GetHeader("X-User-Password")
		
		if username == "" || password == "" {
			response.Error(c, 401, "缺少用户认证信息")
			c.Abort()
			return
		}
		
		user, err := service.GetUserByUsernameAndAPIKey(username, password)
		if err != nil {
			if err.Error() == "账户已被禁用，请联系管理员" {
				response.Error(c, 403, "账户已被禁用，请联系管理员")
			} else if locked, lockMins := userBasicAuthLimiter.recordFailure(clientIP); locked {
				response.Error(c, 429, fmt.Sprintf("认证失败次数过多，已被锁定%d分钟", lockMins))
			} else {
				response.Error(c, 401, "用户认证失败")
			}
			c.Abort()
			return
		}
		
		userBasicAuthLimiter.reset(clientIP)
		c.Set("auth_level", auth.LevelUser)
		c.Set("username", user.Username)
		c.Next()
	}
}

func UserAuthOrBasic() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader("X-User-Username") != "" {
			UserBasicAuth()(c)
		} else {
			UserAuth()(c)
		}
	}
}

func UserPageAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if token := c.Query("token"); token != "" {
			accessToken, err := service.ConsumeAccessToken(token, "user")
			if err != nil {
				c.Redirect(302, "/user/login")
				c.Abort()
				return
			}
			user, err := service.GetUserByUsername(accessToken.Target)
			if err != nil || user.Status == "disabled" {
				c.Redirect(302, "/user/login?error=disabled")
				c.Abort()
				return
			}
			session := sessions.Default(c)
			session.Options(secureSessionOptions(86400))
			session.Set("user_logged_in", true)
			session.Set("user_username", user.Username)
			session.Set("user_id", user.ID)
			if err := session.Save(); err != nil {
				c.Redirect(302, "/user/login")
				c.Abort()
				return
			}
			c.Redirect(302, "/user/containers")
			c.Abort()
			return
		}
		
		session := sessions.Default(c)
		loggedIn := session.Get("user_logged_in")
		
		if loggedIn == nil || loggedIn != true {
			c.Redirect(302, "/user/login")
			c.Abort()
			return
		}
		
		username := session.Get("user_username")
		if username != nil {
			user, err := service.GetUserByUsername(username.(string))
			if err != nil || user.Status == "disabled" {
				session.Clear()
				session.Save()
				c.Redirect(302, "/user/login?error=disabled")
				c.Abort()
				return
			}
		}
		
		c.Next()
	}
}

func ContainerAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		clientIP := c.ClientIP()
		if locked, remaining := containerAuthLimiter.isLocked(clientIP); locked {
			response.Error(c, 429, fmt.Sprintf("认证失败次数过多，请在%d分钟后重试", remaining))
			c.Abort()
			return
		}

		containerHash := c.GetHeader("X-Container-Hash")
		if containerHash == "" {
			response.Error(c, 401, "缺少容器凭证")
			c.Abort()
			return
		}
		
		cred, err := service.GetContainerByHash(containerHash)
		if err != nil {
			if locked, lockMins := containerAuthLimiter.recordFailure(clientIP); locked {
				response.Error(c, 429, fmt.Sprintf("认证失败次数过多，已被锁定%d分钟", lockMins))
			} else {
				response.Error(c, 401, "容器级认证失败")
			}
			c.Abort()
			return
		}
		
		containerAuthLimiter.reset(clientIP)
		c.Set("auth_level", auth.LevelContainer)
		c.Set("container_name", cred.ContainerName)
		c.Next()
	}
}

