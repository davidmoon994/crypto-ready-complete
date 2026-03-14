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
func (s *Service) Login(credential, password string) (*model.User, error) {
	// 先尝试用phone查询
	user, err := s.repo.GetUserByPhone(credential)
	if err != nil || user == nil {
		// 再尝试用username查询
		user, err = s.repo.GetUserByUsername(credential)
		if err != nil || user == nil {
			return nil, errors.New("用户不存在")
		}
	}

	// 验证密码
	passwordHash := hashPassword(password)
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
// CreateAPIUser 创建API用户（简化版：不需要API密钥）
func (s *Service) CreateAPIUser(username, password string) (int64, error) {
	// 1. 检查用户名是否已存在
	existingUser, err := s.repo.GetUserByUsername(username)
	if err != nil {
		return 0, err
	}
	if existingUser != nil {
		return 0, errors.New("该用户名已被使用")
	}

	// 2. 创建API用户（不关联Admin账户，initial_balance=0）
	passwordHash := hashPassword(password)
	userID, err := s.repo.CreateAPIUser(username, passwordHash, 0, 0) // admin_account_id=0表示未绑定
	if err != nil {
		return 0, fmt.Errorf("创建用户失败: %v", err)
	}

	fmt.Printf("✓ API用户创建成功: %s\n", username)
	return userID, nil
}

// AdminRecharge 管理员给用户充值（从系统账户划转份额）
func (s *Service) AdminRecharge(userID, adminAccountID int, amount float64, currency string) error {
	// 获取系统账户
	systemRecharge, err := s.repo.GetSystemRecharge(adminAccountID, currency)
	if err != nil {
		return fmt.Errorf("获取系统账户失败: %v", err)
	}
	if systemRecharge == nil {
		return errors.New("系统账户不存在")
	}

	// 🔥 简单模式：1:1分配份额
	purchaseShares := amount

	// 创建用户充值记录
	_, err = s.repo.CreateRechargeWithShares(
		userID,
		adminAccountID,
		amount,
		currency,
		0, // base_balance 不需要了
		purchaseShares,
	)
	if err != nil {
		return fmt.Errorf("创建充值记录失败: %v", err)
	}

	fmt.Printf("✓ 用户充值成功:\n")
	fmt.Printf("  用户ID: %d\n", userID)
	fmt.Printf("  充值金额: $%.2f %s\n", amount, currency)
	fmt.Printf("  获得份额: %.2f\n", purchaseShares)

	return nil
}

// AdminDepositToExchange Admin充值到交易所（按币种独立计算）
func (s *Service) AdminDepositToExchange(adminAccountID int, amount float64, currency string) error {
	if amount <= 0 {
		return errors.New("充值金额必须大于0")
	}

	adminAccount, err := s.repo.GetAdminAccountByID(adminAccountID)
	if err != nil {
		return err
	}
	if adminAccount == nil {
		return errors.New("Admin账户不存在")
	}

	fmt.Printf("\n💵 Admin充值到交易所:\n")
	fmt.Printf("  账户: %s\n", adminAccount.AccountType)
	fmt.Printf("  币种: %s\n", currency)
	fmt.Printf("  充值金额: $%.2f\n", amount)

	// 获取该币种的系统充值记录
	systemRecharge, err := s.repo.GetSystemRecharge(adminAccountID, currency)
	if err != nil {
		return fmt.Errorf("获取系统账户失败: %v", err)
	}

	// 获取该币种的总份额
	totalShares, err := s.repo.GetTotalSharesByCurrency(adminAccountID, currency)
	if err != nil {
		return fmt.Errorf("获取总份额失败: %v", err)
	}

	// 🔥 计算购买份额（简化：净值=1）
	// TODO: 未来可以改进为按实际汇率计算净值
	var netValue float64
	var purchasedShares float64

	if totalShares == 0 {
		// 该币种首次充值
		netValue = 1.0
		purchasedShares = amount
		fmt.Printf("\n  [%s首次充值] 净值: $1.00\n", currency)
	} else {
		// 该币种后续充值
		// 简化处理：假设稳定币净值=1
		netValue = 1.0
		purchasedShares = amount
		fmt.Printf("\n  [%s充值] 净值: $%.4f\n", currency, netValue)
	}

	fmt.Printf("  购买份额: %.4f\n", purchasedShares)

	// 创建或更新系统充值记录
	if systemRecharge == nil {
		// 创建新的系统充值记录
		rechargeID, err := s.repo.CreateRechargeWithShares(
			0, // user_id = 0
			adminAccountID,
			amount,
			currency,
			0,
			purchasedShares,
		)
		if err != nil {
			return fmt.Errorf("创建系统充值记录失败: %v", err)
		}
		fmt.Printf("\n  ✓ 创建%s系统充值记录 (ID: %d)\n", currency, rechargeID)
	} else {
		// 更新现有记录
		newAmount := systemRecharge.Amount + amount
		newShares := systemRecharge.Shares + purchasedShares

		err = s.repo.UpdateRechargeAmountAndShares(systemRecharge.ID, newAmount, newShares)
		if err != nil {
			return fmt.Errorf("更新系统充值记录失败: %v", err)
		}
		fmt.Printf("\n  ✓ 更新%s系统充值记录 (ID: %d)\n", currency, systemRecharge.ID)
		fmt.Printf("    累计充值: $%.2f → $%.2f\n", systemRecharge.Amount, newAmount)
		fmt.Printf("    累计份额: %.4f → %.4f\n", systemRecharge.Shares, newShares)
	}

	// 更新Admin账户总份额（所有币种之和）
	allShares, err := s.repo.GetAllSharesByAccount(adminAccountID)
	if err != nil {
		return fmt.Errorf("获取总份额失败: %v", err)
	}
	err = s.repo.UpdateAdminAccountShares(adminAccountID, allShares)
	if err != nil {
		return fmt.Errorf("更新总份额失败: %v", err)
	}

	fmt.Printf("  账户总份额更新为: %.4f\n", allShares)
	fmt.Printf("\n  ⚠️  请将 $%.2f %s 充值到 %s\n", amount, currency, adminAccount.AccountType)
	fmt.Println("  → 充值完成后点击「手动检查余额」")

	return nil
}

// UpdateRechargeAmount 修改充值金额
func (s *Service) UpdateRechargeAmount(rechargeID int, newAmount float64) error {
	// 1. 获取充值记录
	recharge, err := s.repo.GetRechargeByID(rechargeID)
	if err != nil {
		return err
	}

	// 2. 重新计算份额
	account, _ := s.repo.GetAdminAccountByID(recharge.AdminAccountID)
	currentBalance, _ := s.walletService.GetBalance(account)
	totalShares, _ := s.repo.GetTotalSharesByCurrency(recharge.AdminAccountID, recharge.Currency)

	netValue := currentBalance / totalShares
	newShares := newAmount / netValue

	// 3. 计算份额差异
	sharesDiff := newShares - recharge.Shares

	// 4. 从系统账户调整份额
	systemRecharge, _ := s.repo.GetSystemRecharge(recharge.AdminAccountID, recharge.Currency)
	if systemRecharge.Shares < sharesDiff {
		return errors.New("系统账户份额不足")
	}

	// 5. 更新
	err = s.repo.UpdateRechargeAmountAndShares(rechargeID, newAmount, newShares)
	if err != nil {
		return err
	}

	err = s.repo.UpdateRechargeShares(systemRecharge.ID, systemRecharge.Shares-sharesDiff)
	if err != nil {
		return err
	}

	return nil
}

// DeleteRecharge 删除充值记录（返还份额到系统账户）
func (s *Service) DeleteRecharge(rechargeID int) error {
	// 1. 获取充值记录
	recharge, err := s.repo.GetRechargeByID(rechargeID)
	if err != nil {
		return err
	}

	if recharge.UserID == 0 {
		return errors.New("不能删除系统账户")
	}

	// 2. 返还份额到系统账户
	systemRecharge, _ := s.repo.GetSystemRecharge(recharge.AdminAccountID, recharge.Currency)
	newSystemShares := systemRecharge.Shares + recharge.Shares

	err = s.repo.UpdateRechargeShares(systemRecharge.ID, newSystemShares)
	if err != nil {
		return err
	}

	// 3. 🔥 直接使用SQL更新，不调用DeactivateRecharge
	// err = s.repo.DeactivateRecharge(rechargeID)  // 删除这行
	// 改为：
	err = s.repo.UpdateRechargeActive(rechargeID, false)
	if err != nil {
		return err
	}

	fmt.Printf("✓ 充值记录已删除 (ID: %d)，份额已返还到系统账户\n", rechargeID)
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

// GetAllUsers 获取所有用户（含盈亏统计）
func (s *Service) GetAllUsers() ([]*model.UserSummary, error) {
	fmt.Println("📋 [GetAllUsers] 开始获取用户基本信息")

	// 🔥 获取所有用户（包括普通用户和API用户）
	users, err := s.repo.GetAllUsersBasic()
	if err != nil {
		fmt.Printf("❌ [GetAllUsers] GetAllUsersBasic 失败: %v\n", err)
		return nil, err
	}

	fmt.Printf("✓ [GetAllUsers] 从数据库获取到 %d 个用户\n", len(users))

	var result []*model.UserSummary
	for _, user := range users {
		// 🔥 跳过管理员账户（id <= 3）
		if user.ID <= 3 {
			continue
		}

		fmt.Printf("  计算用户 %d (%s) 的Dashboard数据...\n", user.ID, user.Phone)

		// 🔥 实时计算每个用户的Dashboard数据
		summary, err := s.GetDashboardSummary(user.ID)
		if err != nil {
			fmt.Printf("  ⚠️  获取用户%d统计失败: %v\n", user.ID, err)
			// 返回空数据
			result = append(result, &model.UserSummary{
				UserID:        user.ID,
				Phone:         user.Phone,
				IsActive:      user.IsActive,
				TotalRecharge: 0,
				CurrentValue:  0,
				TotalProfit:   0,
				RechargeCount: 0,
			})
			continue
		}

		fmt.Printf("  ✓ 用户%d: 充值=$%.2f, 当前=$%.2f, 盈亏=$%.2f\n",
			user.ID, summary.TotalRecharge, summary.CurrentValue, summary.TotalProfit)

		result = append(result, &model.UserSummary{
			UserID:        user.ID,
			Phone:         user.Phone,
			IsActive:      user.IsActive,
			TotalRecharge: summary.TotalRecharge,
			CurrentValue:  summary.CurrentValue,
			TotalProfit:   summary.TotalProfit,
			RechargeCount: summary.RechargeCount,
		})
	}

	fmt.Printf("✓ [GetAllUsers] 返回 %d 个用户汇总\n", len(result))
	return result, nil
}

// GetUserByID 获取用户详情
func (s *Service) GetUserByID(userID int) (*model.User, error) {
	return s.repo.GetUserByID(userID)
}

func (s *Service) GetDashboardSummary(userID int) (*model.DashboardSummary, error) {
	user, err := s.repo.GetUserByID(userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("用户不存在")
	}

	// 🔥 API用户：使用自己的API密钥查询余额
	if user.IsAPIUser {
		if user.APIKey == "" || user.APISecret == "" {
			return &model.DashboardSummary{
				TotalRecharge:   0,
				CurrentValue:    0,
				TotalProfit:     0,
				TotalProfitRate: 0,
				LastUpdateTime:  time.Now().Format("2006-01-02 15:04:05"),
			}, nil
		}

		userAccount := &model.AdminAccount{
			AccountType: user.APIType,
			APIKey:      user.APIKey,
			APISecret:   user.APISecret,
			Passphrase:  user.APIPassphrase,
		}

		var currency string
		if user.APIType == "Binance" {
			currency = "USDC"
		} else if user.APIType == "OKX" {
			currency = "USDT"
		} else {
			return nil, errors.New("不支持的API类型")
		}

		currentBalance, err := s.walletService.GetBalanceByAsset(userAccount, currency)
		if err != nil {
			return nil, fmt.Errorf("获取API账户%s余额失败: %v", currency, err)
		}

		totalProfit := currentBalance - user.InitialBalance
		profitRate := 0.0
		if user.InitialBalance > 0 {
			profitRate = (totalProfit / user.InitialBalance) * 100
		}

		holdDays := int(time.Since(user.CreatedAt).Hours() / 24)
		if holdDays < 1 {
			holdDays = 1
		}

		// 🔥 使用相同的盈亏率计算逻辑
		dailyRate := 0.0
		monthlyRate := 0.0
		quarterlyRate := 0.0
		annualRate := 0.0

		if holdDays > 0 && user.InitialBalance > 0 {
			// 日盈亏率 = 总盈亏 / 总充值(初始余额) / 持有天数
			dailyRate = (totalProfit / user.InitialBalance / float64(holdDays)) * 100
			monthlyRate = dailyRate * 30
			quarterlyRate = dailyRate * 90
			annualRate = dailyRate * 365
		}

		fmt.Printf("[API用户 %d] 初始余额=$%.2f, 当前余额=$%.2f, 盈亏=$%.2f (%.2f%%), 年化=%.2f%%\n",
			userID, user.InitialBalance, currentBalance, totalProfit, profitRate, annualRate)

		return &model.DashboardSummary{
			TotalRecharge:   user.InitialBalance,
			CurrentValue:    currentBalance,
			TotalProfit:     totalProfit,
			TotalProfitRate: profitRate,
			RechargeCount:   1,
			MonthlyRate:     monthlyRate,
			QuarterlyRate:   quarterlyRate,
			AnnualRate:      annualRate,
			AvgHoldDays:     holdDays,
			LastUpdateTime:  time.Now().Format("2006-01-02 15:04:05"),
		}, nil
	}

	// 🔥 普通用户：基于份额计算
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

		// 🔥 关键：基于份额计算当前价值
		adminAccount, err := s.repo.GetAdminAccountByID(r.AdminAccountID)
		if err != nil || adminAccount == nil {
			fmt.Printf("⚠️  充值ID %d: 无法获取账户信息\n", r.ID)
			totalCurrentValue += r.Amount
			continue
		}

		// 🔥 1:1份额模式：获取总充值金额
		fmt.Printf("🔍 [GetDashboardSummary] 调用GetTotalRechargeAmountByCurrency, adminAccountID=%d, currency=%s\n",
			r.AdminAccountID, r.Currency)

		totalRechargeAmount, err := s.repo.GetTotalRechargeAmountByCurrency(r.AdminAccountID, r.Currency)

		fmt.Printf("🔍 [GetDashboardSummary] GetTotalRechargeAmountByCurrency返回: totalRechargeAmount=%.2f, err=%v\n",
			totalRechargeAmount, err)

		if err != nil || totalRechargeAmount <= 0 {
			fmt.Printf("⚠️  充值ID %d: 无法获取总充值金额 (currency: %s), err=%v, totalRechargeAmount=%.2f\n",
				r.ID, r.Currency, err, totalRechargeAmount)
			totalCurrentValue += r.Amount
			continue
		}

		// 🔥 按币种获取余额
		currentBalance, err := s.walletService.GetBalanceByAsset(adminAccount, r.Currency)
		if err != nil {
			fmt.Printf("⚠️  充值ID %d: 无法获取余额\n", r.ID)
			totalCurrentValue += r.Amount
			continue
		}

		// 🔥 1:1份额模式：净值 = 当前余额 / 总充值金额
		netValue := currentBalance / totalRechargeAmount
		currentValue := r.Amount * netValue // 用户当前价值 = 用户充值 × 净值
		totalCurrentValue += currentValue

		fmt.Printf("  [充值ID %d] 币种=%s, 用户充值=$%.2f, 总充值=$%.2f, 余额=$%.2f, 净值=$%.4f, 当前价值=$%.2f\n",
			r.ID, r.Currency, r.Amount, totalRechargeAmount, currentBalance, netValue, currentValue)

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

	// 🔥 优化盈亏率计算逻辑
	dailyRate := 0.0
	monthlyRate := 0.0
	quarterlyRate := 0.0
	annualRate := 0.0

	if avgHoldDays > 0 && totalRecharge > 0 {
		// 日盈亏率 = 总盈亏 / 总充值 / 平均持有天数
		dailyRate = (totalProfit / totalRecharge / float64(avgHoldDays)) * 100
		monthlyRate = dailyRate * 30
		quarterlyRate = dailyRate * 90
		annualRate = dailyRate * 365
	}

	fmt.Printf("\n[Dashboard总览] 用户ID %d:\n", userID)
	fmt.Printf("  总充值: $%.2f\n", totalRecharge)
	fmt.Printf("  当前价值: $%.2f\n", totalCurrentValue)
	fmt.Printf("  总盈亏: $%.2f (%.2f%%)\n", totalProfit, totalProfitRate)
	fmt.Printf("  平均持有天数: %d天\n", avgHoldDays)
	fmt.Printf("  日盈亏率: %.4f%%\n", dailyRate)
	fmt.Printf("  月盈亏率: %.2f%%\n", monthlyRate)
	fmt.Printf("  季度盈亏率: %.2f%%\n", quarterlyRate)
	fmt.Printf("  年盈亏率: %.2f%%\n", annualRate)

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
		LastUpdateTime:  time.Now().Format("2006-01-02 15:04:05"),
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
		if !r.IsActive {
			continue
		}

		// 🔥 实时计算盈亏
		account, err := s.repo.GetAdminAccountByID(r.AdminAccountID)
		if err != nil || account == nil {
			fmt.Printf("⚠️  充值ID %d: 无法获取账户信息\n", r.ID)
			continue
		}

		// 获取总充值金额（shares > 0）
		totalRechargeAmount, err := s.repo.GetTotalRechargeAmountByCurrency(r.AdminAccountID, r.Currency)
		if err != nil || totalRechargeAmount <= 0 {
			fmt.Printf("⚠️  充值ID %d: 无法获取总充值金额\n", r.ID)
			continue
		}

		// 获取当前余额
		currentBalance, err := s.walletService.GetBalanceByAsset(account, r.Currency)
		if err != nil {
			fmt.Printf("⚠️  充值ID %d: 无法获取余额\n", r.ID)
			continue
		}

		// 🔥 计算净值和当前价值
		netValue := currentBalance / totalRechargeAmount
		currentValue := r.Amount * netValue
		currentProfit := currentValue - r.Amount
		profitRate := 0.0
		if r.Amount > 0 {
			profitRate = (currentProfit / r.Amount) * 100
		}

		// 计算持有天数
		daysHeld := int(time.Since(r.RechargeAt).Hours() / 24)
		if daysHeld < 1 {
			daysHeld = 1
		}

		fmt.Printf("  [充值记录] ID=%d, 金额=$%.2f, 净值=%.4f, 当前=$%.2f, 盈亏=$%.2f (%.2f%%)\n",
			r.ID, r.Amount, netValue, currentValue, currentProfit, profitRate)

		item := &model.RechargeWithProfit{
			Recharge:      r,
			AccountType:   account.AccountType,
			CurrentProfit: currentProfit,
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

// SaveUserAPIKeys 保存用户API密钥并验证
func (s *Service) SaveUserAPIKeys(userID int, apiType, apiKey, apiSecret, passphrase string) error {
	// 验证API密钥
	testAccount := &model.AdminAccount{
		AccountType: apiType,
		APIKey:      apiKey,
		APISecret:   apiSecret,
		Passphrase:  passphrase,
	}

	initialBalance, err := s.walletService.GetBalance(testAccount)
	if err != nil {
		return fmt.Errorf("API验证失败: %v", err)
	}

	// 保存API密钥和初始余额
	err = s.repo.UpdateUserAPIKeys(userID, apiType, apiKey, apiSecret, passphrase, initialBalance)
	if err != nil {
		return err
	}

	fmt.Printf("✓ 用户 %d API密钥保存成功，初始余额: $%.2f\n", userID, initialBalance)
	return nil
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

// GetAPIDashboardData 获取API用户Dashboard数据（同时返回USDC和USDT余额）
func (s *Service) GetAPIDashboardData(userID int) (*model.APIDashboardData, error) {
	user, err := s.repo.GetUserByID(userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("用户不存在")
	}

	if !user.IsAPIUser {
		return nil, errors.New("非API用户")
	}

	// 检查是否已设置API密钥
	if user.APIKey == "" || user.APISecret == "" {
		return &model.APIDashboardData{
			HasAPIKeys: false,
		}, nil
	}

	userAccount := &model.AdminAccount{
		AccountType: user.APIType,
		APIKey:      user.APIKey,
		APISecret:   user.APISecret,
		Passphrase:  user.APIPassphrase,
	}

	// 🔥 支持同时获取 USDC 和 USDT
	balances := make(map[string]float64)

	switch user.APIType {
	case "Binance":
		balances["USDC"], err = s.walletService.GetBalanceByAsset(userAccount, "USDC")
		if err != nil {
			return nil, fmt.Errorf("获取USDC余额失败: %v", err)
		}
		balances["USDT"], err = s.walletService.GetBalanceByAsset(userAccount, "USDT")
		if err != nil {
			balances["USDT"] = 0 // Binance可能没有USDT
		}
	case "OKX":
		balances["USDT"], err = s.walletService.GetBalanceByAsset(userAccount, "USDT")
		if err != nil {
			return nil, fmt.Errorf("获取USDT余额失败: %v", err)
		}
		balances["USDC"], err = s.walletService.GetBalanceByAsset(userAccount, "USDC")
		if err != nil {
			balances["USDC"] = 0 // OKX可能没有USDC
		}
	default:
		return nil, errors.New("不支持的API类型")
	}

	fmt.Printf("[API用户 %d] USDC=$%.2f, USDT=$%.2f\n", userID, balances["USDC"], balances["USDT"])

	totalBalance := balances["USDC"] + balances["USDT"]
	totalProfit := totalBalance - user.InitialBalance
	profitRate := 0.0
	if user.InitialBalance > 0 {
		profitRate = (totalProfit / user.InitialBalance) * 100
	}

	holdDays := int(time.Since(user.CreatedAt).Hours() / 24)
	if holdDays < 1 {
		holdDays = 1
	}

	dailyRate := 0.0
	monthlyRate := 0.0
	quarterlyRate := 0.0
	annualRate := 0.0
	if holdDays > 0 && profitRate != 0 {
		dailyRate = profitRate / float64(holdDays)
		monthlyRate = dailyRate * 30
		quarterlyRate = dailyRate * 90
		annualRate = dailyRate * 365
	}

	positions, _ := s.walletService.GetPositions(userAccount, 20)
	orders, _ := s.walletService.GetOrders(userAccount, 20)
	historyTrades, _ := s.walletService.GetHistoryTrades(userAccount, 50)

	return &model.APIDashboardData{
		HasAPIKeys: true,
		Summary: &model.DashboardSummary{
			TotalRecharge:   user.InitialBalance,
			CurrentValue:    totalBalance,
			TotalProfit:     totalProfit,
			TotalProfitRate: profitRate,
			MonthlyRate:     monthlyRate,
			QuarterlyRate:   quarterlyRate,
			AnnualRate:      annualRate,
			AvgHoldDays:     holdDays,
			LastUpdateTime:  time.Now().Format("2006-01-02 15:04:05"),
		},
		CurrentBalance: totalBalance,
		USDCBalance:    balances["USDC"],
		USDTBalance:    balances["USDT"],
		InitialBalance: user.InitialBalance,
		TotalProfit:    totalProfit,
		ProfitRate:     profitRate,
		Positions:      positions,
		Orders:         orders,
		HistoryTrades:  historyTrades,
		LastUpdateTime: time.Now().Format("2006-01-02 15:04:05"),
	}, nil
}

// UpdateAPIUserInitialBalance 更新API用户的初始余额
func (s *Service) UpdateAPIUserInitialBalance(userID int, initialBalance float64) error {
	return s.repo.UpdateUserInitialBalance(userID, initialBalance)
}
