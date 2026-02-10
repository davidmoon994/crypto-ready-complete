package main

import (
	"crypto-final/internal/handler"
	"crypto-final/internal/repository"
	"crypto-final/internal/scheduler"
	"crypto-final/internal/service"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"
)

func main() {
	// 读取环境变量
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080" // 本地开发默认端口
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "crypto_final.db"
	}

	// 获取管理员密码（必须）
	adminPassword := os.Getenv("ADMIN_PASSWORD")
	if adminPassword == "" {
		log.Fatal("❌ 错误: 必须设置 ADMIN_PASSWORD 环境变量")
	}

	// 获取用户默认密码（可选）
	userPassword := os.Getenv("USER_PASSWORD")
	if userPassword == "" {
		userPassword = "user123456" // 默认密码
	}

	// 初始化数据库
	repo, err := repository.NewRepository(dbPath, adminPassword)
	if err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}
	defer repo.Close()

	// 初始化服务层
	svc := service.NewService(repo)
	svc.SetUserDefaultPassword(userPassword) // 设置用户默认密码

	// 初始化处理器
	h := handler.NewHandler(svc)

	// 初始化定时任务
	sched := scheduler.NewScheduler(svc)
	sched.Start()
	defer sched.Stop()

	// 设置Gin模式
	ginMode := os.Getenv("GIN_MODE")
	if ginMode == "" {
		ginMode = gin.ReleaseMode
	}
	gin.SetMode(ginMode)

	r := gin.Default()

	// CORS中间件
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	// 加载HTML模板
	r.LoadHTMLGlob("web/templates/*")

	// 前端页面路由
	r.GET("/", func(c *gin.Context) {
		c.HTML(200, "index.html", nil)
	})

	r.GET("/admin", func(c *gin.Context) {
		c.HTML(200, "admin.html", nil)
	})

	r.GET("/dashboard", func(c *gin.Context) {
		c.HTML(200, "dashboard.html", nil)
	})

	// API路由
	api := r.Group("/api")
	{
		// 公开接口
		api.POST("/login", h.Login)

		// 需要认证的接口
		auth := api.Group("", h.AuthMiddleware())
		{
			// Dashboard用户接口
			auth.GET("/dashboard/summary", h.GetDashboardSummary)
			auth.GET("/dashboard/recharges", h.GetDashboardRecharges)
			auth.GET("/dashboard/recharge/:id/history", h.GetRechargeHistory)
			auth.POST("/dashboard/refresh", h.DashboardManualRefresh)

			// 管理员接口
			admin := auth.Group("", h.AdminMiddleware())
			{
				// 用户管理
				admin.POST("/admin/users", h.AdminCreateUser)
				admin.GET("/admin/users", h.AdminGetUsers)
				admin.POST("/admin/recharge", h.AdminRecharge)

				// 钱包管理
				admin.POST("/admin/accounts/config", h.AdminConfigAccount)
				admin.GET("/admin/accounts/status", h.AdminGetAccountsStatus)

				// 系统管理
				admin.POST("/admin/manual-check", h.AdminManualCheck)
			}
		}
	}

	// 启动信息
	fmt.Println("╔════════════════════════════════════════════════╗")
	fmt.Println("║   加密货币盈亏追踪系统                          ║")
	fmt.Println("╠════════════════════════════════════════════════╣")
	fmt.Printf("║  服务端口: %s                                  ║\n", port)
	fmt.Println("║  登录页:   /                                   ║")
	fmt.Println("║  管理后台: /admin                              ║")
	fmt.Println("║  用户页面: /dashboard                          ║")
	fmt.Println("╠════════════════════════════════════════════════╣")
	fmt.Println("║  管理员账号: admin                             ║")
	fmt.Printf("║  下次检查: %s  ║\n", sched.GetNextRun().Format("2006-01-02 15:04:05"))
	fmt.Println("╚════════════════════════════════════════════════╝")

	// 优雅关闭
	go func() {
		if err := r.Run(":" + port); err != nil {
			log.Fatalf("服务启动失败: %v", err)
		}
	}()

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Println("\n正在关闭服务...")
}
