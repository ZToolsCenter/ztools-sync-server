package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ZToolsCenter/ztools-sync-server/auth"
	"github.com/ZToolsCenter/ztools-sync-server/config"
	"github.com/ZToolsCenter/ztools-sync-server/repository"
	"github.com/ZToolsCenter/ztools-sync-server/store"
	syncsvc "github.com/ZToolsCenter/ztools-sync-server/sync"
	"github.com/ZToolsCenter/ztools-sync-server/transport"
)

func main() {
	cfg, err := config.LoadStandalone()
	if err != nil {
		log.Fatalf("[Standalone] 配置加载失败: %v", err)
	}
	db, err := store.OpenWithOptions(cfg.Database)
	if err != nil {
		log.Fatalf("[Standalone] 数据库连接失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("[Standalone] 数据库句柄初始化失败: %v", err)
	}
	defer sqlDB.Close()

	authService := auth.New(db, cfg.JWTSecret)
	if cfg.BootstrapUsername != "" {
		created, err := authService.EnsureUser(cfg.BootstrapUsername, cfg.BootstrapPassword)
		if err != nil {
			log.Fatalf("[Standalone] 所有者账户初始化失败: %v", err)
		}
		if created {
			log.Printf("[Standalone] 已创建所有者账户: %s", cfg.BootstrapUsername)
		}
	}
	hasUsers, err := authService.HasUsers()
	if err != nil {
		log.Fatalf("[Standalone] 用户状态检查失败: %v", err)
	}
	if !hasUsers && !cfg.AllowRegistration {
		log.Fatal("[Standalone] 当前没有用户；首次启动必须配置 ZTOOLS_USERNAME 和 ZTOOLS_PASSWORD")
	}

	repo := repository.New(db)
	syncService := syncsvc.NewServiceWithOptions(repo, syncsvc.ServiceOptions{
		MaxConcurrentPushes: 1,
	})
	hub := transport.NewHub()
	router := transport.NewRouter(authService, syncService, hub, transport.RouterOptions{
		AllowRegistration: cfg.AllowRegistration,
	})

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("[Standalone] ZTools 独立同步服务器启动: http://localhost:%s", cfg.Port)
		serverErrors <- server.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case sig := <-stop:
		log.Printf("[Standalone] 收到退出信号: %s", sig)
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("[Standalone] HTTP 服务异常退出: %v", err)
		}
		return
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		log.Printf("[Standalone] HTTP 服务关闭超时: %v", err)
	}
	if cfg.Database.Driver == store.DriverSQLite {
		_ = db.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error
	}
}
