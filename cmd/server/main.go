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
	_, err = auth.EnsureBootstrapAdmin(db, cfg)
	if err != nil {
		log.Fatalf("初始化管理员失败: %v", err)
	}
	_, err = reconcile.New(db, cfg).Run(true)
	if err != nil {
		log.Printf("[OSS] 启动存储对账失败: %v", err)
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
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("ListenAndServe 失败: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Fatalf("HTTP Shutdown 失败: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}
