package handler

import (
	"crypto-final/internal/model"
	"crypto-final/internal/service"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *service.Service
}

func NewHandler(svc *service.Service) *Handler {
	return &Handler{service: svc}
}

// AuthMiddleware 验证用户登录
func (h *Handler) AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		phone, password, ok := c.Request.BasicAuth()
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "需要认证"})
			c.Abort()
			return
		}

		user, err := h.service.Login(phone, password)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "认证失败: " + err.Error()})
			c.Abort()
			return
		}

		c.Set("user", user)
		c.Next()
	}
}

// AdminMiddleware 验证管理员权限
func (h *Handler) AdminMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		user, exists := c.Get("user")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "需要认证"})
			c.Abort()
			return
		}

		u := user.(*model.User)
		if !u.IsAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "需要管理员权限"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// Login 登录
func (h *Handler) Login(c *gin.Context) {
	var req model.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误"})
		return
	}

	user, err := h.service.Login(req.Phone, req.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "登录成功",
		"user": gin.H{
			"id":       user.ID,
			"phone":    user.Phone,
			"is_admin": user.IsAdmin,
		},
	})
}

// Admin路由

// AdminCreateUser 管理员创建用户
func (h *Handler) AdminCreateUser(c *gin.Context) {
	var req model.AdminCreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误"})
		return
	}

	userID, err := h.service.AdminCreateUser(req.Phone)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "用户创建成功，密码为：abc123456",
		"user_id": userID,
	})
}

// AdminGetUsers 获取所有Dashboard用户
func (h *Handler) AdminGetUsers(c *gin.Context) {
	users, err := h.service.GetAllDashboardUsersWithStats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"users": users})
}

// AdminRecharge 管理员充值
func (h *Handler) AdminRecharge(c *gin.Context) {
	var req model.AdminRechargeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误"})
		return
	}

	err := h.service.AdminRecharge(req.UserID, req.AdminAccountID, req.Amount, req.Currency)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "充值成功"})
}

// AdminGetAccountsStatus 获取Admin账户状态
func (h *Handler) AdminGetAccountsStatus(c *gin.Context) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("❌ AdminGetAccountsStatus panic: %v\n", r)
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("服务器错误: %v", r)})
		}
	}()

	statuses, err := h.service.GetAdminAccountsStatus()
	if err != nil {
		fmt.Printf("❌ GetAdminAccountsStatus error: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"accounts": statuses})
}

// AdminConfigAccount 配置Admin账户
func (h *Handler) AdminConfigAccount(c *gin.Context) {
	var req model.AdminAccountConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误"})
		return
	}

	err := h.service.ConfigAdminAccount(req.AccountType, req.APIKey, req.APISecret, req.WalletAddress)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "配置保存成功"})
}

// AdminManualCheck 手动触发余额检查
func (h *Handler) AdminManualCheck(c *gin.Context) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("❌ AdminManualCheck panic: %v\n", r)
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("余额检查失败: %v", r)})
		}
	}()

	fmt.Println("📊 手动触发余额检查")

	if err := h.service.UpdateDailyBalances(); err != nil {
		fmt.Printf("❌ 余额检查失败: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	fmt.Println("✓ 余额检查完成")
	c.JSON(http.StatusOK, gin.H{"message": "余额检查完成"})
}

// Dashboard路由

// GetDashboardSummary 获取Dashboard总览
func (h *Handler) GetDashboardSummary(c *gin.Context) {
	user := c.MustGet("user").(*model.User)

	summary := h.service.GetDashboardSummary(user.ID)
	c.JSON(http.StatusOK, summary)
}

// GetDashboardRecharges 获取充值列表
func (h *Handler) GetDashboardRecharges(c *gin.Context) {
	user := c.MustGet("user").(*model.User)

	recharges, err := h.service.GetUserRechargesWithProfit(user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"recharges": recharges})
}

// GetRechargeHistory 获取单笔充值历史
func (h *Handler) GetRechargeHistory(c *gin.Context) {
	user := c.MustGet("user").(*model.User)

	rechargeID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "充值ID无效"})
		return
	}

	history, err := h.service.GetRechargeProfitHistory(rechargeID, user.ID)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"history": history})
}

// DashboardManualRefresh Dashboard用户手动刷新盈亏
func (h *Handler) DashboardManualRefresh(c *gin.Context) {
	// 触发余额更新
	if err := h.service.UpdateDailyBalances(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "刷新完成"})
}
