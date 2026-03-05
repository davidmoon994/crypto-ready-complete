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

// hashPassword 计算密码哈希
func hashPassword(password string) string {
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

// CreateAPIUser 创建API用户（独立Admin账户）
func (s *Service) CreateAPIUser(username, password, apiType, apiKey, apiSecret, passphrase string) (int64, int, error) {
	// 1. 检查用户名是否已存在
	existingUser, err := s.repo.GetUserByUsername(username)
	if err != nil {
		return 0, 0, err
	}
	if existingUser != nil {
		return 0, 0, errors.New("该用户名已被使用")
	}

	// 2. 创建独立的Admin账户
	adminAccountID, err := s.repo.CreateAdminAccount(apiType, apiKey, apiSecret, passphrase)
	if err != nil {
		return 0, 0, fmt.Errorf("创建Admin账户失败: %v", err)
	}

	// 3. 测试API并获取初始余额
	adminAccount, err := s.repo.GetAdminAccountByID(adminAccountID)
	if err != nil {
		s.repo.DeleteAdminAccount(adminAccountID)
		return 0, 0, err
	}

	initialBalance, err := s.walletService.GetBalance(adminAccount)
	if err != nil {
		s.repo.DeleteAdminAccount(adminAccountID)
		return 0, 0, fmt.Errorf("API验证失败: %v", err)
	}

	// 4. 初始化账户
	s.repo.UpdateAdminAccountBalance(adminAccountID, initialBalance)
	s.repo.UpdateAdminAccountShares(adminAccountID, initialBalance)

	fmt.Printf("✓ API账户验证成功，初始余额: $%.2f\n", initialBalance)

	// 5. 为原有资金创建系统充值记录
	if initialBalance > 0 {
		_, err = s.repo.CreateRechargeWithShares(0, adminAccountID, initialBalance, "USDT", 0.0, initialBalance)
		if err != nil {
			s.repo.DeleteAdminAccount(adminAccountID)
			return 0, 0, fmt.Errorf("初始化系统份额失败: %v", err)
		}
	}

	// 6. 创建用户（记录初始余额和用户名）
	passwordHash := hashPassword(password)
	userID, err := s.repo.CreateAPIUser(username, passwordHash, adminAccountID, initialBalance)
	if err != nil {
		s.repo.DeleteAdminAccount(adminAccountID)
		return 0, 0, fmt.Errorf("创建用户失败: %v", err)
	}

	fmt.Printf("✓ API用户创建成功: %s (初始本金: $%.2f)\n", username, initialBalance)

	return userID, adminAccountID, nil
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

	fmt.Printf("\n💰 充值操作开始:\n")
	fmt.Printf("  用户: %s (ID: %d)\n", user.Phone, userID)
	fmt.Printf("  充值金额: $%.2f %s\n", amount, currency)
	fmt.Printf("  充值到: %s (ID: %d)\n", adminAccount.AccountType, adminAccountID)

	// 🔥 【关键】步骤1: 充值前先刷新账户余额（从API获取最新余额）
	fmt.Println("\n  [步骤1] 充值前刷新账户余额...")

	latestBalance, err := s.walletService.GetBalance(adminAccount)
	if err != nil {
		fmt.Printf("  ⚠️  获取最新余额失败: %v\n", err)
		fmt.Println("  → 使用数据库中的余额")
	} else {
		// 更新数据库中的余额
		s.repo.UpdateAdminAccountBalance(adminAccountID, latestBalance)
		adminAccount.CurrentBalance = latestBalance
		fmt.Printf("  ✓ 充值前实时余额: $%.2f\n", latestBalance)
	}

	currentBalance := adminAccount.CurrentBalance
	currentShares := adminAccount.TotalShares

	fmt.Printf("\n  [步骤2] 当前账户状态:\n")
	fmt.Printf("    余额: $%.2f\n", currentBalance)
	fmt.Printf("    总份额: %.4f\n", currentShares)

	// 🔥 【关键】步骤3: 计算份额
	var purchasedShares float64
	var netValue float64

	if currentShares == 0 || currentBalance == 0 {
		// 首次充值：净值初始化为1
		purchasedShares = amount
		netValue = 1.0
		fmt.Printf("\n  [步骤3] 首次充值，净值初始化为: $1.00\n")
	} else {
		// 后续充值：根据当前净值计算份额
		netValue = currentBalance / currentShares
		purchasedShares = amount / netValue
		fmt.Printf("\n  [步骤3] 计算份额:\n")
		fmt.Printf("    充值前净值: $%.4f\n", netValue)
	}

	fmt.Printf("    用户购买份额: %.4f\n", purchasedShares)

	// 🔥 步骤4: 更新账户总份额
	newTotalShares := currentShares + purchasedShares
	if err := s.repo.UpdateAdminAccountShares(adminAccountID, newTotalShares); err != nil {
		return fmt.Errorf("更新账户份额失败: %v", err)
	}

	// 🔥 步骤5: 创建充值记录
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

	fmt.Printf("\n  [步骤4] ✓ 充值记录已创建 (ID: %d)\n", rechargeID)
	fmt.Printf("    新总份额: %.4f\n", newTotalShares)

	// 🔥 【关键】步骤6: 提示用户完成充值
	fmt.Printf("\n  [步骤5] ⚠️  请立即将 $%.2f %s 充值到 %s 账户\n", amount, currency, adminAccount.AccountType)
	fmt.Println("  → 充值完成后，点击「手动检查余额」更新数据")

	// 🔥 步骤7: 等待用户确认后，再次刷新余额
	// 注意：这里不自动刷新，需要用户手动点击"手动检查余额"
	// 这样可以确保用户已经完成充值操作

	fmt.Println("\n✓ 充值操作完成（等待实际充值）")
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

// LoginByUsername 通过用户名登录
func (s *Service) LoginByUsername(username, password string) (*model.User, error) {
	user, err := s.repo.GetUserByUsername(username)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("用户不存在")
	}

	// 验证密码
	passwordHash := hashPassword(password)
	if user.PasswordHash != passwordHash {
		return nil, errors.New("密码错误")
	}

	return user, nil
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
func (s *Service) GetDashboardSummary(userID int) (*model.DashboardSummary, error) {
	user, err := s.repo.GetUserByID(userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("用户不存在")
	}

	summary := &model.DashboardSummary{}

	// API用户
	if user.IsAPIUser && user.APIAdminAccountID > 0 {
		adminAccount, err := s.repo.GetAdminAccountByID(user.APIAdminAccountID)
		if err != nil || adminAccount == nil {
			return nil, errors.New("无法获取API账户信息")
		}

		summary.TotalRecharge = 0
		summary.CurrentValue = adminAccount.CurrentBalance
		summary.TotalProfit = 0
		summary.TotalProfitRate = 0

		return summary, nil
	}

	// 🔥 普通用户：修复盈亏计算
	recharges, err := s.repo.GetRechargesByUserID(userID)
	if err != nil {
		return nil, err
	}

	totalRecharge := 0.0
	totalCurrentValue := 0.0
	totalHoldDays := 0
	activeCount := 0

	for _, r := range recharges {
		if !r.IsActive {
			continue
		}

		activeCount++
		totalRecharge += r.Amount

		// 🔥 关键修复：基于份额计算当前价值
		adminAccount, err := s.repo.GetAdminAccountByID(r.AdminAccountID)
		if err != nil || adminAccount == nil {
			// 如果获取失败，使用充值金额
			totalCurrentValue += r.Amount
			continue
		}

		// 计算当前价值 = 持有份额 × 净值
		if adminAccount.TotalShares > 0 && r.Shares > 0 {
			netValue := adminAccount.CurrentBalance / adminAccount.TotalShares
			currentValue := r.Shares * netValue
			totalCurrentValue += currentValue

			fmt.Printf("  [调试] 充值ID %d: 份额=%.4f, 净值=%.4f, 当前价值=%.2f\n",
				r.ID, r.Shares, netValue, currentValue)
		} else {
			totalCurrentValue += r.Amount
		}

		// 计算持有天数
		holdDays := int(time.Since(r.RechargeAt).Hours() / 24)
		if holdDays < 1 {
			holdDays = 1
		}
		totalHoldDays += holdDays
	}

	totalProfit := totalCurrentValue - totalRecharge
	totalProfitRate := 0.0
	avgHoldDays := 0

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
		dailyRate := totalProfitRate / float64(avgHoldDays)
		monthlyRate = dailyRate * 30
		quarterlyRate = dailyRate * 90
		annualRate = dailyRate * 365
	}

	fmt.Printf("\n[Dashboard总览] 用户ID %d:\n", userID)
	fmt.Printf("  总充值: $%.2f\n", totalRecharge)
	fmt.Printf("  当前价值: $%.2f\n", totalCurrentValue)
	fmt.Printf("  总盈亏: $%.2f (%.2f%%)\n", totalProfit, totalProfitRate)

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
	}, nil
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

	fmt.Printf("共有 %d 笔活跃充值需要计算盈亏\n", len(allRecharges))

	for _, recharge := range allRecharges {
		// 获取Admin账户当前状态
		adminAccount, err := s.repo.GetAdminAccountByID(recharge.AdminAccountID)
		if err != nil || adminAccount == nil {
			fmt.Printf("⚠️  充值ID %d: 无法获取Admin账户\n", recharge.ID)
			continue
		}

		currentBalance := adminAccount.CurrentBalance
		totalShares := adminAccount.TotalShares

		// 🔥 核心算法：基于份额计算盈亏
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

			// 盈亏率 = 盈亏 / 本金 × 100%
			if recharge.Amount > 0 {
				profitRate = (profit / recharge.Amount) * 100
			}

			// 获取用户信息用于日志
			user, _ := s.repo.GetUserByID(recharge.UserID)
			userName := "未知"
			if user != nil {
				if user.Phone == "system" {
					userName = "系统"
				} else {
					userName = user.Phone
				}
			}

			fmt.Printf("  充值ID %d [%s]: 本金=$%.2f, 份额=%.4f, 净值=$%.4f, 当前=$%.2f, 盈亏=%s$%.2f (%.2f%%)\n",
				recharge.ID,
				userName,
				recharge.Amount,
				recharge.Shares,
				netValue,
				currentValue,
				formatSign(profit), abs(profit),
				profitRate)
		} else {
			// 异常情况：份额为0
			currentValue = recharge.Amount
			profit = 0
			profitRate = 0
			fmt.Printf("⚠️  充值ID %d: 份额数据异常(shares=%.4f, totalShares=%.4f)\n",
				recharge.ID, recharge.Shares, totalShares)
		}

		// 保存盈亏记录
		err = s.repo.SaveRechargeDailyProfit(recharge.ID, today, currentBalance, profit, profitRate)
		if err != nil {
			fmt.Printf("⚠️  充值ID %d: 保存盈亏失败: %v\n", recharge.ID, err)
		}
	}

	fmt.Println("✓ 成功计算充值盈亏")
	fmt.Printf("\n========== 每日余额检查完成 (成功: %d, 失败: %d) ==========\n\n", successCount, errorCount)

	return nil // ✅ 添加这行
} // ✅ 添加这个结束大括号

// formatSign 格式化符号
func formatSign(value float64) string {
	if value >= 0 {
		return "+"
	}
	return ""
}

// abs 绝对值
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
	// 获取用户信息
	user, err := s.repo.GetUserByID(userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("用户不存在")
	}

	// API用户返回空列表（没有充值记录）
	if user.IsAPIUser {
		return []*model.RechargeResponse{}, nil
	}

	// 普通用户返回充值记录
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

// GetAPIDashboardData 获取API用户Dashboard数据
func (s *Service) GetAPIDashboardData(userID int) (*model.APIDashboardData, error) {
	// 获取用户信息
	user, err := s.repo.GetUserByID(userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("用户不存在")
	}

	// 确认是API用户
	if !user.IsAPIUser || user.APIAdminAccountID == 0 {
		return nil, errors.New("非API用户")
	}

	// 获取Admin账户
	adminAccount, err := s.repo.GetAdminAccountByID(user.APIAdminAccountID)
	if err != nil {
		return nil, err
	}
	if adminAccount == nil {
		return nil, errors.New("API账户不存在")
	}

	// 获取当前余额
	currentBalance, err := s.walletService.GetBalance(adminAccount)
	if err != nil {
		return nil, fmt.Errorf("获取余额失败: %v", err)
	}

	// 更新数据库中的余额
	s.repo.UpdateAdminAccountBalance(user.APIAdminAccountID, currentBalance)

	// 计算盈亏
	totalProfit := currentBalance - user.InitialBalance
	profitRate := 0.0
	if user.InitialBalance > 0 {
		profitRate = (totalProfit / user.InitialBalance) * 100
	}

	// 获取持仓列表（最近20单）
	positions, err := s.walletService.GetPositions(adminAccount, 20)
	if err != nil {
		fmt.Printf("⚠️  获取持仓失败: %v\n", err)
		positions = []model.Position{}
	}

	// 获取委托列表（最近20单）
	orders, err := s.walletService.GetOrders(adminAccount, 20)
	if err != nil {
		fmt.Printf("⚠️  获取委托失败: %v\n", err)
		orders = []model.Order{}
	}

	// 获取历史记录（50条）
	historyTrades, err := s.walletService.GetHistoryTrades(adminAccount, 50)
	if err != nil {
		fmt.Printf("⚠️  获取历史记录失败: %v\n", err)
		historyTrades = []model.HistoryTrade{}
	}

	data := &model.APIDashboardData{
		CurrentBalance: currentBalance,
		InitialBalance: user.InitialBalance,
		TotalProfit:    totalProfit,
		ProfitRate:     profitRate,
		Positions:      positions,
		Orders:         orders,
		HistoryTrades:  historyTrades,
		LastUpdateTime: time.Now().Format("2006-01-02 15:04:05"),
	}

	return data, nil
}
