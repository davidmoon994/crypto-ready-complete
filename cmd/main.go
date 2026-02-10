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
	// 初始化数据库
	repo, err := repository.NewRepository("crypto_final.db")
	if err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}
	defer repo.Close()

	// 初始化服务层
	svc := service.NewService(repo)

	// 初始化处理器
	h := handler.NewHandler(svc)

	// 初始化定时任务
	sched := scheduler.NewScheduler(svc)
	sched.Start()
	defer sched.Stop()

	// 设置Gin模式
	gin.SetMode(gin.ReleaseMode)
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
	fmt.Println("║  服务地址: http://localhost:8080              ║")
	fmt.Println("║  登录页:   http://localhost:8080              ║")
	fmt.Println("║  管理后台: http://localhost:8080/admin        ║")
	fmt.Println("║  用户页面: http://localhost:8080/dashboard    ║")
	fmt.Println("╠════════════════════════════════════════════════╣")
	fmt.Println("║  默认管理员: admin / admin123                 ║")
	fmt.Println("║  普通用户密码: abc123456                      ║")
	fmt.Printf("║  下次检查: %s  ║\n", sched.GetNextRun().Format("2006-01-02 15:04:05"))
	fmt.Println("╚════════════════════════════════════════════════╝")

	// 优雅关闭
	go func() {
		if err := r.Run(":8080"); err != nil {
			log.Fatalf("服务启动失败: %v", err)
		}
	}()

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Println("\n正在关闭服务...")
}
