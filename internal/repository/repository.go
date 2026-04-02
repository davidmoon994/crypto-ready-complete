package repository

import (
	"crypto-final/internal/model"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type Repository struct {
	db *sql.DB
}

type Record struct {
	CreatedAt time.Time
}

func NewRepository(dbPath string, adminPassword string) (*Repository, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	repo := &Repository{db: db}
	if err := repo.InitDB(adminPassword); err != nil {
		return nil, err
	}

	return repo, nil
}

func (r *Repository) InitDB(adminPassword string) error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		phone TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		is_admin BOOLEAN DEFAULT 0,
		is_active BOOLEAN DEFAULT 1,  -- 新增
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS admin_accounts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		account_type TEXT UNIQUE NOT NULL,
		api_key TEXT,
		api_secret TEXT,
		wallet_address TEXT,
		passphrase TEXT,           -- ← 新增
		current_balance REAL DEFAULT 0,
		total_shares REAL DEFAULT 0,  -- 新增：总份额数
		is_active BOOLEAN DEFAULT 1,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS admin_account_balances (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		admin_account_id INTEGER NOT NULL,
		record_date DATE NOT NULL,
		balance REAL NOT NULL,
		daily_change REAL DEFAULT 0,
		daily_change_rate REAL DEFAULT 0,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (admin_account_id) REFERENCES admin_accounts(id),
		UNIQUE(admin_account_id, record_date)
	);

	CREATE TABLE IF NOT EXISTS recharges (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		admin_account_id INTEGER NOT NULL,
		amount REAL NOT NULL,
		currency TEXT NOT NULL,
		recharge_at TIMESTAMP NOT NULL,
		base_balance REAL NOT NULL,
		shares REAL NOT NULL DEFAULT 0,  -- 新增：用户持有的份额数
		is_active BOOLEAN DEFAULT 1,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (user_id) REFERENCES users(id),
		FOREIGN KEY (admin_account_id) REFERENCES admin_accounts(id)
	);

	CREATE TABLE IF NOT EXISTS recharge_daily_profits (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		recharge_id INTEGER NOT NULL,
		record_date DATE NOT NULL,
		admin_account_balance REAL NOT NULL,
		profit REAL NOT NULL,
		profit_rate REAL NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (recharge_id) REFERENCES recharges(id),
		UNIQUE(recharge_id, record_date)
	);
	`

	_, err := r.db.Exec(schema)
	if err != nil {
		return err
	}

	// 创建默认管理员（使用环境变量密码）
	passwordHash := hashPassword(adminPassword)
	defaultAdmin := `
	INSERT OR IGNORE INTO users (id, phone, password_hash, is_admin)
	VALUES (1, 'admin', ?, 1);
	`
	_, err = r.db.Exec(defaultAdmin, passwordHash)

	// 初始化3个Admin账户
	accounts := `
	INSERT OR IGNORE INTO admin_accounts (id, account_type) VALUES (1, 'Binance');
	INSERT OR IGNORE INTO admin_accounts (id, account_type) VALUES (2, 'OKX');
	INSERT OR IGNORE INTO admin_accounts (id, account_type) VALUES (3, 'Wallet');
	`
	_, _ = r.db.Exec(accounts)

	return err
}

// hashPassword 计算密码哈希
func hashPassword(password string) string {
	hash := sha256.Sum256([]byte(password))
	return hex.EncodeToString(hash[:])
}

// User operations
func (r *Repository) CreateUser(phone, passwordHash string) (int64, error) {
	result, err := r.db.Exec(
		"INSERT INTO users (phone, password_hash, is_admin) VALUES (?, ?, 0)",
		phone, passwordHash,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (r *Repository) GetUserByPhone(phone string) (*model.User, error) {
	user := &model.User{}
	err := r.db.QueryRow(`
		SELECT id, 
		       COALESCE(phone, ''), 
		       COALESCE(username, ''), 
		       password_hash, 
		       is_admin, 
		       COALESCE(is_active, 1),
		       COALESCE(is_api_user, 0),
		       COALESCE(api_admin_account_id, 0),
		       COALESCE(initial_balance, 0),
		       COALESCE(api_type, ''),
		       COALESCE(api_key, ''),
		       COALESCE(api_secret, ''),
		       COALESCE(api_passphrase, ''),
		       created_at 
		FROM users 
		WHERE phone = ?`,
		phone,
	).Scan(
		&user.ID,
		&user.Phone,
		&user.Username,
		&user.PasswordHash,
		&user.IsAdmin,
		&user.IsActive,
		&user.IsAPIUser,
		&user.APIAdminAccountID,
		&user.InitialBalance,
		&user.APIType,
		&user.APIKey,
		&user.APISecret,
		&user.APIPassphrase,
		&user.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	return user, err
}

// GetAllUsers 获取所有用户（含盈亏统计）
func (r *Repository) GetAllUsers() ([]*model.UserSummary, error) {
	rows, err := r.db.Query(`
		SELECT 
			u.id,
			COALESCE(u.phone, u.username, '未命名') as display_name,
			COALESCE(u.is_active, 1) as is_active,
			COALESCE(u.is_api_user, 0) as is_api_user,
			COALESCE(SUM(CASE WHEN r.is_active = 1 THEN r.amount ELSE 0 END), 0) as total_recharge,
			COUNT(CASE WHEN r.is_active = 1 THEN r.id END) as recharge_count
		FROM users u
		LEFT JOIN recharges r ON u.id = r.user_id
		WHERE u.is_admin = 0
		GROUP BY u.id
		ORDER BY u.id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*model.UserSummary
	for rows.Next() {
		user := &model.UserSummary{}
		var isAPIUser int
		var isActive int

		err := rows.Scan(
			&user.UserID,
			&user.Phone,
			&isActive,
			&isAPIUser,
			&user.TotalRecharge,
			&user.RechargeCount,
		)
		if err != nil {
			return nil, err
		}

		user.IsActive = (isActive == 1)

		// API用户不计算盈亏
		if isAPIUser == 1 {
			user.CurrentValue = 0
			user.TotalProfit = 0
		} else {
			// 普通用户计算盈亏
			currentValue := user.TotalRecharge
			recharges, _ := r.GetRechargesByUserID(user.UserID)
			for _, rech := range recharges {
				if !rech.IsActive {
					continue
				}
				latestProfit, _ := r.GetLatestRechargeProfit(rech.ID)
				if latestProfit != nil {
					currentValue += latestProfit.Profit
				}
			}
			user.CurrentValue = currentValue
			user.TotalProfit = currentValue - user.TotalRecharge
		}

		users = append(users, user)
	}

	return users, nil
}

func (r *Repository) GetAllDashboardUsers() ([]*model.User, error) {
	rows, err := r.db.Query(
		"SELECT id, phone, created_at FROM users WHERE is_admin = 0 ORDER BY created_at DESC",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*model.User
	for rows.Next() {
		u := &model.User{}
		err := rows.Scan(&u.ID, &u.Phone, &u.CreatedAt)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}

// UpdateUserAPIKeys 更新用户API密钥
func (r *Repository) UpdateUserAPIKeys(userID int, apiType, apiKey, apiSecret, passphrase string, initialBalance float64) error {
	_, err := r.db.Exec(`
		UPDATE users 
		SET api_type = ?, 
		    api_key = ?, 
		    api_secret = ?, 
		    api_passphrase = ?,
		    initial_balance = ?
		WHERE id = ?`,
		apiType, apiKey, apiSecret, passphrase, initialBalance, userID,
	)
	return err
}

// AdminAccount operations
func (r *Repository) GetAdminAccountByID(id int) (*model.AdminAccount, error) {
	acc := &model.AdminAccount{}
	var apiKey, apiSecret, walletAddress, passphrase sql.NullString

	err := r.db.QueryRow(
		`SELECT id, account_type, api_key, api_secret, wallet_address, passphrase, 
		        current_balance, COALESCE(total_shares, 0), is_active, updated_at
		 FROM admin_accounts WHERE id = ?`,
		id,
	).Scan(&acc.ID, &acc.AccountType, &apiKey, &apiSecret,
		&walletAddress, &passphrase, &acc.CurrentBalance, &acc.TotalShares,
		&acc.IsActive, &acc.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// 转换 NullString 为 string
	acc.APIKey = apiKey.String
	acc.APISecret = apiSecret.String
	acc.WalletAddress = walletAddress.String
	acc.Passphrase = passphrase.String

	return acc, nil
}
func (r *Repository) GetAdminAccountByType(accountType string) (*model.AdminAccount, error) {
	acc := &model.AdminAccount{}
	var apiKey, apiSecret, walletAddress, passphrase sql.NullString

	err := r.db.QueryRow(
		`SELECT id, account_type, api_key, api_secret, wallet_address, passphrase, 
		        current_balance, COALESCE(total_shares, 0), is_active, updated_at
		 FROM admin_accounts WHERE account_type = ?`,
		accountType,
	).Scan(&acc.ID, &acc.AccountType, &apiKey, &apiSecret,
		&walletAddress, &passphrase, &acc.CurrentBalance, &acc.TotalShares,
		&acc.IsActive, &acc.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// 转换 NullString 为 string
	acc.APIKey = apiKey.String
	acc.APISecret = apiSecret.String
	acc.WalletAddress = walletAddress.String
	acc.Passphrase = passphrase.String

	return acc, nil
}

func (r *Repository) GetAllAdminAccounts() ([]*model.AdminAccount, error) {
	rows, err := r.db.Query(
		`SELECT id, account_type, api_key, api_secret, wallet_address, passphrase, 
		        current_balance, COALESCE(total_shares, 0), is_active, updated_at
		 FROM admin_accounts ORDER BY id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []*model.AdminAccount
	for rows.Next() {
		acc := &model.AdminAccount{}
		var apiKey, apiSecret, walletAddress, passphrase sql.NullString

		err := rows.Scan(&acc.ID, &acc.AccountType, &apiKey, &apiSecret,
			&walletAddress, &passphrase, &acc.CurrentBalance, &acc.TotalShares,
			&acc.IsActive, &acc.UpdatedAt)
		if err != nil {
			return nil, err
		}

		// 转换 NullString 为 string
		acc.APIKey = apiKey.String
		acc.APISecret = apiSecret.String
		acc.WalletAddress = walletAddress.String
		acc.Passphrase = passphrase.String

		accounts = append(accounts, acc)
	}
	return accounts, nil
}

func (r *Repository) UpdateAdminAccountConfig(accountType, apiKey, apiSecret, walletAddress, passphrase string) error {
	_, err := r.db.Exec(
		`UPDATE admin_accounts 
		 SET api_key=?, api_secret=?, wallet_address=?, passphrase=?, is_active=1, updated_at=CURRENT_TIMESTAMP
		 WHERE account_type=?`,
		apiKey, apiSecret, walletAddress, passphrase, accountType,
	)
	return err
}

// GetAllUsersBasic 获取所有普通用户和API用户的基本信息（排除admin账户）
func (r *Repository) GetAllUsersBasic() ([]*model.User, error) {
	rows, err := r.db.Query(`
		SELECT id, 
		       COALESCE(phone, '') as phone, 
		       COALESCE(username, '') as username, 
		       is_active, 
		       is_api_user, 
		       created_at
		FROM users
		WHERE id > 1
		ORDER BY is_api_user, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*model.User
	for rows.Next() {
		var user model.User
		err := rows.Scan(
			&user.ID,
			&user.Phone,
			&user.Username,
			&user.IsActive,
			&user.IsAPIUser,
			&user.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		users = append(users, &user)
	}

	return users, nil
}
func (r *Repository) UpdateAdminAccountBalance(id int, balance float64) error {
	_, err := r.db.Exec(
		"UPDATE admin_accounts SET current_balance=?, updated_at=CURRENT_TIMESTAMP WHERE id=?",
		balance, id,
	)
	return err
}

// UpdateRechargeActive 更新充值记录活跃状态
func (r *Repository) UpdateRechargeActive(rechargeID int, isActive bool) error {
	activeValue := 0
	if isActive {
		activeValue = 1
	}

	_, err := r.db.Exec(`
		UPDATE recharges 
		SET is_active = ? 
		WHERE id = ?`,
		activeValue, rechargeID,
	)
	return err
}

// AdminAccountBalance operations
func (r *Repository) SaveAdminAccountBalance(accountID int, date string, balance, change, changeRate float64) error {
	_, err := r.db.Exec(
		`INSERT INTO admin_account_balances (admin_account_id, record_date, balance, daily_change, daily_change_rate)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(admin_account_id, record_date) 
		 DO UPDATE SET balance=?, daily_change=?, daily_change_rate=?`,
		accountID, date, balance, change, changeRate,
		balance, change, changeRate,
	)
	return err
}

func (r *Repository) GetLatestAdminAccountBalance(accountID int) (float64, error) {
	var balance float64
	err := r.db.QueryRow(
		`SELECT balance FROM admin_account_balances 
		 WHERE admin_account_id=? ORDER BY record_date DESC LIMIT 1`,
		accountID,
	).Scan(&balance)

	if err == sql.ErrNoRows {
		return 0, nil
	}
	return balance, err
}

// GetRechargeStatistics 获取充值统计（按账户和币种）
func (r *Repository) GetRechargeStatistics() (map[int]map[string]float64, error) {
	rows, err := r.db.Query(`
		SELECT admin_account_id, currency, SUM(amount) as total
		FROM recharges
		WHERE is_active = 1 AND shares > 0
		GROUP BY admin_account_id, currency
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// 结构: map[accountID]map[currency]total
	stats := make(map[int]map[string]float64)

	for rows.Next() {
		var accountID int
		var currency string
		var total float64

		if err := rows.Scan(&accountID, &currency, &total); err != nil {
			return nil, err
		}

		if stats[accountID] == nil {
			stats[accountID] = make(map[string]float64)
		}
		stats[accountID][currency] = total
	}

	return stats, nil
}

func (r *Repository) GetAdminAccountBalanceByDate(accountID int, date string) (float64, error) {
	var balance float64
	err := r.db.QueryRow(
		"SELECT balance FROM admin_account_balances WHERE admin_account_id=? AND record_date=?",
		accountID, date,
	).Scan(&balance)

	if err == sql.ErrNoRows {
		return 0, nil
	}
	return balance, err
}

func (r *Repository) GetTodayAdminAccountChange(accountID int, today string) (float64, float64, error) {
	var change, changeRate float64
	err := r.db.QueryRow(
		"SELECT daily_change, daily_change_rate FROM admin_account_balances WHERE admin_account_id=? AND record_date=?",
		accountID, today,
	).Scan(&change, &changeRate)

	if err == sql.ErrNoRows {
		return 0, 0, nil
	}
	return change, changeRate, err
}

// Recharge operations
func (r *Repository) CreateRecharge(recharge *model.Recharge) (int64, error) {
	result, err := r.db.Exec(
		`INSERT INTO recharges (user_id, admin_account_id, amount, currency, recharge_at, base_balance, is_active)
		 VALUES (?, ?, ?, ?, ?, ?, 1)`,
		recharge.UserID, recharge.AdminAccountID, recharge.Amount,
		recharge.Currency, recharge.RechargeAt, recharge.BaseBalance,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// CreateAdminAccount 创建新的Admin账户
func (r *Repository) CreateAdminAccount(accountType, apiKey, apiSecret, passphrase string) (int, error) {
	result, err := r.db.Exec(
		`INSERT INTO admin_accounts (account_type, api_key, api_secret, passphrase, wallet_address, current_balance, total_shares, is_active, updated_at)
		 VALUES (?, ?, ?, ?, '', 0, 0, 1, CURRENT_TIMESTAMP)`,
		accountType, apiKey, apiSecret, passphrase,
	)
	if err != nil {
		return 0, err
	}

	id, err := result.LastInsertId()
	return int(id), err
}

// DeleteAdminAccount 删除Admin账户（用于回滚）
func (r *Repository) DeleteAdminAccount(accountID int) error {
	_, err := r.db.Exec("DELETE FROM admin_accounts WHERE id = ?", accountID)
	return err
}

// CreateAPIUser 创建API用户（使用username）
func (r *Repository) CreateAPIUser(username, passwordHash string, adminAccountID int, initialBalance float64) (int64, error) {
	result, err := r.db.Exec(
		`INSERT INTO users (username, password_hash, is_admin, is_active, is_api_user, api_admin_account_id, initial_balance)
		 VALUES (?, ?, 0, 1, 1, ?, ?)`,
		username, passwordHash, adminAccountID, initialBalance,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// GetUserByUsername 通过用户名获取用户
func (r *Repository) GetUserByUsername(username string) (*model.User, error) {
	user := &model.User{}
	err := r.db.QueryRow(`
		SELECT id, 
		       COALESCE(phone, ''), 
		       COALESCE(username, ''), 
		       password_hash, 
		       is_admin, 
		       COALESCE(is_active, 1),
		       COALESCE(is_api_user, 0),
		       COALESCE(api_admin_account_id, 0),
		       COALESCE(initial_balance, 0),
		       COALESCE(api_type, ''),
		       COALESCE(api_key, ''),
		       COALESCE(api_secret, ''),
		       COALESCE(api_passphrase, ''),
		       created_at 
		FROM users 
		WHERE username = ?`,
		username,
	).Scan(
		&user.ID,
		&user.Phone,
		&user.Username,
		&user.PasswordHash,
		&user.IsAdmin,
		&user.IsActive,
		&user.IsAPIUser,
		&user.APIAdminAccountID,
		&user.InitialBalance,
		&user.APIType,
		&user.APIKey,        // 🔥 添加
		&user.APISecret,     // 🔥 添加
		&user.APIPassphrase, // 🔥 添加
		&user.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	return user, err
}

// GetSystemRecharge 获取系统充值记录（按账户和币种）
func (r *Repository) GetSystemRecharge(adminAccountID int, currency string) (*model.Recharge, error) {
	recharge := &model.Recharge{}
	err := r.db.QueryRow(`
		SELECT id, user_id, admin_account_id, amount, currency, 
		       COALESCE(base_balance, 0), COALESCE(shares, 0),
		       recharge_at, is_active
		FROM recharges 
		WHERE user_id = 0 
		  AND admin_account_id = ? 
		  AND currency = ?
		  AND is_active = 1
		LIMIT 1`,
		adminAccountID, currency,
	).Scan(
		&recharge.ID,
		&recharge.UserID,
		&recharge.AdminAccountID,
		&recharge.Amount,
		&recharge.Currency,
		&recharge.BaseBalance,
		&recharge.Shares,
		&recharge.RechargeAt,
		&recharge.IsActive,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return recharge, nil
}

// GetSystemSharesByCurrency 获取系统账户某币种的总份额
func (r *Repository) GetSystemSharesByCurrency(adminAccountID int, currency string) (float64, error) {
	var shares float64
	err := r.db.QueryRow(`
		SELECT COALESCE(SUM(shares), 0)
		FROM recharges
		WHERE user_id = 0 
		  AND admin_account_id = ?
		  AND currency = ?
		  AND is_active = 1`,
		adminAccountID, currency,
	).Scan(&shares)

	return shares, err
}

// GetUserSharesByCurrency 获取用户在某账户某币种的总份额
func (r *Repository) GetUserSharesByCurrency(userID, adminAccountID int, currency string) (float64, error) {
	var shares float64
	err := r.db.QueryRow(`
		SELECT COALESCE(SUM(shares), 0)
		FROM recharges
		WHERE user_id = ?
		  AND admin_account_id = ?
		  AND currency = ?
		  AND is_active = 1`,
		userID, adminAccountID, currency,
	).Scan(&shares)

	return shares, err
}

// GetTotalSharesByCurrency 获取某个账户某个币种的总份额
func (r *Repository) GetTotalSharesByCurrency(adminAccountID int, currency string) (float64, error) {
	var totalShares float64
	err := r.db.QueryRow(`
		SELECT COALESCE(SUM(shares), 0)
		FROM recharges
		WHERE admin_account_id = ? 
		  AND currency = ? 
		  AND is_active = 1`,
		adminAccountID, currency,
	).Scan(&totalShares)

	return totalShares, err
}

// GetAllSharesByAccount 获取某账户所有币种的总份额
func (r *Repository) GetAllSharesByAccount(adminAccountID int) (float64, error) {
	var shares float64
	err := r.db.QueryRow(`
		SELECT COALESCE(SUM(shares), 0)
		FROM recharges
		WHERE admin_account_id = ?
		  AND is_active = 1`,
		adminAccountID,
	).Scan(&shares)

	return shares, err
}

// UpdateRechargeShares 更新充值记录的份额
func (r *Repository) UpdateRechargeShares(rechargeID int, shares float64) error {
	_, err := r.db.Exec(
		"UPDATE recharges SET shares = ? WHERE id = ?",
		shares, rechargeID,
	)
	return err
}

// UpdateRechargeAmountAndShares 更新充值记录的金额和份额
func (r *Repository) UpdateRechargeAmountAndShares(rechargeID int, amount, shares float64) error {
	_, err := r.db.Exec(
		"UPDATE recharges SET amount = ?, shares = ? WHERE id = ?",
		amount, shares, rechargeID,
	)
	return err
}

func (r *Repository) GetRechargesByUserID(userID int) ([]*model.Recharge, error) {
	rows, err := r.db.Query(
		`SELECT id, user_id, admin_account_id, amount, currency, recharge_at, 
		        COALESCE(base_balance, 0), COALESCE(shares, 0), is_active, created_at
		 FROM recharges WHERE user_id=? ORDER BY recharge_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var recharges []*model.Recharge
	for rows.Next() {
		r := &model.Recharge{}
		err := rows.Scan(&r.ID, &r.UserID, &r.AdminAccountID, &r.Amount, &r.Currency,
			&r.RechargeAt, &r.BaseBalance, &r.Shares, &r.IsActive, &r.CreatedAt)
		if err != nil {
			return nil, err
		}
		recharges = append(recharges, r)
	}
	return recharges, nil
}

func (r *Repository) GetAllActiveRecharges() ([]*model.Recharge, error) {
	rows, err := r.db.Query(`
		SELECT id, user_id, admin_account_id, amount, currency, 
		       COALESCE(base_balance, 0), COALESCE(shares, 0), recharge_at, is_active
		FROM recharges
		WHERE is_active = 1
		ORDER BY recharge_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var recharges []*model.Recharge
	for rows.Next() {
		r := &model.Recharge{}
		err := rows.Scan(
			&r.ID,
			&r.UserID,
			&r.AdminAccountID,
			&r.Amount,
			&r.Currency,
			&r.BaseBalance,
			&r.Shares,
			&r.RechargeAt,
			&r.IsActive,
		)
		if err != nil {
			return nil, err
		}
		recharges = append(recharges, r)
	}

	return recharges, nil
}

// GetRechargeByID 获取充值记录
func (r *Repository) GetRechargeByID(rechargeID int) (*model.Recharge, error) {
	recharge := &model.Recharge{}
	err := r.db.QueryRow(`
		SELECT id, user_id, admin_account_id, amount, currency, 
		       base_balance, shares, recharge_at, is_active
		FROM recharges
		WHERE id = ?`,
		rechargeID,
	).Scan(
		&recharge.ID,
		&recharge.UserID,
		&recharge.AdminAccountID,
		&recharge.Amount,
		&recharge.Currency,
		&recharge.BaseBalance,
		&recharge.Shares,
		&recharge.RechargeAt,
		&recharge.IsActive,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	return recharge, err
}

// UpdateUserStatus 更新用户状态
func (r *Repository) UpdateUserStatus(userID int, isActive bool) error {
	_, err := r.db.Exec(
		"UPDATE users SET is_active = ? WHERE id = ? AND is_admin = 0",
		isActive, userID,
	)
	return err
}

// GetUserByID 获取用户信息（含is_active）
func (r *Repository) GetUserByID(userID int) (*model.User, error) {
	user := &model.User{}
	err := r.db.QueryRow(`
		SELECT id, 
		       COALESCE(phone, ''),
		       COALESCE(username, ''),
		       password_hash, 
		       is_admin, 
		       COALESCE(is_active, 1), 
		       COALESCE(is_api_user, 0), 
		       COALESCE(api_admin_account_id, 0),
		       COALESCE(initial_balance, 0),
		       COALESCE(api_type, ''),
		       COALESCE(api_key, ''),
		       COALESCE(api_secret, ''),
		       COALESCE(api_passphrase, ''),
		       created_at 
		FROM users 
		WHERE id = ?`,
		userID,
	).Scan(
		&user.ID,
		&user.Phone,
		&user.Username, // 新增
		&user.PasswordHash,
		&user.IsAdmin,
		&user.IsActive,
		&user.IsAPIUser,
		&user.APIAdminAccountID,
		&user.InitialBalance,
		&user.APIType,       // 新增
		&user.APIKey,        // 新增
		&user.APISecret,     // 新增
		&user.APIPassphrase, // 新增
		&user.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return user, err
}

// DeleteRecharge 删除充值记录
func (r *Repository) DeleteRecharge(rechargeID int) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 删除充值的每日盈亏记录
	_, err = tx.Exec("DELETE FROM recharge_daily_profits WHERE recharge_id = ?", rechargeID)
	if err != nil {
		return err
	}

	// 删除充值记录
	_, err = tx.Exec("DELETE FROM recharges WHERE id = ?", rechargeID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// UpdateRechargeStatus 更新充值状态（软删除）
func (r *Repository) UpdateRechargeStatus(rechargeID int, isActive bool) error {
	_, err := r.db.Exec(
		"UPDATE recharges SET is_active = ? WHERE id = ?",
		isActive, rechargeID,
	)
	return err
}

// RechargeDailyProfit operations
func (r *Repository) SaveRechargeDailyProfit(rechargeID int, date string, accountBalance, profit, profitRate float64) error {
	_, err := r.db.Exec(
		`INSERT INTO recharge_daily_profits (recharge_id, record_date, admin_account_balance, profit, profit_rate)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(recharge_id, record_date)
		 DO UPDATE SET admin_account_balance=?, profit=?, profit_rate=?`,
		rechargeID, date, accountBalance, profit, profitRate,
		accountBalance, profit, profitRate,
	)
	return err
}

func (r *Repository) GetLatestRechargeProfit(rechargeID int) (*model.RechargeDailyProfit, error) {
	p := &model.RechargeDailyProfit{}
	err := r.db.QueryRow(
		`SELECT id, recharge_id, record_date, admin_account_balance, profit, profit_rate, created_at
		 FROM recharge_daily_profits WHERE recharge_id=? ORDER BY record_date DESC LIMIT 1`,
		rechargeID,
	).Scan(&p.ID, &p.RechargeID, &p.RecordDate, &p.AdminAccountBalance,
		&p.Profit, &p.ProfitRate, &p.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	return p, err
}

func (r *Repository) GetRechargeProfitHistory(rechargeID int) ([]*model.RechargeDailyProfit, error) {
	rows, err := r.db.Query(
		`SELECT id, recharge_id, record_date, admin_account_balance, profit, profit_rate, created_at
		 FROM recharge_daily_profits WHERE recharge_id=? ORDER BY record_date DESC`,
		rechargeID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var profits []*model.RechargeDailyProfit
	for rows.Next() {
		p := &model.RechargeDailyProfit{}
		err := rows.Scan(&p.ID, &p.RechargeID, &p.RecordDate, &p.AdminAccountBalance,
			&p.Profit, &p.ProfitRate, &p.CreatedAt)
		if err != nil {
			return nil, err
		}
		profits = append(profits, p)
	}
	return profits, nil
}

func (r *Repository) Close() error {
	return r.db.Close()
}

// UpdateAdminAccountShares 更新Admin账户总份额
func (r *Repository) UpdateAdminAccountShares(accountID int, totalShares float64) error {
	_, err := r.db.Exec(
		"UPDATE admin_accounts SET total_shares = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		totalShares, accountID,
	)
	return err
}

// CreateRechargeWithShares 创建充值记录（含份额）
func (r *Repository) CreateRechargeWithShares(userID, adminAccountID int, amount float64, currency string, baseBalance, shares float64) (int64, error) {
	result, err := r.db.Exec(
		`INSERT INTO recharges (user_id, admin_account_id, amount, currency, base_balance, shares, recharge_at, is_active)
         VALUES (?, ?, ?, ?, ?, ?, datetime('now'), 1)`,
		userID, adminAccountID, amount, currency, baseBalance, shares,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// UpdateUserInitialBalance 更新用户初始余额
func (r *Repository) UpdateUserInitialBalance(userID int, initialBalance float64) error {
	_, err := r.db.Exec(`
		UPDATE users 
		SET initial_balance = ? 
		WHERE id = ? AND is_api_user = 1`,
		initialBalance, userID,
	)
	return err
}

// GetTotalRechargeAmountByCurrency 获取某个账户某个币种的总充值金额（不包括系统充值）
func (r *Repository) GetTotalRechargeAmountByCurrency(adminAccountID int, currency string) (float64, error) {
	var totalAmount float64
	err := r.db.QueryRow(`
		SELECT COALESCE(SUM(amount), 0)
		FROM recharges
		WHERE admin_account_id = ? 
		  AND currency = ?
		  AND is_active = 1
		  AND user_id > 0
	`, adminAccountID, currency).Scan(&totalAmount)
	
	return totalAmount, err
}

// SaveMilestone 保存充值里程碑快照
func (r *Repository) SaveMilestone(rechargeID, userID int, milestoneType string, daysHeld int, amount, currentValue, profit, profitRate, netValue float64) error {
	milestoneDate := time.Now().Format("2006-01-02")

	_, err := r.db.Exec(`
		INSERT OR REPLACE INTO recharge_milestones 
		(recharge_id, user_id, milestone_type, milestone_date, days_held, amount, current_value, profit, profit_rate, net_value)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, rechargeID, userID, milestoneType, milestoneDate, daysHeld, amount, currentValue, profit, profitRate, netValue)

	return err
}

// GetMilestone 获取充值的里程碑快照
func (r *Repository) GetMilestone(rechargeID int, milestoneType string) (map[string]interface{}, error) {
	var milestone map[string]interface{}
	var daysHeld int
	var amount, currentValue, profit, profitRate, netValue float64
	var milestoneDate string

	err := r.db.QueryRow(`
		SELECT milestone_date, days_held, amount, current_value, profit, profit_rate, net_value
		FROM recharge_milestones
		WHERE recharge_id = ? AND milestone_type = ?
	`, rechargeID, milestoneType).Scan(&milestoneDate, &daysHeld, &amount, &currentValue, &profit, &profitRate, &netValue)

	if err != nil {
		return nil, err
	}

	milestone = map[string]interface{}{
		"milestone_date": milestoneDate,
		"days_held":      daysHeld,
		"amount":         amount,
		"current_value":  currentValue,
		"profit":         profit,
		"profit_rate":    profitRate,
		"net_value":      netValue,
	}

	return milestone, nil
}

// RecordWithdrawal 记录全部撤资
func (r *Repository) RecordWithdrawal(rechargeID, userID int, originalAmount, withdrawnAmount, finalProfit, finalProfitRate float64, daysHeld int) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	
	// 1. 记录撤资
	_, err = tx.Exec(`
		INSERT INTO withdrawals 
		(recharge_id, user_id, original_amount, withdrawn_amount, final_profit, final_profit_rate, days_held, withdrawal_type, remaining_amount)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'full', 0)
	`, rechargeID, userID, originalAmount, withdrawnAmount, finalProfit, finalProfitRate, daysHeld)
	
	if err != nil {
		return err
	}
	
	// 2. 停用充值记录
	_, err = tx.Exec(`
		UPDATE recharges 
		SET is_active = 0 
		WHERE id = ?
	`, rechargeID)
	
	if err != nil {
		return err
	}
	
	return tx.Commit()
}

// RecordPartialWithdrawal 记录部分撤资
func (r *Repository) RecordPartialWithdrawal(
	originalRechargeID, userID int,
	withdrawPrincipal, withdrawAmount, withdrawProfit, withdrawProfitRate float64,
	remainingPrincipal, remainingValue float64,
	daysHeld int,
	originalRechargeAt time.Time,
	adminAccountID int,
	currency string,
) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	
	// 1. 停用原充值记录
	_, err = tx.Exec(`
		UPDATE recharges 
		SET is_active = 0 
		WHERE id = ?
	`, originalRechargeID)
	
	if err != nil {
		return err
	}
	
	// 2. 创建新的充值记录（剩余部分，继续持有）
	result, err := tx.Exec(`
		INSERT INTO recharges 
		(user_id, admin_account_id, amount, currency, recharge_at, base_balance, is_active)
		VALUES (?, ?, ?, ?, ?, ?, 1)
	`, userID, adminAccountID, remainingPrincipal, currency, originalRechargeAt, 0)
	
	if err != nil {
		return err
	}
	
	newRechargeID, _ := result.LastInsertId()
	
	// 3. 复制原充值的月度快照到新充值记录
	_, err = tx.Exec(`
		INSERT INTO recharge_monthly_snapshots 
		(recharge_id, user_id, snapshot_date, period_number, days_in_period, amount, start_value, end_value, period_profit, period_profit_rate, net_value, created_at)
		SELECT 
			? as recharge_id,
			user_id,
			snapshot_date,
			period_number,
			days_in_period,
			? as amount,
			start_value * ?,
			end_value * ?,
			period_profit * ?,
			period_profit_rate,
			net_value,
			created_at
		FROM recharge_monthly_snapshots
		WHERE recharge_id = ?
	`, newRechargeID, remainingPrincipal, remainingPrincipal/withdrawPrincipal, remainingPrincipal/withdrawPrincipal, remainingPrincipal/withdrawPrincipal, originalRechargeID)
	
	if err != nil {
		// 快照复制失败不影响撤资，只记录日志
		fmt.Printf("⚠️  复制快照失败: %v\n", err)
	}
	
	// 4. 记录撤资
	_, err = tx.Exec(`
		INSERT INTO withdrawals 
		(recharge_id, user_id, original_amount, withdrawn_amount, final_profit, final_profit_rate, days_held, withdrawal_type, remaining_amount)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'partial', ?)
	`, originalRechargeID, userID, withdrawPrincipal, withdrawAmount, withdrawProfit, withdrawProfitRate, daysHeld, remainingPrincipal)
	
	if err != nil {
		return err
	}
	
	return tx.Commit()
}

// GetWithdrawals 获取用户的撤资记录
func (r *Repository) GetWithdrawals(userID int) ([]*model.Withdrawal, error) {
	rows, err := r.db.Query(`
		SELECT w.id, w.recharge_id, w.original_amount, w.withdrawn_amount, 
		       w.final_profit, w.final_profit_rate, w.days_held, w.withdrawn_at,
		       r.currency, r.recharge_at
		FROM withdrawals w
		LEFT JOIN recharges r ON w.recharge_id = r.id
		WHERE w.user_id = ?
		ORDER BY w.withdrawn_at DESC
	`, userID)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var withdrawals []*model.Withdrawal
	for rows.Next() {
		var w model.Withdrawal
		err := rows.Scan(
			&w.ID,
			&w.RechargeID,
			&w.OriginalAmount,
			&w.WithdrawnAmount,
			&w.FinalProfit,
			&w.FinalProfitRate,
			&w.DaysHeld,
			&w.WithdrawnAt,
			&w.Currency,
			&w.RechargeAt,
		)
		if err != nil {
			return nil, err
		}
		withdrawals = append(withdrawals, &w)
	}

	return withdrawals, nil
}

// SaveMonthlySnapshot 保存月度快照
func (r *Repository) SaveMonthlySnapshot(rechargeID, userID, periodNumber, daysInPeriod int, amount, startValue, endValue, periodProfit, periodProfitRate, netValue float64) error {
	snapshotDate := time.Now().Format("2006-01-02")
	
	_, err := r.db.Exec(`
		INSERT OR REPLACE INTO recharge_monthly_snapshots 
		(recharge_id, user_id, snapshot_date, period_number, days_in_period, amount, start_value, end_value, period_profit, period_profit_rate, net_value)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, rechargeID, userID, snapshotDate, periodNumber, daysInPeriod, amount, startValue, endValue, periodProfit, periodProfitRate, netValue)
	
	return err
}

// GetMonthlySnapshot 获取指定周期的快照
func (r *Repository) GetMonthlySnapshot(rechargeID int, periodNumber int) (map[string]interface{}, error) {
	var snapshot map[string]interface{}
	var snapshotDate string
	var daysInPeriod int
	var amount, startValue, endValue, periodProfit, periodProfitRate, netValue float64
	
	err := r.db.QueryRow(`
		SELECT snapshot_date, days_in_period, amount, start_value, end_value, period_profit, period_profit_rate, net_value
		FROM recharge_monthly_snapshots
		WHERE recharge_id = ? AND period_number = ?
	`, rechargeID, periodNumber).Scan(&snapshotDate, &daysInPeriod, &amount, &startValue, &endValue, &periodProfit, &periodProfitRate, &netValue)
	
	if err != nil {
		return nil, err
	}
	
	snapshot = map[string]interface{}{
		"snapshot_date":      snapshotDate,
		"days_in_period":     daysInPeriod,
		"amount":             amount,
		"start_value":        startValue,
		"end_value":          endValue,
		"period_profit":      periodProfit,
		"period_profit_rate": periodProfitRate,
		"net_value":          netValue,
	}
	
	return snapshot, nil
}

// GetLastSnapshotPeriod 获取最后记录的周期号
func (r *Repository) GetLastSnapshotPeriod(rechargeID int) (int, error) {
	var lastPeriod int
	
	err := r.db.QueryRow(`
		SELECT COALESCE(MAX(period_number), 0)
		FROM recharge_monthly_snapshots
		WHERE recharge_id = ?
	`, rechargeID).Scan(&lastPeriod)
	
	return lastPeriod, err
}

// GetRecentSnapshots 获取最近N个周期的快照
func (r *Repository) GetRecentSnapshots(rechargeID int, count int) ([]map[string]interface{}, error) {
	rows, err := r.db.Query(`
		SELECT period_number, snapshot_date, period_profit, period_profit_rate, start_value, end_value
		FROM recharge_monthly_snapshots
		WHERE recharge_id = ?
		ORDER BY period_number DESC
		LIMIT ?
	`, rechargeID, count)
	
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	var snapshots []map[string]interface{}
	for rows.Next() {
		var periodNumber int
		var snapshotDate string
		var periodProfit, periodProfitRate, startValue, endValue float64
		
		err := rows.Scan(&periodNumber, &snapshotDate, &periodProfit, &periodProfitRate, &startValue, &endValue)
		if err != nil {
			return nil, err
		}
		
		snapshot := map[string]interface{}{
			"period_number":      periodNumber,
			"snapshot_date":      snapshotDate,
			"period_profit":      periodProfit,
			"period_profit_rate": periodProfitRate,
			"start_value":        startValue,
			"end_value":          endValue,
		}
		snapshots = append(snapshots, snapshot)
	}
	
	return snapshots, nil
}