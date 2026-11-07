package db

import (
	"time"

	_ "github.com/go-sql-driver/mysql" // 只注册驱动，不直接调用
	"github.com/jmoiron/sqlx"
)

// Open：全局只建一个连接池（对应 Node 版 prisma.js 的「PrismaClient 必须单例」）。
func Open(dsn string) (*sqlx.DB, error) {
	d, err := sqlx.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	// 与 mall-api/.env.example 的 PRISMA_CONNECTION_LIMIT 默认 10 对齐
	d.SetMaxOpenConns(10)
	d.SetMaxIdleConns(5)
	d.SetConnMaxLifetime(60 * time.Minute)
	if err := d.Ping(); err != nil {
		return nil, err
	}
	return d, nil
}
