package model

import (
	"time"
)

// User Dashboard用户（虚拟账本容器）
// User模型添加字段
type User struct {
	ID                int       `json:"id"`
	Phone             string    `json:"phone"`
	PasswordHash      string    `json:"-"`
	IsAdmin           bool      `json:"is_admin"`
	IsActive          bool      `json:"is_active"`
	IsAPIUser         bool      `json:"is_api_user"`
	APIAdminAccountID int       `json:"api_admin_account_id"`
	InitialBalance    float64   `json:"initial_balance"` // 新增
	CreatedAt         time.Time `json:"created_at"`
}

// AdminAccount Admin绑定的3个真实账户
type AdminAccount struct {
	ID             int       `json:"id"`
	AccountType    string    `json:"account_type"`
	APIKey         string    `json:"api_key,omitempty"`
	APISecret      string    `json:"api_secret,omitempty"`
	WalletAddress  string    `json:"wallet_address,omitempty"`
	Passphrase     string    `json:"passphrase,omitempty"` // ← 新增
	CurrentBalance float64   `json:"current_balance"`
	TotalShares    float64   `json:"total_shares"` // 新增
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
	Shares         float64   `json:"shares"`       // 新增
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

	// 新增化率
	MonthlyRate   float64 `json:"monthly_rate"`   // 月化率
	QuarterlyRate float64 `json:"quarterly_rate"` // 季度化率
	AnnualRate    float64 `json:"annual_rate"`    // 年化率

	// 持有天数
	AvgHoldDays int `json:"avg_hold_days"`
}

type RechargeWithProfit struct {
	Recharge      *Recharge `json:"recharge"`
	AccountType   string    `json:"account_type"`
	CurrentProfit float64   `json:"current_profit"`
	CurrentRate   float64   `json:"current_rate"`
	DaysHeld      int       `json:"days_held"`
}

// UserDetailResponse 用户详情（含充值记录）
type UserDetailResponse struct {
	UserID        int               `json:"user_id"`
	Phone         string            `json:"phone"`
	IsActive      bool              `json:"is_active"`
	TotalRecharge float64           `json:"total_recharge"`
	CurrentValue  float64           `json:"current_value"`
	TotalProfit   float64           `json:"total_profit"`
	ProfitRate    float64           `json:"profit_rate"`
	RechargeCount int               `json:"recharge_count"`
	Recharges     []*RechargeDetail `json:"recharges"`
}

// RechargeDetail 充值详情
type RechargeDetail struct {
	ID             int       `json:"id"`
	Amount         float64   `json:"amount"`
	Currency       string    `json:"currency"`
	AdminAccountID int       `json:"admin_account_id"`
	AccountType    string    `json:"account_type"`
	RechargeAt     time.Time `json:"recharge_at"`
	BaseBalance    float64   `json:"base_balance"`
	CurrentProfit  float64   `json:"current_profit"`
	CurrentRate    float64   `json:"current_rate"`
	IsActive       bool      `json:"is_active"`
}

// RechargeStatistics 充值统计
type RechargeStatistics struct {
	TotalRecharges    float64                  `json:"total_recharges"`
	AccountStatistics map[string]*AccountStats `json:"account_statistics"`
}

// AccountStats 单个账户的充值统计
type AccountStats struct {
	AccountType string  `json:"account_type"`
	USDC        float64 `json:"usdc"`
	USDT        float64 `json:"usdt"`
	Total       float64 `json:"total"`
}

type RechargeResponse struct {
	ID            int       `json:"id"`
	Amount        float64   `json:"amount"`
	Currency      string    `json:"currency"`
	AccountType   string    `json:"account_type"`
	RechargeAt    time.Time `json:"recharge_at"`
	CurrentProfit float64   `json:"current_profit"`
	CurrentRate   float64   `json:"current_rate"`
}

type UserSummary struct {
	UserID        int     `json:"user_id"`
	Phone         string  `json:"phone"`
	IsActive      bool    `json:"is_active"` // 确保有这个字段
	TotalRecharge float64 `json:"total_recharge"`
	CurrentValue  float64 `json:"current_value"`
	TotalProfit   float64 `json:"total_profit"`
	RechargeCount int     `json:"recharge_count"`
}

// API用户Dashboard数据
type APIDashboardData struct {
	CurrentBalance float64        `json:"current_balance"`  // 当前总资金
	InitialBalance float64        `json:"initial_balance"`  // 初始本金
	TotalProfit    float64        `json:"total_profit"`     // 总盈亏
	ProfitRate     float64        `json:"profit_rate"`      // 盈亏率
	Positions      []Position     `json:"positions"`        // 当前持仓
	Orders         []Order        `json:"orders"`           // 当前委托
	HistoryTrades  []HistoryTrade `json:"history_trades"`   // 历史记录
	LastUpdateTime string         `json:"last_update_time"` // 最后更新时间
}

// 持仓信息
type Position struct {
	Symbol            string  `json:"symbol"`              // 交易对
	Side              string  `json:"side"`                // 方向: "LONG"/"SHORT"
	Size              float64 `json:"size"`                // 持仓数量
	EntryPrice        float64 `json:"entry_price"`         // 开仓均价
	MarkPrice         float64 `json:"mark_price"`          // 标记价格
	UnrealizedPnl     float64 `json:"unrealized_pnl"`      // 未实现盈亏
	UnrealizedPnlRate float64 `json:"unrealized_pnl_rate"` // 盈亏率
	Leverage          int     `json:"leverage"`            // 杠杆倍数
	MarginType        string  `json:"margin_type"`         // 全仓/逐仓
}

// 委托信息
type Order struct {
	OrderID     string  `json:"order_id"`     // 订单ID
	Symbol      string  `json:"symbol"`       // 交易对
	Side        string  `json:"side"`         // 方向: "BUY"/"SELL"
	Type        string  `json:"type"`         // 类型: "LIMIT"/"MARKET"
	Price       float64 `json:"price"`        // 委托价格
	OrigQty     float64 `json:"orig_qty"`     // 原始数量
	ExecutedQty float64 `json:"executed_qty"` // 已成交数量
	Status      string  `json:"status"`       // 状态
	Time        string  `json:"time"`         // 下单时间
}

// 历史成交
type HistoryTrade struct {
	Symbol      string  `json:"symbol"`       // 交易对
	Side        string  `json:"side"`         // 方向
	OpenTime    string  `json:"open_time"`    // 开仓时间
	CloseTime   string  `json:"close_time"`   // 平仓时间
	OpenPrice   float64 `json:"open_price"`   // 开仓价
	ClosePrice  float64 `json:"close_price"`  // 平仓价
	Quantity    float64 `json:"quantity"`     // 数量
	RealizedPnl float64 `json:"realized_pnl"` // 已实现盈亏
	Commission  float64 `json:"commission"`   // 手续费
}
