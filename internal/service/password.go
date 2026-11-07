package service

import "golang.org/x/crypto/bcrypt"

// bcrypt 与 Node 版 bcryptjs 兼容：
// - Go 生成 $2a$ 前缀，bcryptjs 可验证
// - bcryptjs 生成 $2a$/$2b$，Go 的 bcrypt.CompareHashAndPassword 也可验证
// - PHP password_hash 生成 $2y$，bcryptjs 可验证，Go 同样支持（见 README 差异说明）
func HashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}
