package main

import (
	"log"

	"mall-api-go/internal/config"
	"mall-api-go/internal/db"
	"mall-api-go/internal/httpapi"
	"mall-api-go/internal/service"
)

// mall-api-go：用 Gin 重写 Node 版 mall-api，接口完全兼容，复用同一个 MySQL 库。
// 启动：cp .env.example .env && go mod tidy && go run .   （默认 :3002）
func main() {
	cfg := config.Load()

	database, err := db.Open(cfg.DSN)
	if err != nil {
		log.Fatalf("数据库连接失败（确认 DATABASE_URL 与 MySQL 已启动）: %v", err)
	}
	defer database.Close()

	deps := &httpapi.Deps{
		DB:  database,
		JWT: service.NewJWTService(cfg, database),
		Cfg: cfg,
	}
	r := httpapi.NewRouter(deps)
	log.Printf("mall-api-go 已启动: http://localhost:%s/api/health", cfg.Port)
	if err := r.Run(":" + cfg.Port); err != nil {
		log.Fatal(err)
	}
}
