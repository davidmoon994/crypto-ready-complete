# 财务管理机器人 - 完整版

一个完整的加密货币盈亏追踪系统，符合你的所有需求！

## 🎯 核心功能

### Admin（管理后台）
✅ 绑定3个真实账户（Binance、OKX、Wallet）  
✅ 每个账户独立显示地址和余额  
✅ 创建Dashboard用户（手机号 + 固定密码abc123456）  
✅ 为用户充值（选择充值到哪个Admin账户）  
✅ 查看所有用户的余额和盈亏  
✅ 手动触发余额检查  

### Dashboard（用户前端）
✅ 手机号 + abc123456 登录  
✅ 查看总充值、当前价值、总盈亏  
✅ 查看每笔充值及盈亏  
✅ 查看每笔充值的每日历史  
✅ 手动刷新盈亏  

### 定时任务
✅ 每天北京时间8:00自动检查  
✅ 更新3个Admin账户余额  
✅ 计算所有充值的盈亏  

## 🚀 快速开始

### 方法1: 直接运行

```bash
# Linux/Mac
./start.sh

# Windows
start.bat
```

### 方法2: 手动运行

```bash
# 下载依赖
go mod download

# 编译
go build -o crypto-final cmd/main.go

# 运行
./crypto-final
```

## 📱 访问系统

启动后访问：
- 登录页：http://localhost:8080
- 管理后台：http://localhost:8080/admin
- 用户页面：http://localhost:8080/dashboard

## 🔑 默认账号

**管理员**
- 账号：admin
- 密码：admin123

**普通用户**
- 由管理员创建
- 密码统一：abc123456

## 📖 使用流程

### 1. 管理员配置

1. 用admin/admin123登录
2. 进入管理后台
3. 点击"配置钱包"，分别配置3个账户：
   - Binance: 输入API Key和Secret
   - OKX: 输入API Key和Secret
   - Wallet: 输入钱包地址
4. 点击"手动检查余额"测试配置

### 2. 创建用户

1. 点击"创建用户"
2. 输入手机号（如：13800138000）
3. 系统自动设置密码为abc123456

### 3. 为用户充值

1. 点击"充值"
2. 选择用户
3. 选择充值到哪个账户（Binance/OKX/Wallet）
4. 输入金额和币种
5. 确认充值

### 4. 用户查看

1. 用手机号+abc123456登录
2. 查看总体盈亏
3. 查看每笔充值详情
4. 点击"查看历史"可看每日变化

## 💡 核心原理

### 盈亏计算

每笔充值记录充值时Admin账户的余额作为基准：

```
假设：
- 用户A在2月1日充值$1000到Binance
- 当时Binance账户余额：$10,000（基准）

2月2日：
- Binance账户余额：$10,200
- 盈亏率 = (10,200 / 10,000) - 1 = 2%
- 盈亏金额 = 1,000 × 2% = $20

2月3日：
- Binance账户余额：$10,500  
- 盈亏率 = (10,500 / 10,000) - 1 = 5%
- 盈亏金额 = 1,000 × 5% = $50
```

### 数据结构

```
Admin账户（3个固定）
├─ Binance (ID=1)
│  ├─ 当前余额
│  └─ 每日余额时间序列
├─ OKX (ID=2)
│  ├─ 当前余额
│  └─ 每日余额时间序列
└─ Wallet (ID=3)
   ├─ 当前余额
   └─ 每日余额时间序列

Dashboard用户充值
└─ 每笔充值
   ├─ 用户ID
   ├─ Admin账户ID（1/2/3）
   ├─ 充值金额
   ├─ 基准余额
   └─ 每日盈亏记录
```

## 🔧 技术栈

- **后端**: Go 1.21 + Gin
- **数据库**: SQLite
- **定时任务**: robfig/cron
- **前端**: HTML + JavaScript（原生）

## 📁 项目结构

```
crypto-final/
├── cmd/
│   └── main.go                 # 程序入口
├── internal/
│   ├── model/                  # 数据模型
│   ├── repository/             # 数据库操作
│   ├── service/                # 业务逻辑
│   ├── handler/                # HTTP处理
│   └── scheduler/              # 定时任务
├── web/
│   └── templates/              # HTML页面
├── go.mod                      # Go模块
├── start.sh                    # Linux/Mac启动
└── start.bat                   # Windows启动
```

## ⚙️ 配置说明

### 真实API接入

编辑 `internal/service/wallet_service.go`：

```go
// TODO: 实际调用Binance API
// 当前使用模拟数据，需替换为真实API调用
```

替换为真实的API调用代码。

### 修改定时任务时间

编辑 `internal/scheduler/scheduler.go`：

```go
// 当前：每天8点
"0 0 8 * * *"

// 改为每天12点
"0 0 12 * * *"
```

## ❓ 常见问题

**Q: 如何修改用户密码？**  
A: 当前密码固定为abc123456，如需修改，编辑 `internal/service/service.go` 中的 `AdminCreateUser` 函数。

**Q: 如何添加更多Admin账户？**  
A: 当前固定为3个账户。如需增加，修改数据库初始化代码。

**Q: 余额显示为0？**  
A: 检查钱包API配置是否正确，点击"手动检查余额"测试。

**Q: 盈亏计算不准确？**  
A: 确保充值时Admin账户已有余额记录，可先手动检查一次余额。

## 📝 数据库

数据库文件：`crypto_final.db`

备份：
```bash
cp crypto_final.db crypto_final_backup.db
```

## 🎉 特性

- ✅ 3个Admin账户完全独立
- ✅ 每笔充值独立计算盈亏
- ✅ 从充值时间点开始计算
- ✅ Dashboard用户只是账本容器
- ✅ 所有密码固定abc123456
- ✅ 手机号登录
- ✅ 充值时选择Admin账户
- ✅ 每日自动更新

## 📞 支持

如遇问题，检查：
1. 终端错误日志
2. 浏览器控制台
3. 数据库是否正常创建

---

**立即可用！开箱即跑！**
