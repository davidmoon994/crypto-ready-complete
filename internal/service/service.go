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
func (s *Service) AdminRecharge(userID int, adminAccountID int, amount float64, currency string) error {
	if amount <= 0 {
		return errors.New("充值金额必须大于0")
	}

	// 验证用户存在
	user, err := s.repo.GetUserByID(userID)
	if err != nil {
		return err
	}
	if user == nil {
		return errors.New("用户不存在")
	}

	// 验证Admin账户存在
	adminAccount, err := s.repo.GetAdminAccountByID(adminAccountID)
	if err != nil {
		return err
	}
	if adminAccount == nil {
		return errors.New("Admin账户不存在")
	}

	// 获取当前账户状态
	currentBalance := adminAccount.CurrentBalance
	currentShares := adminAccount.TotalShares

	fmt.Printf("\n💰 充值操作:\n")
	fmt.Printf("  用户: %s\n", user.Phone)
	fmt.Printf("  充值金额: $%.2f %s\n", amount, currency)
	fmt.Printf("  充值到: %s\n", adminAccount.AccountType)
	fmt.Printf("  充值前余额: $%.2f\n", currentBalance)
	fmt.Printf("  充值前总份额: %.4f\n", currentShares)

	// 计算份额
	var purchasedShares float64
	var netValue float64

	if currentShares == 0 || currentBalance == 0 {
		// 第一笔充值：初始化净值为1
		purchasedShares = amount
		netValue = 1.0
		fmt.Printf("  首次充值，净值初始化为: $1.00\n")
	} else {
		// 后续充值：根据当前净值计算份额
		netValue = currentBalance / currentShares
		purchasedShares = amount / netValue
		fmt.Printf("  当前净值: $%.4f\n", netValue)
	}

	fmt.Printf("  购买份额: %.4f\n", purchasedShares)

	// 更新Admin账户的总份额
	newTotalShares := currentShares + purchasedShares
	if err := s.repo.UpdateAdminAccountShares(adminAccountID, newTotalShares); err != nil {
		return fmt.Errorf("更新账户份额失败: %v", err)
	}

	// 创建充值记录
	rechargeID, err := s.repo.CreateRechargeWithShares(
		userID,
		adminAccountID,
		amount,
		currency,
		currentBalance, // base_balance: 充值时的账户余额
		purchasedShares,
	)
	if err != nil {
		// 回滚份额更新
		s.repo.UpdateAdminAccountShares(adminAccountID, currentShares)
		return fmt.Errorf("创建充值记录失败: %v", err)
	}

	fmt.Printf("✓ 充值记录已创建 (ID: %d)\n", rechargeID)
	fmt.Printf("✓ 新总份额: %.4f\n", newTotalShares)

	return nil
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
		fmt.Printf("❌ GetAllAdminAccounts error: %v\n", err)
		return nil, fmt.Errorf("获取Admin账户失败: %v", err)
	}

	if len(accounts) == 0 {
		fmt.Println("⚠️  没有找到Admin账户")
		return []*model.AdminAccountStatusResponse{}, nil
	}

	today := time.Now().Format("2006-01-02")
	var result []*model.AdminAccountStatusResponse

	for _, acc := range accounts {
		fmt.Printf("处理账户: %s (ID: %d)\n", acc.AccountType, acc.ID)

		isConfigured := false
		address := ""

		if acc.AccountType == "Wallet" {
			isConfigured = acc.WalletAddress != ""
			if isConfigured {
				address = acc.WalletAddress // 显示完整钱包地址
			} else {
				address = "未配置"
			}
		} else {
			// 对于Binance和OKX，显示API Key的前8位作为标识
			isConfigured = acc.APIKey != "" && acc.APISecret != ""
			if acc.AccountType == "OKX" {
				isConfigured = isConfigured && acc.Passphrase != ""
			}

			if isConfigured {
				// 显示API Key的部分内容作为标识
				if len(acc.APIKey) > 8 {
					address = "API: " + acc.APIKey[:8] + "****"
				} else {
					address = "API已配置"
				}
			} else {
				address = "未配置"
			}
		}

		// 获取今日变化
		dailyChange, dailyChangeRate, err := s.repo.GetTodayAdminAccountChange(acc.ID, today)
		if err != nil {
			dailyChange = 0
			dailyChangeRate = 0
		}

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
func (s *Service) ConfigAdminAccount(accountType, apiKey, apiSecret, walletAddress, passphrase string) error {
	return s.repo.UpdateAdminAccountConfig(accountType, apiKey, apiSecret, walletAddress, passphrase)
}

// UpdateUserStatus 更新用户状态（直接设置）
func (s *Service) UpdateUserStatus(userID int, isActive bool) error {
	return s.repo.UpdateUserStatus(userID, isActive)
}

// GetUserByID 获取用户信息
func (s *Service) GetUserByID(userID int) (*model.User, error) {
	return s.repo.GetUserByID(userID)
}

// GetDashboardSummary Dashboard用户总览
func (s *Service) GetDashboardSummary(userID int) *model.DashboardSummary {
	recharges, err := s.repo.GetRechargesByUserID(userID)
	if err != nil {
		return &model.DashboardSummary{}
	}

	totalRecharge := 0.0
	totalCurrentValue := 0.0
	totalHoldDays := 0

	for _, r := range recharges {
		if !r.IsActive {
			continue
		}

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

		// 计算持有天数
		holdDays := int(time.Since(r.RechargeAt).Hours() / 24)
		if holdDays < 1 {
			holdDays = 1 // 至少算1天
		}
		totalHoldDays += holdDays
	}

	totalProfit := totalCurrentValue - totalRecharge
	totalProfitRate := 0.0
	avgHoldDays := 0

	activeCount := 0
	for _, r := range recharges {
		if r.IsActive {
			activeCount++
		}
	}

	if totalRecharge > 0 {
		totalProfitRate = (totalProfit / totalRecharge) * 100
	}

	if activeCount > 0 {
		avgHoldDays = totalHoldDays / activeCount
	}

	// 计算化率
	monthlyRate := 0.0
	quarterlyRate := 0.0
	annualRate := 0.0

	if avgHoldDays > 0 && totalProfitRate != 0 {
		// 日化率
		dailyRate := totalProfitRate / float64(avgHoldDays)

		// 月化率 = 日化率 × 30
		monthlyRate = dailyRate * 30

		// 季度化率 = 日化率 × 90
		quarterlyRate = dailyRate * 90

		// 年化率 = 日化率 × 365
		annualRate = dailyRate * 365
	}

	return &model.DashboardSummary{
		TotalRecharge:   totalRecharge,
		CurrentValue:    totalCurrentValue,
		TotalProfit:     totalProfit,
		TotalProfitRate: totalProfitRate,
		RechargeCount:   activeCount,
		MonthlyRate:     monthlyRate,
		QuarterlyRate:   quarterlyRate,
		AnnualRate:      annualRate,
		AvgHoldDays:     avgHoldDays,
	}
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
		successCount++
	}

	// 步骤2: 计算每笔充值的盈亏（基于份额）
	fmt.Println("\n开始计算充值盈亏...")

	allRecharges, err := s.repo.GetAllActiveRecharges()
	if err != nil {
		fmt.Printf("❌ 获取充值记录失败: %v\n", err)
		return err
	}

	fmt.Printf("开始计算%d笔充值的盈亏...\n", len(allRecharges))

	for _, recharge := range allRecharges {
		// 获取Admin账户当前状态
		adminAccount, err := s.repo.GetAdminAccountByID(recharge.AdminAccountID)
		if err != nil || adminAccount == nil {
			fmt.Printf("⚠️  充值ID %d: 无法获取Admin账户\n", recharge.ID)
			continue
		}

		currentBalance := adminAccount.CurrentBalance
		totalShares := adminAccount.TotalShares

		// 核心算法：基于份额计算
		var currentValue float64
		var profit float64
		var profitRate float64

		if totalShares > 0 && recharge.Shares > 0 {
			// 当前净值 = 账户余额 / 总份额
			netValue := currentBalance / totalShares

			// 用户当前价值 = 持有份额 × 净值
			currentValue = recharge.Shares * netValue

			// 盈亏 = 当前价值 - 本金
			profit = currentValue - recharge.Amount

			// 盈亏率
			if recharge.Amount > 0 {
				profitRate = (profit / recharge.Amount) * 100
			}

			fmt.Printf("  充值ID %d: 本金=$%.2f, 份额=%.4f, 净值=$%.4f, 当前=$%.2f, 盈亏=%s$%.2f (%.2f%%)\n",
				recharge.ID,
				recharge.Amount,
				recharge.Shares,
				netValue,
				currentValue,
				formatSign(profit), abs(profit),
				profitRate)
		} else {
			// 异常情况
			currentValue = recharge.Amount
			profit = 0
			profitRate = 0
			fmt.Printf("⚠️  充值ID %d: 份额数据异常\n", recharge.ID)
		}

		// 保存盈亏记录
		err = s.repo.SaveRechargeDailyProfit(recharge.ID, today, currentBalance, profit, profitRate)
		if err != nil {
			fmt.Printf("⚠️  充值ID %d: 保存盈亏失败: %v\n", recharge.ID, err)
		}
	}

	fmt.Println("✓ 成功计算充值盈亏")
	fmt.Printf("==========每日余额检查完成 (成功: %d, 失败: %d) ==========\n\n", successCount, errorCount)

	return nil
}

// 辅助函数
func formatSign(value float64) string {
	if value >= 0 {
		return "+"
	}
	return ""
}

func abs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

// ToggleUserStatus 切换用户状态
func (s *Service) ToggleUserStatus(userID int) error {
	user, err := s.repo.GetUserByID(userID)
	if err != nil {
		return err
	}
	if user == nil {
		return errors.New("用户不存在")
	}

	// 切换状态
	newStatus := !user.IsActive
	return s.repo.UpdateUserStatus(userID, newStatus)
}

func (s *Service) GetDashboardRecharges(userID int) ([]*model.RechargeResponse, error) {
	recharges, err := s.repo.GetRechargesByUserID(userID)
	if err != nil {
		return nil, err
	}

	var result []*model.RechargeResponse
	for _, r := range recharges {
		// 只返回活跃的充值
		if !r.IsActive {
			continue
		}

		// 获取账户类型
		account, _ := s.repo.GetAdminAccountByID(r.AdminAccountID)
		accountType := "未知"
		if account != nil {
			accountType = account.AccountType
		}

		// 获取最新盈亏
		latestProfit, _ := s.repo.GetLatestRechargeProfit(r.ID)

		currentProfit := 0.0
		currentRate := 0.0

		if latestProfit != nil {
			currentProfit = latestProfit.Profit
			currentRate = latestProfit.ProfitRate
		}

		response := &model.RechargeResponse{
			ID:            r.ID,
			Amount:        r.Amount,
			Currency:      r.Currency,
			AccountType:   accountType,
			RechargeAt:    r.RechargeAt,
			CurrentProfit: currentProfit,
			CurrentRate:   currentRate,
		}
		result = append(result, response)
	}

	return result, nil
}

// GetUserDetail 获取用户详情（含充值记录）
func (s *Service) GetUserDetail(userID int) (*model.UserDetailResponse, error) {
	user, err := s.repo.GetUserByID(userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("用户不存在")
	}

	// 获取充值记录
	recharges, err := s.repo.GetRechargesByUserID(userID)
	if err != nil {
		return nil, err
	}

	var rechargeDetails []*model.RechargeDetail
	totalRecharge := 0.0
	totalCurrentValue := 0.0

	for _, r := range recharges {
		// 获取账户类型
		account, _ := s.repo.GetAdminAccountByID(r.AdminAccountID)
		accountType := ""
		if account != nil {
			accountType = account.AccountType
		}

		// 获取最新盈亏
		latestProfit, _ := s.repo.GetLatestRechargeProfit(r.ID)
		currentProfit := 0.0
		currentRate := 0.0
		currentValue := r.Amount

		if latestProfit != nil {
			currentProfit = latestProfit.Profit
			currentRate = latestProfit.ProfitRate
			currentValue = r.Amount * (1 + currentRate/100)
		}

		if r.IsActive {
			totalRecharge += r.Amount
			totalCurrentValue += currentValue
		}

		detail := &model.RechargeDetail{
			ID:             r.ID,
			Amount:         r.Amount,
			Currency:       r.Currency,
			AdminAccountID: r.AdminAccountID,
			AccountType:    accountType,
			RechargeAt:     r.RechargeAt,
			BaseBalance:    r.BaseBalance,
			CurrentProfit:  currentProfit,
			CurrentRate:    currentRate,
			IsActive:       r.IsActive,
		}
		rechargeDetails = append(rechargeDetails, detail)
	}

	totalProfit := totalCurrentValue - totalRecharge
	profitRate := 0.0
	if totalRecharge > 0 {
		profitRate = (totalProfit / totalRecharge) * 100
	}

	return &model.UserDetailResponse{
		UserID:        user.ID,
		Phone:         user.Phone,
		IsActive:      user.IsActive,
		TotalRecharge: totalRecharge,
		CurrentValue:  totalCurrentValue,
		TotalProfit:   totalProfit,
		ProfitRate:    profitRate,
		RechargeCount: len(recharges),
		Recharges:     rechargeDetails,
	}, nil
}

// DeleteRecharge 删除充值记录
func (s *Service) DeleteRecharge(rechargeID, adminUserID int) error {
	// 验证是管理员操作
	recharge, err := s.repo.GetRechargeByID(rechargeID)
	if err != nil {
		return err
	}
	if recharge == nil {
		return errors.New("充值记录不存在")
	}

	return s.repo.DeleteRecharge(rechargeID)
}

// GetRechargeStatistics 获取充值统计
func (s *Service) GetRechargeStatistics() (*model.RechargeStatistics, error) {
	// 获取充值统计数据
	stats, err := s.repo.GetRechargeStatistics()
	if err != nil {
		return nil, err
	}

	// 获取所有Admin账户
	accounts, err := s.repo.GetAllAdminAccounts()
	if err != nil {
		return nil, err
	}

	accountStatistics := make(map[string]*model.AccountStats)
	totalRecharges := 0.0

	// 按账户类型汇总
	for _, account := range accounts {
		accountStats := &model.AccountStats{
			AccountType: account.AccountType,
			USDC:        0,
			USDT:        0,
			Total:       0,
		}

		if currencyStats, exists := stats[account.ID]; exists {
			if usdc, ok := currencyStats["USDC"]; ok {
				accountStats.USDC = usdc
				accountStats.Total += usdc
				totalRecharges += usdc
			}
			if usdt, ok := currencyStats["USDT"]; ok {
				accountStats.USDT = usdt
				accountStats.Total += usdt
				totalRecharges += usdt
			}
		}

		accountStatistics[account.AccountType] = accountStats
	}

	return &model.RechargeStatistics{
		TotalRecharges:    totalRecharges,
		AccountStatistics: accountStatistics,
	}, nil
}
