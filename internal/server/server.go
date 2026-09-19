// 服务路由
package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

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
	"github.com/helantianshen/oss-sync/internal/update"
	"github.com/helantianshen/oss-sync/internal/vaults"
	"github.com/helantianshen/oss-sync/internal/version"
	"github.com/helantianshen/oss-sync/internal/webui"
)

// Server 持有运行期依赖：配置、DB、磁盘根。
type Server struct {
	Cfg *config.Config
	DB  *gorm.DB
	// Updater 非空时挂载 /api/admin/version 与 /api/admin/update/* 路由。
	Updater *update.Updater
	// UpdateService 为 helper 交接提供异步关闭回调，优于旧 supervisor 模型。
	UpdateService *update.Service
	// PluginManager 持有管理员安装的 WASM 服务插件。
	PluginManager *serverplugin.Manager
}

// New 创建 Server 实例，确保磁盘根目录存在。
func New(cfg *config.Config, db *gorm.DB) (*Server, error) {
	if err := auth.EnsureDatabaseJWTSecret(db, cfg); err != nil {
		return nil, fmt.Errorf("初始化数据库 JWT 密钥失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(cfg.Storage.DataDir), 0o755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}
	return &Server{Cfg: cfg, DB: db}, nil
}

// Router 构建 Gin 路由和中间件。
func (s *Server) Router() *gin.Engine {
	gin.SetMode(s.Cfg.Server.Mode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(accessLogger())
	if s.PluginManager != nil {
		r.Use(s.PluginManager.Middleware(s.Cfg))
	}

	// 超出内存上限的 multipart 内容由 Gin 写入临时文件。
	r.MaxMultipartMemory = s.Cfg.Server.MaxMultipartMemoryMB << 20

	r.GET("/healthz", s.healthz)
	r.GET("/readyz", s.readyz)

	authH := auth.NewHandler(s.DB, s.Cfg)
	authH.Register(r)

	adminH := admin.New(s.DB, s.Cfg)
	adminH.Register(r)

	webH, err := webui.New(s.DB, s.Cfg)
	if err != nil {
		panic("webui.New: " + err.Error())
	}
	if s.UpdateService != nil && s.Updater != nil {
		webH.SetUpdateService(s.UpdateService, s.Updater)
	}
	if s.PluginManager != nil {
		webH.SetPluginManager(s.PluginManager)
	}
	webH.Register(r)

	vaultsH := vaults.New(s.DB, s.Cfg)
	vaultsH.Register(r)

	devicesH := devices.New(s.DB, s.Cfg)
	devicesH.Register(r)

	syncH := syncapi.New(s.DB, s.Cfg)
	syncH.Register(r)

	blogH, err := blog.New(s.DB, s.Cfg)
	if err != nil {
		panic("blog.New: " + err.Error())
	}
	if s.PluginManager != nil {
		blogH.SetPluginHooks(s.PluginManager)
	}
	blogH.Register(r)

	sharesH := shares.New(s.DB, s.Cfg)
	sharesH.Register(r)

	if s.Updater != nil {
		var updateH *update.Handler
		if s.UpdateService != nil {
			updateH = update.NewHandlerWithService(s.DB, s.Cfg, s.Updater, s.UpdateService.Manager(), s.UpdateService)
		} else {
			updateH = update.NewHandler(s.DB, s.Cfg, s.Updater)
		}
		updateH.Register(r)
	}
	if s.PluginManager != nil {
		s.PluginManager.RegisterRoutes(r, s.Cfg)
	}

	return r
}

func (s *Server) healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"env":     config.Env(),
		"version": version.Version,
	})
}

func (s *Server) readyz(c *gin.Context) {
	sqlDB, err := s.DB.DB()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"ready": false, "version": version.Version, "error": err.Error()})
		return
	}
	if err := sqlDB.Ping(); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"ready": false, "version": version.Version, "error": err.Error()})
		return
	}
	var openStorageIssues int64
	if err := s.DB.Model(&models.StorageIssue{}).
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
