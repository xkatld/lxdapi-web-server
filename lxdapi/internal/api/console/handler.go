package console

import (
	"encoding/json"
	"lxdapi/internal/console"
	"lxdapi/internal/service"
	"lxdapi/pkg/auth"
	"lxdapi/pkg/logger"
	"lxdapi/pkg/response"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  8192,
	WriteBufferSize: 8192,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// CreateToken 创建WebSocket令牌
// @Summary 创建WebSocket令牌
// @Description 为容器控制台创建WebSocket访问令牌
// @Tags Console API
// @Accept json
// @Produce json
// @Param request body object{hostname=string} true "容器主机名"
// @Success 200 {object} response.Response "创建成功"
// @Failure 400 {object} response.Response "参数错误"
// @Failure 500 {object} response.Response "生成失败"
// @Router /api/console/token [post]
func CreateToken(c *gin.Context) {
	var req struct {
		Hostname string `json:"hostname" binding:"required"`
	}
	
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 400, "参数错误")
		return
	}

	if auth_level_val, exists := c.Get("auth_level"); exists {
		if auth_level, ok := auth_level_val.(auth.APILevel); ok {
			if auth_level == auth.LevelUser || auth_level == auth.LevelContainer {
				if deny_code, deny_msg := service.CheckContainerOperationByName(req.Hostname, auth_level == auth.LevelUser, "console"); deny_code != 0 {
					response.Error(c, deny_code, deny_msg)
					return
				}
			}
		}
	}

	token, err := console.GenerateToken(req.Hostname)
	if err != nil {
		response.Error(c, 500, "生成令牌失败")
		return
	}
	
	response.Success(c, gin.H{
		"token": token,
	})
}

// HandleWebSocket WebSocket控制台
// @Summary WebSocket控制台连接
// @Description 建立WebSocket连接用于容器控制台
// @Tags Console API
// @Accept json
// @Produce json
// @Param token query string true "访问令牌"
// @Router /api/console/ws [get]
func HandleWebSocket(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 400,
			"msg":  "缺少令牌参数",
		})
		return
	}
	
	containerName, valid := console.ValidateAndConsume(token)
	if !valid {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code": 401,
			"msg":  "令牌无效、已使用或已过期",
		})
		return
	}
	
	logger.Info("WebSocket控制台连接请求: %s", containerName)
	
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.Error("WebSocket升级失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": 500,
			"msg":  "WebSocket升级失败: " + err.Error(),
		})
		return
	}
	defer conn.Close()
	
	session, err := console.CreateSession(containerName, conn)
	if err != nil {
		logger.Error("创建控制台会话失败: %v", err)
		conn.WriteMessage(websocket.TextMessage, []byte("错误: "+err.Error()+"\r\n"))
		return
	}
	defer session.Close()
	
	logger.Info("控制台会话建立成功: %s", session.ID)
	
	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logger.Error("WebSocket连接异常关闭: %v", err)
			} else {
				logger.Info("WebSocket连接正常关闭")
			}
			break
		}
		
		if messageType == websocket.TextMessage {
			var msg struct {
				Type string `json:"type"`
				Data string `json:"data"`
			}
			
			if err := json.Unmarshal(message, &msg); err != nil {
				if err := session.WriteInput(message); err != nil {
					logger.Error("写入控制台输入失败: %v", err)
					conn.WriteMessage(websocket.TextMessage, []byte("错误: 写入失败\r\n"))
					break
				}
			} else if msg.Type == "input" {
				if err := session.WriteInput([]byte(msg.Data)); err != nil {
					logger.Error("写入控制台输入失败: %v", err)
					conn.WriteMessage(websocket.TextMessage, []byte("错误: 写入失败\r\n"))
					break
				}
			}
		}
	}
	
	logger.Info("控制台会话结束: %s", session.ID)
}
