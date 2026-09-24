// Package router 从功能处理器组装 HTTP 传输层
// 端点行为归各功能包所有，本包负责注册顺序、进程级中间件与健康检查
package router

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/helantianshen/oss-sync/internal/admin"
	"github.com/helantianshen/oss-sync/internal/auth"
	"github.com/helantianshen/oss-sync/internal/blog"
	"github.com/helantianshen/oss-sync/internal/config"
	"github.com/helantianshen/oss-sync/internal/devices"
	"github.com/helantianshen/oss-sync/internal/models"
	"github.com/helantianshen/oss-sync/internal/serverplugin"
	"github.com/helantianshen/oss-sync/internal/shares"
	"github.com/helantianshen/oss-sync/internal/syncapi"
	"github.com/helantianshen/oss-sync/internal/transport/http/middleware"
	"github.com/helantianshen/oss-sync/internal/update"
	"github.com/helantianshen/oss-sync/internal/vaults"
	"github.com/helantianshen/oss-sync/internal/version"
	"github.com/helantianshen/oss-sync/internal/webui"
)

// Dependencies 声明路由组装所需的运行时服务，显式边界避免处理器直接触及进程入口
type Dependencies struct {
	Cfg           *config.Config
	DB            *gorm.DB
	Updater       *update.Updater
	UpdateService *update.Service
	PluginManager *serverplugin.Manager
}

// Build 创建 Gin 引擎，安装进程级中间件并注册启用的功能路由
func Build(deps Dependencies) (*gin.Engine, error) {
	if deps.Cfg == nil {
		return nil, fmt.Errorf("router: nil config")
	}
	if deps.DB == nil {
		return nil, fmt.Errorf("router: nil database")
	}

	gin.SetMode(deps.Cfg.Server.Mode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.AccessLogger())
	if deps.PluginManager != nil {
		r.Use(deps.PluginManager.Middleware(deps.Cfg))
	}

	// 超过该阈值的 multipart 请求体由 Gin 写入临时文件，不再常驻内存
	r.MaxMultipartMemory = deps.Cfg.Server.MaxMultipartMemoryMB << 20
	registerHealthRoutes(r, deps.DB)

	authH := auth.NewHandler(deps.DB, deps.Cfg)
	authH.Register(r)

	adminH := admin.New(deps.DB, deps.Cfg)
	adminH.Register(r)

	webH, err := webui.New(deps.DB, deps.Cfg)
	if err != nil {
		return nil, fmt.Errorf("webui.New: %w", err)
	}
	if deps.UpdateService != nil && deps.Updater != nil {
		webH.SetUpdateService(deps.UpdateService, deps.Updater)
	}
	if deps.PluginManager != nil {
		webH.SetPluginManager(deps.PluginManager)
	}
	webH.Register(r)

	vaultsH := vaults.New(deps.DB, deps.Cfg)
	vaultsH.Register(r)

	devicesH := devices.New(deps.DB, deps.Cfg)
	devicesH.Register(r)

	syncH := syncapi.New(deps.DB, deps.Cfg)
	syncH.Register(r)

	blogH, err := blog.New(deps.DB, deps.Cfg)
	if err != nil {
		return nil, fmt.Errorf("blog.New: %w", err)
	}
	if deps.PluginManager != nil {
		deps.PluginManager.SetFileWriter(syncH)
		blogH.SetPluginHooks(deps.PluginManager)
		blogH.SetPluginDataHooks(deps.PluginManager)
	}
	blogH.Register(r)

	sharesH := shares.New(deps.DB, deps.Cfg)
	sharesH.Register(r)

	if deps.Updater != nil {
		var updateH *update.Handler
		if deps.UpdateService != nil {
			updateH = update.NewHandlerWithService(deps.DB, deps.Cfg, deps.Updater, deps.UpdateService.Manager(), deps.UpdateService)
		} else {
			updateH = update.NewHandler(deps.DB, deps.Cfg, deps.Updater)
		}
		updateH.Register(r)
	}
	if deps.PluginManager != nil {
		deps.PluginManager.RegisterRoutes(r, deps.Cfg)
	}

	return r, nil
}

// registerHealthRoutes 在功能路由之前暴露存活与就绪检查
func registerHealthRoutes(r *gin.Engine, db *gorm.DB) {
	r.GET("/healthz", healthz)
	r.GET("/readyz", readyz(db))
}

func healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"env":     config.Env(),
		"version": version.Version,
	})
}

func readyz(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sqlDB, err := db.DB()
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"ready": false, "version": version.Version, "error": err.Error()})
			return
		}
		if err := sqlDB.Ping(); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"ready": false, "version": version.Version, "error": err.Error()})
			return
		}

		var openStorageIssues int64
		if err := db.Model(&models.StorageIssue{}).
			Where("resolved_at IS NULL AND kind IN ?", []string{"missing", "hash_mismatch"}).
			Count(&openStorageIssues).Error; err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"ready": false, "version": version.Version, "error": err.Error()})
			return
		}
		if openStorageIssues > 0 {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"ready":               false,
				"version":             version.Version,
				"open_storage_issues": openStorageIssues,
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ready": true, "version": version.Version, "open_storage_issues": 0})
	}
}
