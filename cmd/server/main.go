// 服务入口
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
	"github.com/oss/oss-server/internal/update"
	"github.com/oss/oss-server/internal/version"

	"gorm.io/gorm"
)

func main() {
	// Hidden helper mode bypasses normal config/database startup.
	if ok, marker := update.IsHelperInvocation(); ok {
		code := update.RunHelper(marker)
		os.Exit(code)
	}
	// --version 只打印版本并退出，供更新流程校验下载的二进制。
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println(version.Version)
		return
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	updater, err := update.NewUpdater(cfg)
	if err != nil {
		log.Fatalf("初始化更新器失败: %v", err)
	}

	// Service shutdown callback is the only post-helper-launch shutdown route.
	// Manager is durable on DataDir for check_id lifecycle.
	var updateSvc *update.Service
	if mgr, err := update.NewManager(cfg.Storage.DataDir); err == nil {
		updateSvc = update.NewService(mgr, updater, cfg)
		// Ordinary startup: safely discover durable pending markers and resume helper
		// after validating active operation and marker safety. Covers crash-after-marker-before-helper-launch.
		if n, err := update.ResumePendingHandoffs(updater.ExecPath()); err != nil {
			log.Printf("[OSS] 恢复待处理更新失败: %v", err)
		} else if n > 0 {
			log.Printf("[OSS] 已恢复 %d 个待处理更新，等待 helper 完成", n)
		}
	} else {
		log.Printf("[OSS] 创建更新管理器失败: %v", err)
	}

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
	if _, err := auth.EnsureBootstrapAdmin(db, cfg); err != nil {
		log.Fatalf("初始化管理员失败: %v", err)
	}
	if _, err := reconcile.New(db, cfg).Run(true); err != nil {
		log.Printf("[OSS] 启动存储对账失败: %v", err)
	}

	updateDone := make(chan struct{})
	if updateSvc != nil {
		updateSvc.SetOnShutdown(func() {
			select {
			case <-updateDone:
			default:
				close(updateDone)
			}
		})
	}

	srv, err := server.New(cfg, db)
	if err != nil {
		log.Fatalf("初始化 server 失败: %v", err)
	}
	srv.Updater = updater
	srv.UpdateService = updateSvc

	router := srv.Router()
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	httpSrv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	sched := cron.NewScheduler(db, cfg)
	sched.Register()
	sched.Start()

	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[OSS] ListenAndServe 失败: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-quit:
		log.Printf("[OSS] 收到退出信号，开始优雅关闭")
	case <-updateDone:
		log.Printf("[OSS] 收到更新完成信号，开始优雅关闭")
	}
	shutdownGracefully(httpSrv, sched, db)
}

// shutdownGracefully 按“先停 scheduler、再 HTTP Shutdown、最后关 DB”的顺序优雅关闭。
func shutdownGracefully(httpSrv *http.Server, sched *cron.Scheduler, db *gorm.DB) {
	stopCtx, cancelStop := context.WithTimeout(context.Background(), 5*time.Second)
	if err := sched.Stop(stopCtx); err != nil {
		log.Printf("[OSS] 停止 cron 失败: %v", err)
	}
	cancelStop()

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP Shutdown 失败: %v", err)
	}
	cancelShutdown()

	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}

