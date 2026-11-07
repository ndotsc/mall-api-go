package service

import (
	"sync"
	"time"
)

// Blacklist：已注销 access token 的 jti 黑名单。
// 对应 Node 版 utils/blacklist.js 的内存版（kvStore 未配 REDIS_URL 时也是内存）。
// TTL 到 token 原定过期时间为止，过期自动失效。
// 单实例用足够；多实例部署需换 Redis 共享（见 README「已知差异」）。
type Blacklist struct {
	mu    sync.Mutex
	items map[string]int64 // jti -> 过期时间（Unix 秒）
}

func NewBlacklist() *Blacklist {
	return &Blacklist{items: make(map[string]int64)}
}

func (b *Blacklist) Add(jti string, expSec int64) {
	if jti == "" || expSec <= time.Now().Unix() {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.items[jti] = expSec
}

func (b *Blacklist) Has(jti string) bool {
	if jti == "" {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	exp, ok := b.items[jti]
	if !ok {
		return false
	}
	if exp <= time.Now().Unix() {
		delete(b.items, jti) // 顺手清理，避免无限增长
		return false
	}
	return true
}
