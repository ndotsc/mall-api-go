# mall-api-go 部署说明（本机直跑，不用 Docker）

Go 版直接在宿主机启动，默认监听 **3002**。三套后端平级放在 `backend/` 下：

| 服务 | 端口 | 启动方式 |
|---|---|---|
| `mall-api` (Node/Express) | 3000 | `node src/server.js` |
| `mall-api-php` (Laravel) | 3001 | Docker，见 `../mall-api-php/deploy.md` |
| **`mall-api-go` (Gin)** | **3002** | **本文：本机 `go run .`** |

三套共用同一个 MySQL `mall` 库和同一把 `JWT_SECRET`，所以令牌可以互认，前端只改
`VITE_API_BASE` 就能切换。全局契约见 [`../README.md`](../README.md)。

---

## 0. 前置检查

| 项 | 命令 | 期望 |
|---|---|---|
| Go 1.27+ | `go version` | `go1.27.x darwin/arm64`（本机已确认 1.27.2） |
| MySQL 已就绪且已由 Prisma 建好表 | `lsof -nP -iTCP:3306 -sTCP:LISTEN` | 有 LISTEN 输出（本机是 Docker 映射的 3306） |
| `.env` 已生成 | `ls -l .env` | 存在，权限 `600` |
| 3002 端口空闲 | `lsof -nP -iTCP:3002 -sTCP:LISTEN` | **无输出** |

`.env` 已由项目自带（被 `.gitignore` 忽略，**不要提交**）。里面的键：

```
DATABASE_URL  JWT_SECRET  JWT_ACCESS_TTL_MIN  JWT_REFRESH_TTL_DAYS  PORT
CORS_ORIGINS  WX_APPID  WX_SECRET  WX_OA_APPID  WX_OA_SECRET
UPLOAD_DIR  PUBLIC_ORIGIN  APP_ENV  LOG_LEVEL
```

其中 `DATABASE_URL` 与 `JWT_SECRET` 是从 `../mall-api/.env` 同步过来的。

> **图片 URL（这次新增）**：`UPLOAD_DIR=../uploads` 让三套后端共用 `backend/uploads`
> 里的图片文件；`PUBLIC_ORIGIN` 留空时接口按请求返回 `http://localhost:3002/uploads/xxx.png`
> 这样的完整 URL，前端跨域部署不必再配 vite proxy。
> 生产 / Nginx 反代要显式填 `PUBLIC_ORIGIN=https://api.example.com`，
> 手机真机调试要填局域网 IP（如 `http://192.168.1.20:3002`），填 `localhost` 在手机上无效。
如果 `.env` 丢了：

```bash
cp .env.example .env
# 然后把 DATABASE_URL / JWT_SECRET 改成与 ../mall-api/.env 完全一致的值
```

> ⚠️ **`JWT_SECRET` 不一致 = 切换后端时用户会掉登录**，这是三后端互认的唯一硬性前提。

---

## 1. 启动

```bash
cd backend/mall-api-go

go mod tidy          # 首次执行 / 改过 go.mod 后执行，更新 go.sum
go vet ./...         # 可选：静态检查
go build ./...       # 可选：纯编译检查，不连数据库

go run .             # 前台启动 :3002
```

看到下面这行就起来了：

```
mall-api-go 已启动: http://localhost:3002/api/health
```

`main.go` 启动时会 `Ping` 数据库，连不上会直接 `log.Fatalf("数据库连接失败...")` 退出，
所以「启动没报错」就等于「数据库通了」。

---

## 2. 验证

```bash
# 健康检查
curl -s http://localhost:3002/api/health
# {"code":0,"message":"ok","data":{"time":"..."}}

# 商品列表（会真实查库）
curl -s "http://localhost:3002/api/products?pageSize=1"

# 搜索/筛选/排序：keyword= 空串不能被当成 0，sort 走白名单
curl -s "http://localhost:3002/api/products?keyword=&minPrice=0&maxPrice=999999&sort=price_asc&pageSize=2"

curl -s http://localhost:3002/api/categories
curl -s http://localhost:3002/api/articles

# 404 也必须是统一信封，不能是 Gin 默认文本
curl -s -i http://localhost:3002/api/not-exist

# 接口文档
curl -s http://localhost:3002/api/docs/openapi.json | head -c 160
# 浏览器打开 http://localhost:3002/api/docs 看 Swagger UI

# ★ 图片 URL 契约：cover 必须是完整 URL，且能直接取到图片
IMG=$(curl -s "http://localhost:3002/api/products?pageSize=1" | sed -n 's/.*"cover":"\([^"]*\)".*/\1/p')
echo "$IMG"     # 期望 http://localhost:3002/uploads/xxx.png
curl -s -o /dev/null -w "HTTP %{http_code}\n" "$IMG"   # 期望 200
```

> 如果 `cover` 还是 `/uploads/xxx.png` 这种相对路径，说明服务没重启到新代码；
> 如果完整 URL 但取图 404，检查 `../uploads` 目录里有没有这个文件。

---

## 3. 令牌互认（切换后端不掉登录）

开另一个终端先启动 Node 版（3000），然后在 3002 这边验证：

```bash
TOKEN=$(curl -s -X POST http://localhost:3000/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"<你的账号>","password":"<你的密码>"}' \
  | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
# 装了 jq 的话：| jq -r .data.token

echo "token 长度: ${#TOKEN}"
curl -s http://localhost:3002/api/auth/me -H "Authorization: Bearer $TOKEN"
```

返回的 `userId` 应与 3000 的 `/api/auth/me` 一致。反过来用 3002 登录、打 3000/3001 同理。

---

## 4. 切换前端

```bash
# mall-app/.env 或 mall-web/.env
VITE_API_BASE=http://localhost:3002/api
```

改完重新 `npm run dev:h5`（mall-app）/ `npm run dev`（mall-web）。上线打包时把
这里换成公网域名，别留 `localhost`（详见 `../mall-api/DEPLOY.md` §6）。

---

## 5. 生产二进制 / 常驻运行

```bash
cd backend/mall-api-go

go build -o bin/mall-api-go .     # 单文件二进制，bin/ 已被 .gitignore 忽略
./bin/mall-api-go                 # 直接跑，不再依赖 Go 工具链

# 本机后台运行（仅演示用）
nohup ./bin/mall-api-go > /tmp/mall-api-go.log 2>&1 &
tail -f /tmp/mall-api-go.log
```

> 生产环境请用 systemd / supervisor / 容器托管进程，并用 Nginx 反代，
> 别直接把 3002 暴露到公网。

---

## 6. 停止

前台 `go run .` / 二进制：直接 `Ctrl+C`。
后台进程：`pkill -f bin/mall-api-go`。

---

## 7. 故障排查

| 现象 | 原因 | 处理 |
|---|---|---|
| `数据库连接失败...` 直接退出 | MySQL 没起 / `DATABASE_URL` 错 / 库里没表 | `lsof -nP -iTCP:3306 -sTCP:LISTEN`；确认 `.env` 的 `DATABASE_URL` 与 `../mall-api/.env` 一致；表由 `mall-api` 的 Prisma 迁移创建，**Go 版不建表** |
| `bind: address already in use` | 3002 被占 | `lsof -nP -iTCP:3002 -sTCP:LISTEN` 查进程，或改 `.env` 的 `PORT` |
| `/api/auth/me` 返回 401，但 3000 正常 | `JWT_SECRET` 不一致 | 对比两边 `.env` 的 `JWT_SECRET`，必须逐字符相同；改完重启 Go 服务 |
| 商品/订单的时间比 Node 版差 8 小时 | MySQL 会话时区不是 UTC | Go 版已固定 `loc=UTC` + `UTC_TIMESTAMP()`；把 MySQL 会话/全局时区设为 `+00:00`（见 `../README.md` 契约第 8 条） |
| 图片打不开（相对路径） | 服务没重启到新代码 / 前端还在自己拼前缀 | 重启 `go run .`；确认响应里的 `cover` 以 `http` 开头；`../README.md` 契约第 9 条 |
| 完整 URL 但取图 404 | `UPLOAD_DIR` 指错，或共享目录里没有该文件 | 检查 `.env` 的 `UPLOAD_DIR=../uploads`；`ls ../uploads`；三套后端共用这一份文件 |
| 真机（手机）上图片全是空白 | origin 推导成了 `localhost:3002`，手机上的 localhost 是手机自己 | 在 `.env` 填 `PUBLIC_ORIGIN=http://<电脑局域网IP>:3002`，重启服务 |
| 前端 https、后端 http 导致图片被浏览器拦截 | 混合内容 (mixed content) | 两边都上 https，或把 `PUBLIC_ORIGIN` 指向 https 的 CDN / 反代 |
| 上传 1~2MB 图片失败 | 旧版 `BodyLimit()` 给 multipart 也套了 1MB | 已改成 multipart 放行 2MB+64KB；重启服务即可 |
| 前端跨域被拦 | `CORS_ORIGINS` 没含前端域名 | `.env` 里改成逗号分隔的域名列表，默认 `*` |
| 登录/注册被 429 | 触发限流（同 IP 15 分钟 10 次） | 临时把 `.env` 的 `APP_ENV` 设为 `test` 跳过限流，压测完改回 |

---

## 8. 边界

- 这是**学习/对照项目**，注销黑名单与限流计数是进程内内存版，多实例部署需要换 Redis。
- `php artisan serve` 是 PHP 那边的开发服务器；Go 这边 `go run .` 用的是 Gin，性能没问题。
- 三套后端同时连同一个库时，别用不同实现同时跑写操作压测，会互相干扰演示数据。
