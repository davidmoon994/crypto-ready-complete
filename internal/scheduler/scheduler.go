package scheduler

import (
	"crypto-final/internal/service"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

type Scheduler struct {
	cron    *cron.Cron
	service *service.Service
}

func NewScheduler(svc *service.Service) *Scheduler {
	// 创建支持秒级的cron，时区为北京时间
	c := cron.New(
		cron.WithSeconds(),
		cron.WithLocation(time.FixedZone("CST", 8*3600)),
	)

	return &Scheduler{
		cron:    c,
		service: svc,
	}
}

func (s *Scheduler) Start() {
	// 每天北京时间早上8点执行
	// Cron表达式: 秒 分 时 日 月 周
	// "0 0 8 * * *" = 每天8:00:00
	_, err := s.cron.AddFunc("0 0 8 * * *", func() {
		fmt.Printf("\n⏰ 定时任务触发: %s\n", time.Now().Format("2006-01-02 15:04:05"))
		if err := s.service.UpdateDailyBalances(); err != nil {
			fmt.Printf("❌ 每日余额检查失败: %v\n", err)
		}
	})

	if err != nil {
		fmt.Printf("❌ 添加定时任务失败: %v\n", err)
		return
	}

	s.cron.Start()
	fmt.Println("✓ 定时任务调度器已启动")
	fmt.Println("  - 每日余额检查: 每天北京时间 08:00:00")
}

func (s *Scheduler) Stop() {
	s.cron.Stop()
	fmt.Println("定时任务调度器已停止")
}

func (s *Scheduler) GetNextRun() time.Time {
	entries := s.cron.Entries()
	if len(entries) > 0 {
		return entries[0].Next
	}
	return time.Time{}
}
