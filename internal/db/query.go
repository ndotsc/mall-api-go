package db

import (
	"database/sql"

	"github.com/jmoiron/sqlx"
)

// Ext：*sqlx.DB 与 *sqlx.Tx 都实现了这三个方法，读写都能用同一套辅助函数。
// 不用 sqlx.Ext 是因为它不含 QueryRowx（Count 要用）。
type Ext interface {
	Queryx(query string, args ...interface{}) (*sqlx.Rows, error)
	QueryRowx(query string, args ...interface{}) *sqlx.Row
	Exec(query string, args ...interface{}) (sql.Result, error)
}

// boolColumns：MySQL 里是 TINYINT(1)，但 Node 版经 Prisma 返回的是 bool。
// 归一化成 bool，前端 `if (a.isDefault)` / `=== true` 才都成立。
var boolColumns = map[string]bool{
	"isDefault": true,
	"revoked":   true,
}

// normalize：把驱动返回的原始类型转成可直接 JSON 序列化的形状。
// - VARCHAR/TEXT 返回 []byte，不转的话 JSON 会变成 base64 字符串
// - TINYINT(1) 返回 int64，转成 bool
func normalize(m map[string]any) {
	for k, v := range m {
		switch val := v.(type) {
		case []byte:
			m[k] = string(val)
		case int64:
			if boolColumns[k] {
				m[k] = val != 0
			}
		}
	}
}

// QueryAll：返回行数组，字段名保持数据库列名（camelCase），
// 因此响应 data 与 Node 版 Prisma 返回的字段天然一致，无需再定义十几张表的 struct。
func QueryAll(q Ext, query string, args ...any) ([]map[string]any, error) {
	rows, err := q.Queryx(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]map[string]any, 0)
	for rows.Next() {
		m := map[string]any{}
		if err := rows.MapScan(m); err != nil {
			return nil, err
		}
		normalize(m)
		list = append(list, m)
	}
	return list, rows.Err()
}

// QueryOne：查不到时返回 (nil, nil)，与 Prisma 的 findUnique/findFirst 行为一致。
func QueryOne(q Ext, query string, args ...any) (map[string]any, error) {
	list, err := QueryAll(q, query, args...)
	if err != nil || len(list) == 0 {
		return nil, err
	}
	return list[0], nil
}

// Count：聚合计数，配合分页用。
func Count(q Ext, query string, args ...any) (int64, error) {
	var n int64
	err := q.QueryRowx(query, args...).Scan(&n)
	return n, err
}
