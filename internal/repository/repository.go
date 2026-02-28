package repository

import (
	"crypto-final/internal/model"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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
	err := r.db.QueryRow(
		"SELECT id, phone, password_hash, is_admin, created_at FROM users WHERE phone = ?",
		phone,
	).Scan(&user.ID, &user.Phone, &user.PasswordHash, &user.IsAdmin, &user.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	return user, err
}

func (r *Repository) GetAllUsers() ([]*model.UserSummary, error) {
	rows, err := r.db.Query(`
		SELECT 
			u.id,
			u.phone,
			u.is_active,
			COALESCE(SUM(CASE WHEN r.is_active = 1 THEN r.amount ELSE 0 END), 0) as total_recharge,
			COUNT(CASE WHEN r.is_active = 1 THEN r.id END) as recharge_count
		FROM users u
		LEFT JOIN recharges r ON u.id = r.user_id
		WHERE u.is_admin = 0
		GROUP BY u.id, u.phone, u.is_active
		ORDER BY u.id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*model.UserSummary
	for rows.Next() {
		user := &model.UserSummary{}
		err := rows.Scan(
			&user.UserID,
			&user.Phone,
			&user.IsActive,
			&user.TotalRecharge,
			&user.RechargeCount,
		)
		if err != nil {
			return nil, err
		}

		// 计算当前价值和盈亏
		currentValue := user.TotalRecharge
		recharges, _ := r.GetRechargesByUserID(user.UserID)
		for _, rech := range recharges {
			if !rech.IsActive {
				continue
			}
			latestProfit, _ := r.GetLatestRechargeProfit(rech.ID)
			if latestProfit != nil {
				rechCurrentValue := rech.Amount * (1 + latestProfit.ProfitRate/100)
				currentValue += (rechCurrentValue - rech.Amount)
			}
		}

		user.CurrentValue = currentValue
		user.TotalProfit = currentValue - user.TotalRecharge
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

func (r *Repository) UpdateAdminAccountBalance(id int, balance float64) error {
	_, err := r.db.Exec(
		"UPDATE admin_accounts SET current_balance=?, updated_at=CURRENT_TIMESTAMP WHERE id=?",
		balance, id,
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
		WHERE is_active = 1
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

func (r *Repository) GetRechargeByID(id int) (*model.Recharge, error) {
	rch := &model.Recharge{}
	err := r.db.QueryRow(
		`SELECT id, user_id, admin_account_id, amount, currency, recharge_at, base_balance, is_active, created_at
		 FROM recharges WHERE id=?`,
		id,
	).Scan(&rch.ID, &rch.UserID, &rch.AdminAccountID, &rch.Amount, &rch.Currency,
		&rch.RechargeAt, &rch.BaseBalance, &rch.IsActive, &rch.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	return rch, err
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
	err := r.db.QueryRow(
		"SELECT id, phone, password_hash, is_admin, is_active, created_at FROM users WHERE id = ?",
		userID,
	).Scan(&user.ID, &user.Phone, &user.PasswordHash, &user.IsAdmin, &user.IsActive, &user.CreatedAt)

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
