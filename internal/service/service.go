package service

import (
	"crypto-final/internal/model"
	"crypto-final/internal/repository"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

type Service struct {
	repo                *repository.Repository
	walletService       *WalletService
	userDefaultPassword string
}

func NewService(repo *repository.Repository) *Service {
	return &Service{
		repo:                repo,
		walletService:       NewWalletService(),
		userDefaultPassword: "user123456", // 默认值
	}
}

// SetUserDefaultPassword 设置用户默认密码
func (s *Service) SetUserDefaultPassword(password string) {
	s.userDefaultPassword = password
}

// HashPassword 密码哈希
func (s *Service) HashPassword(password string) string {
	hash := sha256.Sum256([]byte(password))
	return hex.EncodeToString(hash[:])
}

// Login 登录
func (s *Service) Login(phone, password string) (*model.User, error) {
	user, err := s.repo.GetUserByPhone(phone)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("用户不存在")
	}

	passwordHash := s.HashPassword(password)
	if user.PasswordHash != passwordHash {
		return nil, errors.New("密码错误")
	}

	return user, nil
}

// AdminCreateUser 管理员创建Dashboard用户（密码固定abc123456）
func (s *Service) AdminCreateUser(phone string) (int64, error) {
	// 检查用户是否已存在
	existing, err := s.repo.GetUserByPhone(phone)
	if err != nil {
		return 0, err
	}
	if existing != nil {
		return 0, errors.New("手机号已存在")
	}

	// 使用配置的默认密码
	passwordHash := s.HashPassword(s.userDefaultPassword)

	return s.repo.CreateUser(phone, passwordHash)
}

// AdminRecharge 管理员为用户充值
func (s *Service) AdminRecharge(userID, adminAccountID int, amount float64, currency string) error {
	// 获取Admin账户当前余额作为基准
	account, err := s.repo.GetAdminAccountByID(adminAccountID)
	if err != nil {
		return err
	}
	if account == nil {
		return errors.New("Admin账户不存在")
	}

	baseBalance := account.CurrentBalance
	if baseBalance == 0 {
		// 如果还没有余额记录，先获取一次
		balance, err := s.walletService.GetBalance(account)
		if err == nil {
			baseBalance = balance
			s.repo.UpdateAdminAccountBalance(adminAccountID, balance)
		} else {
			return fmt.Errorf("无法获取账户余额: %v", err)
		}
	}

	// 创建充值记录
	recharge := &model.Recharge{
		UserID:         userID,
		AdminAccountID: adminAccountID,
		Amount:         amount,
		Currency:       currency,
		RechargeAt:     time.Now(),
		BaseBalance:    baseBalance, // 记录基准余额
		IsActive:       true,
	}

	_, err = s.repo.CreateRecharge(recharge)
	return err
}

// GetAllDashboardUsersWithStats 获取所有Dashboard用户及统计
func (s *Service) GetAllDashboardUsersWithStats() ([]*model.DashboardUserListItem, error) {
	users, err := s.repo.GetAllDashboardUsers()
	if err != nil {
		return nil, err
	}

	var result []*model.DashboardUserListItem
	for _, user := range users {
		stats := s.CalculateUserStats(user.ID)
		item := &model.DashboardUserListItem{
			UserID:        user.ID,
			Phone:         user.Phone,
			TotalRecharge: stats.TotalRecharge,
			CurrentValue:  stats.CurrentValue,
			TotalProfit:   stats.TotalProfit,
			ProfitRate:    stats.TotalProfitRate,
			RechargeCount: stats.RechargeCount,
			IsActive:      true,
			CreatedAt:     user.CreatedAt.Format("2006-01-02"),
		}
		result = append(result, item)
	}

	return result, nil
}

// GetAdminAccountsStatus 获取Admin账户状态
func (s *Service) GetAdminAccountsStatus() ([]*model.AdminAccountStatusResponse, error) {
	accounts, err := s.repo.GetAllAdminAccounts()
	if err != nil {
		return nil, err
	}

	today := time.Now().Format("2006-01-02")
	var result []*model.AdminAccountStatusResponse

	for _, acc := range accounts {
		isConfigured := false
		address := ""

		if acc.AccountType == "Wallet" {
			if acc.WalletAddress != "" {
				address = acc.WalletAddress
			}

		} else {
			isConfigured = acc.APIKey != ""

			if isConfigured {
				address = "API已配置"
			}
		}

		// 获取今日变化
		dailyChange, dailyChangeRate, _ := s.repo.GetTodayAdminAccountChange(acc.ID, today)

		status := &model.AdminAccountStatusResponse{
			ID:              acc.ID,
			AccountType:     acc.AccountType,
			Address:         address,
			CurrentBalance:  acc.CurrentBalance,
			IsConfigured:    isConfigured,
			DailyChange:     dailyChange,
			DailyChangeRate: dailyChangeRate,
		}
		result = append(result, status)
	}

	return result, nil
}

// ConfigAdminAccount 配置Admin账户
func (s *Service) ConfigAdminAccount(accountType, apiKey, apiSecret, walletAddress string) error {
	return s.repo.UpdateAdminAccountConfig(accountType, apiKey, apiSecret, walletAddress)
}

// GetDashboardSummary Dashboard用户总览
func (s *Service) GetDashboardSummary(userID int) *model.DashboardSummary {
	return s.CalculateUserStats(userID)
}

// GetUserRechargesWithProfit 获取用户充值及盈亏
func (s *Service) GetUserRechargesWithProfit(userID int) ([]*model.RechargeWithProfit, error) {
	recharges, err := s.repo.GetRechargesByUserID(userID)
	if err != nil {
		return nil, err
	}

	var result []*model.RechargeWithProfit
	for _, r := range recharges {
		// 获取最新盈亏
		latestProfit, _ := s.repo.GetLatestRechargeProfit(r.ID)

		profit := 0.0
		profitRate := 0.0
		if latestProfit != nil {
			profit = latestProfit.Profit
			profitRate = latestProfit.ProfitRate
		}

		// 获取账户类型
		account, _ := s.repo.GetAdminAccountByID(r.AdminAccountID)
		accountType := ""
		if account != nil {
			accountType = account.AccountType
		}

		// 计算持有天数
		daysHeld := int(time.Since(r.RechargeAt).Hours() / 24)

		item := &model.RechargeWithProfit{
			Recharge:      r,
			AccountType:   accountType,
			CurrentProfit: profit,
			CurrentRate:   profitRate,
			DaysHeld:      daysHeld,
		}
		result = append(result, item)
	}

	return result, nil
}

// GetRechargeProfitHistory 获取单笔充值的历史盈亏
func (s *Service) GetRechargeProfitHistory(rechargeID, userID int) ([]*model.RechargeDailyProfit, error) {
	// 验证所有权
	recharge, err := s.repo.GetRechargeByID(rechargeID)
	if err != nil {
		return nil, err
	}
	if recharge == nil {
		return nil, errors.New("充值记录不存在")
	}
	if recharge.UserID != userID {
		return nil, errors.New("无权访问此充值记录")
	}

	return s.repo.GetRechargeProfitHistory(rechargeID)
}

// CalculateUserStats 计算用户统计数据
func (s *Service) CalculateUserStats(userID int) *model.DashboardSummary {
	recharges, err := s.repo.GetRechargesByUserID(userID)
	if err != nil {
		return &model.DashboardSummary{}
	}

	totalRecharge := 0.0
	totalCurrentValue := 0.0

	for _, r := range recharges {
		totalRecharge += r.Amount

		// 获取最新盈亏
		latestProfit, _ := s.repo.GetLatestRechargeProfit(r.ID)
		if latestProfit != nil {
			// 当前价值 = 充值金额 × (1 + 盈亏率)
			currentValue := r.Amount * (1 + latestProfit.ProfitRate/100)
			totalCurrentValue += currentValue
		} else {
			totalCurrentValue += r.Amount
		}
	}

	totalProfit := totalCurrentValue - totalRecharge
	totalProfitRate := 0.0
	if totalRecharge > 0 {
		totalProfitRate = (totalProfit / totalRecharge) * 100
	}

	return &model.DashboardSummary{
		TotalRecharge:   totalRecharge,
		CurrentValue:    totalCurrentValue,
		TotalProfit:     totalProfit,
		TotalProfitRate: totalProfitRate,
		RechargeCount:   len(recharges),
	}
}

// UpdateDailyBalances 定时任务：更新每日余额
func (s *Service) UpdateDailyBalances() error {
	today := time.Now().Format("2006-01-02")
	fmt.Printf("\n========== 开始每日余额检查 [%s] ==========\n", today)

	// 安全检查：确保walletService不为nil
	if s.walletService == nil {
		return fmt.Errorf("钱包服务未初始化")
	}

	// 添加这两行 ↓
	successCount := 0
	errorCount := 0

	// 步骤1: 更新3个Admin账户的余额
	accounts, err := s.repo.GetAllAdminAccounts()
	if err != nil {
		return err
	}

	for _, account := range accounts {
		if !account.IsActive {
			continue
		}

		// 读取余额
		balance, err := s.walletService.GetBalance(account)
		if err != nil {
			fmt.Printf("❌ 读取%s余额失败: %v\n", account.AccountType, err)
			errorCount++
			continue
		}

		// 验证余额有效性
		if balance < 0 {
			fmt.Printf("⚠️  %s余额异常: %.2f，跳过\n", account.AccountType, balance)
			errorCount++
			continue
		}

		// 计算日变化
		yesterdayBalance, _ := s.repo.GetLatestAdminAccountBalance(account.ID)
		if yesterdayBalance == 0 {
			yesterdayBalance = balance
		}

		dailyChange := balance - yesterdayBalance
		dailyChangeRate := 0.0
		if yesterdayBalance > 0 {
			dailyChangeRate = (dailyChange / yesterdayBalance) * 100
		}

		// 保存余额记录
		s.repo.SaveAdminAccountBalance(account.ID, today, balance, dailyChange, dailyChangeRate)
		s.repo.UpdateAdminAccountBalance(account.ID, balance)

		fmt.Printf("✓ %s 账户: $%.2f (变化: %+.2f, %+.2f%%)\n",
			account.AccountType, balance, dailyChange, dailyChangeRate)
	}

	// 步骤2: 计算所有充值的盈亏
	recharges, err := s.repo.GetAllActiveRecharges()
	if err != nil {
		return err
	}

	fmt.Printf("\n开始计算 %d 笔充值的盈亏...\n", len(recharges))
	for _, recharge := range recharges {
		// 获取Admin账户今天的余额
		currentBalance, err := s.repo.GetAdminAccountBalanceByDate(recharge.AdminAccountID, today)
		if err != nil || currentBalance == 0 {
			// 如果没有今天的记录，使用当前余额
			account, _ := s.repo.GetAdminAccountByID(recharge.AdminAccountID)
			if account != nil {
				currentBalance = account.CurrentBalance
			}
		}

		if currentBalance == 0 {
			continue
		}

		// 计算盈亏
		profit, profitRate := s.CalculateProfit(recharge.Amount, recharge.BaseBalance, currentBalance)

		// 保存
		err = s.repo.SaveRechargeDailyProfit(recharge.ID, today, currentBalance, profit, profitRate)
		if err != nil {
			fmt.Printf("❌ 充值ID %d 盈亏保存失败: %v\n", recharge.ID, err)
			continue
		}

		successCount++
	}

	fmt.Printf("✓ 成功计算充值盈亏\n")
	fmt.Printf("========== 每日余额检查完成 ==========\n\n")

	return nil
}

// CalculateProfit 计算盈亏
func (s *Service) CalculateProfit(amount, baseBalance, currentBalance float64) (profit, profitRate float64) {
	if baseBalance == 0 {
		return 0, 0
	}

	// 盈亏率 = (当前余额 / 基准余额) - 1
	profitRate = (currentBalance/baseBalance - 1) * 100

	// 盈亏金额 = 充值金额 × (盈亏率 / 100)
	profit = amount * profitRate / 100

	return profit, profitRate
}
