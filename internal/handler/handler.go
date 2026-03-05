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

// Login 用户登录（兼容多种字段名）
func (h *Handler) Login(c *gin.Context) {
	var req struct {
		Username string `json:"username"` // 新字段
		Phone    string `json:"phone"`    // 旧字段
		Password string `json:"password" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误: " + err.Error()})
		return
	}

	// 🔥 兼容处理：username或phone都可以
	loginIdentifier := req.Username
	if loginIdentifier == "" {
		loginIdentifier = req.Phone
	}

	if loginIdentifier == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供用户名或手机号"})
		return
	}

	if req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供密码"})
		return
	}

	// 先尝试用手机号查询
	user, err := h.service.Login(loginIdentifier, req.Password)

	// 如果失败，尝试用用户名查询
	if err != nil || user == nil {
		user, err = h.service.LoginByUsername(loginIdentifier, req.Password)
	}

	if err != nil || user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}

	// 返回用户信息
	displayName := user.Phone
	if user.IsAPIUser && user.Username != "" {
		displayName = user.Username
	}
	if displayName == "" {
		displayName = loginIdentifier
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "登录成功",
		"user": gin.H{
			"id":        user.ID,
			"username":  displayName,
			"is_admin":  user.IsAdmin,
			"is_active": user.IsActive,
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

// AdminGetUserDetail 获取用户详情
func (h *Handler) AdminGetUserDetail(c *gin.Context) {
	userID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "用户ID无效"})
		return
	}

	detail, err := h.service.GetUserDetail(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, detail)
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

// AdminToggleUserStatus 启用/停用用户
func (h *Handler) AdminToggleUserStatus(c *gin.Context) {
	userID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "用户ID无效"})
		return
	}

	// 获取当前用户状态
	user, err := h.service.GetUserByID(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if user == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}

	// 切换状态
	newStatus := !user.IsActive
	if err := h.service.UpdateUserStatus(userID, newStatus); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	statusText := "已停用"
	if newStatus {
		statusText = "已启用"
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "用户状态已更新",
		"is_active": newStatus,
		"status":    statusText,
	})
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
	var req struct {
		AccountType   string `json:"account_type" binding:"required"`
		APIKey        string `json:"api_key,omitempty"`
		APISecret     string `json:"api_secret,omitempty"`
		WalletAddress string `json:"wallet_address,omitempty"`
		Passphrase    string `json:"passphrase,omitempty"` // 新增
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误"})
		return
	}

	err := h.service.ConfigAdminAccount(req.AccountType, req.APIKey, req.APISecret, req.WalletAddress, req.Passphrase)
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
	userIDStr, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未授权"})
		return
	}

	userID := userIDStr.(int)

	summary, err := h.service.GetDashboardSummary(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

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

// AdminDeleteRecharge 删除充值记录
func (h *Handler) AdminDeleteRecharge(c *gin.Context) {
	rechargeID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "充值ID无效"})
		return
	}

	user := c.MustGet("user").(*model.User)
	if err := h.service.DeleteRecharge(rechargeID, user.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "充值记录已删除"})
}

// AdminGetRechargeStats 获取充值统计
func (h *Handler) AdminGetRechargeStats(c *gin.Context) {
	stats, err := h.service.GetRechargeStatistics()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// AdminCreateAPIUser 通过API创建独立的Dashboard用户
func (h *Handler) AdminCreateAPIUser(c *gin.Context) {
	var req struct {
		Username   string `json:"username" binding:"required"` // 改为username
		Password   string `json:"password" binding:"required"`
		APIType    string `json:"api_type" binding:"required"`
		APIKey     string `json:"api_key" binding:"required"`
		APISecret  string `json:"api_secret" binding:"required"`
		Passphrase string `json:"passphrase"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误: " + err.Error()})
		return
	}

	// 验证API类型
	if req.APIType != "Binance" && req.APIType != "OKX" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "API类型只能是 Binance 或 OKX"})
		return
	}

	// OKX必须提供Passphrase
	if req.APIType == "OKX" && req.Passphrase == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "OKX账户必须提供Passphrase"})
		return
	}

	// 创建API用户
	userID, adminAccountID, err := h.service.CreateAPIUser(
		req.Username, // 传入username
		req.Password,
		req.APIType,
		req.APIKey,
		req.APISecret,
		req.Passphrase,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":          "API用户创建成功",
		"user_id":          userID,
		"admin_account_id": adminAccountID,
		"login_username":   req.Username, // 返回username
		"dashboard_url":    "/dashboard",
	})
}

// GetAPIDashboard 获取API用户Dashboard
func (h *Handler) GetAPIDashboard(c *gin.Context) {
	userIDStr, _ := c.Get("userID")
	userID := userIDStr.(int)

	data, err := h.service.GetAPIDashboardData(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, data)
}

// AdminDepositToExchange Admin直接充值到交易所（进入系统账户）
func (h *Handler) AdminDepositToExchange(c *gin.Context) {
	var req struct {
		AdminAccountID int     `json:"admin_account_id" binding:"required"`
		Amount         float64 `json:"amount" binding:"required"`
		Currency       string  `json:"currency" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	err := h.service.AdminDepositToExchange(req.AdminAccountID, req.Amount, req.Currency)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "充值到系统账户成功"})
}
