package model

import (
	"time"
)

// User Dashboard用户（虚拟账本容器）
type User struct {
	ID           int       `json:"id"`
	Phone        string    `json:"phone"`
	PasswordHash string    `json:"-"`
	IsAdmin      bool      `json:"is_admin"`
	CreatedAt    time.Time `json:"created_at"`
}

// AdminAccount Admin绑定的3个真实账户
type AdminAccount struct {
	ID             int       `json:"id"`
	AccountType    string    `json:"account_type"`
	APIKey         string    `json:"api_key,omitempty"`
	APISecret      string    `json:"api_secret,omitempty"`
	WalletAddress  string    `json:"wallet_address,omitempty"`
	CurrentBalance float64   `json:"current_balance"`
	IsActive       bool      `json:"is_active"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// AdminAccountBalance Admin账户每日余额记录
type AdminAccountBalance struct {
	ID              int       `json:"id"`
	AdminAccountID  int       `json:"admin_account_id"`
	RecordDate      string    `json:"record_date"` // YYYY-MM-DD
	Balance         float64   `json:"balance"`
	DailyChange     float64   `json:"daily_change"`
	DailyChangeRate float64   `json:"daily_change_rate"`
	CreatedAt       time.Time `json:"created_at"`
}

// Recharge Dashboard用户的充值记录
type Recharge struct {
	ID             int       `json:"id"`
	UserID         int       `json:"user_id"`
	AdminAccountID int       `json:"admin_account_id"` // 充值到哪个Admin账户
	Amount         float64   `json:"amount"`
	Currency       string    `json:"currency"`
	RechargeAt     time.Time `json:"recharge_at"`
	BaseBalance    float64   `json:"base_balance"` // 充值时Admin账户的余额（基准）
	IsActive       bool      `json:"is_active"`
	CreatedAt      time.Time `json:"created_at"`
}

// RechargeDailyProfit 每笔充值的每日盈亏
type RechargeDailyProfit struct {
	ID                  int       `json:"id"`
	RechargeID          int       `json:"recharge_id"`
	RecordDate          string    `json:"record_date"`
	AdminAccountBalance float64   `json:"admin_account_balance"`
	Profit              float64   `json:"profit"`
	ProfitRate          float64   `json:"profit_rate"`
	CreatedAt           time.Time `json:"created_at"`
}

// Request/Response 模型

type LoginRequest struct {
	Phone    string `json:"phone" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type AdminCreateUserRequest struct {
	Phone string `json:"phone" binding:"required"`
}

type AdminRechargeRequest struct {
	UserID         int     `json:"user_id" binding:"required"`
	AdminAccountID int     `json:"admin_account_id" binding:"required"`
	Amount         float64 `json:"amount" binding:"required"`
	Currency       string  `json:"currency" binding:"required"`
}

type AdminAccountConfigRequest struct {
	AccountType   string `json:"account_type" binding:"required"`
	APIKey        string `json:"api_key,omitempty"`
	APISecret     string `json:"api_secret,omitempty"`
	WalletAddress string `json:"wallet_address,omitempty"`
}

type AdminAccountStatusResponse struct {
	ID              int     `json:"id"`
	AccountType     string  `json:"account_type"`
	Address         string  `json:"address,omitempty"`
	CurrentBalance  float64 `json:"current_balance"`
	IsConfigured    bool    `json:"is_configured"`
	DailyChange     float64 `json:"daily_change"`
	DailyChangeRate float64 `json:"daily_change_rate"`
}

type DashboardUserListItem struct {
	UserID        int     `json:"user_id"`
	Phone         string  `json:"phone"`
	TotalRecharge float64 `json:"total_recharge"`
	CurrentValue  float64 `json:"current_value"`
	TotalProfit   float64 `json:"total_profit"`
	ProfitRate    float64 `json:"profit_rate"`
	RechargeCount int     `json:"recharge_count"`
	IsActive      bool    `json:"is_active"`
	CreatedAt     string  `json:"created_at"`
}

type DashboardSummary struct {
	TotalRecharge   float64 `json:"total_recharge"`
	CurrentValue    float64 `json:"current_value"`
	TotalProfit     float64 `json:"total_profit"`
	TotalProfitRate float64 `json:"total_profit_rate"`
	RechargeCount   int     `json:"recharge_count"`
}

type RechargeWithProfit struct {
	Recharge      *Recharge `json:"recharge"`
	AccountType   string    `json:"account_type"`
	CurrentProfit float64   `json:"current_profit"`
	CurrentRate   float64   `json:"current_rate"`
	DaysHeld      int       `json:"days_held"`
}
