package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/oss/oss-server/internal/auth"
	"github.com/oss/oss-server/internal/config"
	"github.com/oss/oss-server/internal/cron"
	"github.com/oss/oss-server/internal/database"
	"github.com/oss/oss-server/internal/reconcile"
	"github.com/oss/oss-server/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
<<<<<<< HEAD
=======
	log.Printf("[OSS] 当前环境 OSS_ENV=%s, db driver=%s", config.Env(), cfg.Database.Driver)
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b

	db, err := database.Init(cfg)
	if err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}

	if err := database.AutoMigrate(db); err != nil {
		log.Fatalf("AutoMigrate 失败: %v", err)
	}
	if err := auth.EnsureRegistrationSetting(db, cfg.Auth.AllowAnonymousRegistration); err != nil {
		log.Fatalf("初始化注册设置失败: %v", err)
	}
<<<<<<< HEAD
	_, err = auth.EnsureBootstrapAdmin(db, cfg)
	if err != nil {
		log.Fatalf("初始化管理员失败: %v", err)
	}
	_, err = reconcile.New(db, cfg).Run(true)
	if err != nil {
		log.Printf("[OSS] 启动存储对账失败: %v", err)
=======
	createdAdmin, err := auth.EnsureBootstrapAdmin(db, cfg, os.Stdin, os.Stdout)
	if err != nil {
		log.Fatalf("初始化管理员失败: %v", err)
	}
	if createdAdmin {
		log.Printf(
			"[OSS] 已创建初始管理员 %q；请访问 /admin 登录并按需调整注册开关",
			cfg.Auth.EffectiveBootstrapAdminUsername(),
		)
	}
	reconcileReport, err := reconcile.New(db, cfg).Run(true)
	if err != nil {
		log.Printf("[OSS] 启动存储对账失败: %v", err)
	} else {
		log.Printf("[OSS] 启动存储对账完成: %s", reconcileReport.String())
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	}

	srv, err := server.New(cfg, db)
	if err != nil {
		log.Fatalf("初始化 server 失败: %v", err)
	}

	router := srv.Router()
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	httpSrv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	sched := cron.NewScheduler(db, cfg)
	sched.Register()
	sched.Start()
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = sched.Stop(stopCtx)
	}()

	go func() {
<<<<<<< HEAD
=======
		log.Printf("[OSS] HTTP 监听 %s", addr)
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("ListenAndServe 失败: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
<<<<<<< HEAD
=======
	log.Println("[OSS] 收到退出信号，正在优雅关闭...")

>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Fatalf("HTTP Shutdown 失败: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
<<<<<<< HEAD
=======
	log.Println("[OSS] 已退出")
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
}
