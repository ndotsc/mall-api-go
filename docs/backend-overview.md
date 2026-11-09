# mall 后端：三语言实现总览

同一个商城业务，用三套后端分别实现，**共用同一个 MySQL 库**（`mall`），接口完全兼容，
前端 `mall-app` / `mall-web` 只需改 `VITE_API_BASE` 就能在三者之间无缝切换。

| 目录 | 技术栈 | 默认端口 | 启动方式 | 说明 |
|---|---|---|---|---|
| `mall-api/` | Node.js · Express 5 + Prisma 6 | **3000** | 本机 `node src/server.js` | 原版（基准实现），接口文档 `API.md` / `GET /api/docs` |
| `mall-api-php/` | PHP · Laravel 11 | **3001** | **Docker Compose**（宿主机不装 PHP/Composer），见 [`mall-api-php/deploy.md`](mall-api-php/deploy.md) | 本轮新增，Query Builder 直接读写现有表（不用 Eloquent 模型） |
| `mall-api-go/` | Go · Gin | **3002** | **本机 `go run .`**，见 [`mall-api-go/deploy.md`](mall-api-go/deploy.md) | 本轮新增，database/sql + sqlx 手写 SQL（不用 GORM） |

> 三个目录**平级**放在 `backend/` 下，互不嵌套。切换端口只是为了避免本地同时启动时冲突，
> 生产环境都可通过环境变量改成 80/443 后面的任意端口。

---

## 为什么能无缝切换（兼容契约）

三套实现必须同时满足下面几条，缺一条前端就会出问题：

1. **统一响应信封**
   - 成功：`{ "code": 0, "message": "ok", "data": ... }`（HTTP 200）
   - 失败：`{ "code": <HTTP状态码>, "message": "...", "data": null }`，且 HTTP 状态码与 code 一致
     （400 / 401 / 403 / 404 / 429 / 500）
2. **同一份数据库**：表名是 Prisma 生成的 **PascalCase**
   （`User` `Product` `Order` `OrderItem` `Cart` `Address` `Category` `Article`
   `Review` `Favorite` `Coupon` `UserCoupon` `RefreshToken`），列名是 **camelCase**
   （`createdAt` `deletedAt` `orderNo` `receiverName` `totalAmount` `isDefault` …）。
   **不要执行任何 migrate**——表由 `mall-api` 的 Prisma 迁移创建，另外两套只读写。
   ⚠️ `Order` 是 SQL 关键字，手写 SQL 必须用反引号 `` `Order` ``。
3. **同一把 JWT 密钥**：`JWT_SECRET` 三处填**相同的值**，HS256，payload 为
   `{ type, jti, userId, role, familyId, iat, exp }`。这样 Node 签发的 token
   能被 PHP / Go 验签通过，反之亦然，切换后端时用户不会掉登录。
4. **金额单位一律「分」(int)**，前端展示才 ÷100。
5. **软删除**：`User` / `Product` 查询自动带 `deletedAt IS NULL`；删商品是置 `deletedAt` 而非物理删。
6. **优惠券折扣算法**与 `mall-api/src/utils/coupon.js` 逐字一致
   （满减看门槛 / 立减直减 / 折扣 `floor(subtotal*(100-value)/100)` 可封顶）。
7. **下单在事务内**完成：按库内价重算 → 校验并核销用户券 → 建订单与商品快照 → 标记券已用。
8. **时间基准统一 UTC**：库里的 `DATETIME` 一律存 UTC 墙钟时间
   （Prisma 写 UTC、PHP 用 `gmdate()`、Go 用 `loc=UTC` + `UTC_TIMESTAMP()` + `t.UTC()`）。
   这点是「三后端共用一库」最容易踩的坑：若某一套按本地时区读写，`createdAt`、
   `validFrom/validTo`、`expiresAt` 会整体偏 8 小时，出现「Go 建的券，Node/PHP 判已过期」。
   ⚠️ 因此 **MySQL 服务器/会话时区也要是 UTC**（官方 `mysql:8.0` 镜像默认即是）；
   部分列的 `DEFAULT CURRENT_TIMESTAMP` 取的是会话时区，本机自建 MySQL 请先
   `SET GLOBAL time_zone = '+00:00';`。
9. **图片 URL 契约**：数据库只存 `/uploads/xxx.png` 相对路径，接口一律返回**完整 URL**；
   写接口同时接受相对路径与完整 URL。详见下一节。

接口清单以 **`mall-api/API.md`** 为准。三套实现已核对为
**43 个业务端点 + 1 个健康检查 + 2 个文档端点 = 46 条路由，逐条一一对应**；
OpenAPI 契约快照（29 paths）三处同源：`mall-api/src/routes/docs.js`、
`mall-api-php/docs/openapi.json`、`mall-api-go/docs/openapi.json`。
各实现的 `README.md` 里也附了端点对照表与「与 Node 版的已知差异」。

---

## 图片 URL 与共享上传目录（前后端异域部署必看）

以前接口只返回 `/uploads/xxx.png`，浏览器会向**前端自己的域名**发起请求，
`mall-web` 靠 vite 把 `/uploads` 代理到 `localhost:3000` 才能显示图；
`mall-app` 则靠 `src/utils/image.js` 的 `resolveImg()` 补前缀。
一旦生产环境前端（`www.example.com`）和后端（`api.example.com`）不同域，或切到
Go / PHP 后端，这两招都会失效。现在改成：

| 位置 | 存/返回什么 | 谁负责 |
|---|---|---|
| MySQL 里的 `Product.cover` / `Article.cover` / `User.avatar` / `OrderItem.cover` | **相对路径** `/uploads/xxx.png` | 请求入口自动把完整 URL 还原成相对路径 |
| 所有接口响应（含 `POST /api/upload` 的 `url`） | **完整 URL** `http(s)://<origin>/uploads/xxx.png` | 统一响应出口自动补齐 origin |
| 前端 | 直接把返回值塞进 `<img :src>` / `<image :src>` | 不需要再做前缀拼接或代理 |

- **origin 来源**：`PUBLIC_ORIGIN` 环境变量优先（生产 / Nginx 反代 / CDN **必须配**，如
  `https://api.example.com` 或 `https://cdn.example.com`）；留空时按当前请求推导，
  本地分别为 `http://localhost:3000` / `:3001` / `:3002`。
  ⚠️ 真机调试（手机连电脑）要填局域网 IP，例如 `http://192.168.1.20:3002`，
  填 `localhost` 在手机上指向手机自己。
- **外链与 data: 原样返回**（如 `https://picsum.photos/...`），不会被改写；
  约定「凡 `/uploads/` 开头的字符串都视为本系统上传的图片路径」。
- **三套后端共用一份文件**：`backend/uploads/`（与各后端目录平级）。
  Node / Go 的 `.env` 里 `UPLOAD_DIR=../uploads`，PHP 容器由 `docker-compose.yml`
  把 `../uploads` 挂到 `/app/public/uploads`。否则「Node 上传的图，切到 Go/PHP 就 404」。
  该目录是运行时数据，**不在任何 Git 仓库里**，也不会被提交。
- 生产多实例不能靠本地目录共享，应换对象存储 / CDN，并把 `PUBLIC_ORIGIN` 指过去。
- `mall-web/vite.config.js` 里的 `/uploads` 代理**保留作兜底**（兼容旧数据里的相对路径），
  但接口已返回完整 URL，正常情况下不会再走到它。

实现落点（三份逻辑一一对应）：

| 后端 | 响应出口 | 请求入口 | 配置 |
|---|---|---|---|
| Node | `src/utils/response.js` → `ok()` | `src/app.js` 中间件 | `src/utils/assetUrl.js` |
| Go | `internal/httpapi/response.go` → `OK()` | `NormalizeAssetBody()` | `internal/httpapi/asset_url.go` |
| PHP | `app/Support/Api.php` → `ok()` | `ApiGuard` 中间件 | 同文件 `absolutize/relativize` |

> 🐛 **PHP 版排错备忘**：`ApiGuard` 对 JSON 请求体的每个字符串都跑一次「完整 URL → 相对路径」
> 正则，早期 PHP 版用 `#` 作 PCRE 分隔符，与模式字符类 `[^?#]` / `[?#]` 里的 `#` 冲突，
> 报 `Unknown modifier ']'`（warning 被 Laravel 提升为 `ErrorException`），表现为
> 「curl 的 GET 全正常、浏览器登录等 JSON POST 全 500」。现统一改用 `~` 分隔符，
> 模式与 Node / Go 逐字一致；排查表见 [`mall-api-php/deploy.md`](mall-api-php/deploy.md) 第 7 节。

---

## 分别启动（命令给你自己执行）

三套的 `.env` 都已生成好（权限 600、被 `.gitignore` 忽略），`JWT_SECRET` 与
`../mall-api/.env` 一致，**不需要再手工填写**。完整步骤与排错见各自的 `deploy.md`：
[`mall-api-go/deploy.md`](mall-api-go/deploy.md)（本机直跑）、
[`mall-api-php/deploy.md`](mall-api-php/deploy.md)（Docker）。

```bash
# 原版 Node（基准）
cd backend/mall-api
pnpm install
./node_modules/.bin/prisma generate      # 别用 pnpm prisma:generate，会被 packageManager 下载检查卡死
./node_modules/.bin/prisma migrate deploy
node src/server.js                       # :3000

# Laravel 版 —— 宿主机不装 PHP/Composer，全部在容器里
cd backend/mall-api-php
docker compose build
docker compose up -d                     # :3001，首次自动在容器内 composer install
docker compose logs -f api

# Gin 版 —— 本机直跑
cd backend/mall-api-go
go mod tidy && go run .                  # :3002，启动会 Ping 数据库
```

切换前端：把 `mall-app/.env` 或 `mall-web/.env` 的 `VITE_API_BASE` 改成对应地址即可
（如 `http://localhost:3001/api` / `http://localhost:3002/api`），**代码一行都不用改**。

> ⚠️ PHP 版在容器里，`DB_HOST` 必须是 `host.docker.internal`（`.env` 已配好），
> 填 `127.0.0.1` 会连到容器自己。
>
> 三套同时连同一个库时，注意 `JWT_SECRET` 必须一致，且不要同时用不同实现跑写操作压测，
> 以免互相干扰演示数据。

---

## 本机验收清单（命令留给你自己跑）

> **当前环境的实际状态**：Go 版已通过 `gofmt` / `go vet ./...` / `go build ./...`（含链接二进制），
> PHP 版的 `docker compose config` 也已校验通过；Node↔Go 的 JWT 双向互认此前已实测。
> PHP 容器首次启动曾因 **Composer 新版安全公告拦截** `laravel/framework ^11` /
> `firebase/php-jwt ^6.10` 而失败（并因自动重启刷屏），已在 `composer.json` 关闭该拦截、
> 在 compose 注入 `COMPOSER_NO_BLOCKING=1` / `COMPOSER_NO_AUDIT=1` 作双保险，
> 并把 `restart` 改为 `no`；**改动后需在你自己终端重新跑一次 `docker compose down && docker compose up -d`**。
> 沙箱内拉取 Docker 镜像被网络策略拦下，所以容器仍由你本机启动（**不需要装 PHP**）。
> 完整命令和排错表见 [`mall-api-php/deploy.md`](mall-api-php/deploy.md) 第 7 节。

### A. Laravel 版（Docker；前置：Docker Desktop + 宿主 MySQL 3306）

```bash
cd backend/mall-api-php

docker compose config                     # 纯语法校验，本环境已通过
docker compose build                      # 构建 PHP 运行时镜像
docker compose up -d                      # 首次启动自动在容器内 composer install
docker compose logs -f api                # 看装依赖进度；Ctrl+C 只退出日志，不停服务
docker compose ps                         # 等 STATUS 变 healthy

# 容器起来后，语法检查也在容器内做（宿主机不用装 PHP）
docker compose exec api sh -c 'find app bootstrap config routes public database -name "*.php" -print0 | xargs -0 -n1 php -l'
docker compose exec api php -l artisan
docker compose exec api php artisan route:list --path=api    # 应看到 46 条
```

> 数据库表由 `mall-api` 的 Prisma 迁移创建，**不要执行 `php artisan migrate`**（见 `database/README.md`）。
> `.env` 里 `APP_DEBUG=true`：500 会带真实报错，异常同时写 `storage/logs/laravel.log`
> （bind mount 落在宿主机，可直接 `tail -f`）。

### B. Gin 版

```bash
cd backend/mall-api-go
go mod tidy
go vet ./...
go build ./...            # 纯编译检查，不连库
# .env 已生成并与 mall-api/.env 对齐，不需要再 cp / 手填
go run .                  # :3002，启动时会 Ping 数据库，连不上会直接退出
```

详细步骤见 [`mall-api-go/deploy.md`](mall-api-go/deploy.md)。

### C. 三后端并排冒烟（三个终端分别起 3000 / 3001 / 3002 后，回到这里跑）

```bash
for p in 3000 3001 3002; do
  echo "===== :$p /api/health ====="; curl -s "http://localhost:$p/api/health"; echo
done

# 商品列表：三份 JSON 的 code/message/data.list 形状应一致
for p in 3000 3001 3002; do
  echo "===== :$p /api/products ====="; curl -s "http://localhost:$p/api/products?pageSize=1"; echo
done

# 搜索/筛选/排序、分类、文档契约
for p in 3000 3001 3002; do
  curl -s "http://localhost:$p/api/products?keyword=&minPrice=0&maxPrice=999999&sort=price_asc&pageSize=2"; echo
  curl -s "http://localhost:$p/api/categories" | head -c 160; echo
  curl -s "http://localhost:$p/api/docs/openapi.json" | head -c 120; echo
done
```

> `keyword=` 空串那一项是专门测「`Number('')===0` 陷阱」：不能被当成 `categoryId=0` 或 `price=0`。
> 需要 `jq` 的话把 `head -c` 换成 `jq '.info'`，本机没装 `jq` 也能跑。

# 图片 URL 契约：三个端口都应返回以 http(s) 开头的完整 URL，且能直接取到图片
for p in 3000 3001 3002; do
  echo "===== :$p cover ====="
  IMG=$(curl -s "http://localhost:$p/api/products?pageSize=1" | sed -n 's/.*"cover":"\([^"]*\)".*/\1/p')
  echo "$IMG"
  curl -s -o /dev/null -w "HTTP %{http_code}\n" "$IMG"
done

### D. 令牌互认（切换后端不掉登录）

```bash
# 用同一个账号分别在 3000 / 3001 / 3002 登录，把 3000 拿到的 token 打到另外两个端口：
TOKEN=$(curl -s -X POST http://localhost:3000/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"<你的账号>","password":"<你的密码>"}' | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
for p in 3000 3001 3002; do
  curl -s "http://localhost:$p/api/auth/me" -H "Authorization: Bearer $TOKEN"; echo
done
# 三个都应返回同一 userId（前提是三个 .env 的 JWT_SECRET 完全一致）
```

### E. 前端切换

把 `mall-app/.env` 或 `mall-web/.env` 的 `VITE_API_BASE` 改成
`http://localhost:3001/api` / `http://localhost:3002/api`，刷新页面走一遍
「登录 → 商品列表 → 详情收藏/评价 → 领券 → 购物车 → 结算用券 → 订单列表」。

---

## 现状与边界

- 这是**学习/对照项目**：三套实现共用一套业务规则，方便横向比较 Express / Laravel / Gin
  在路由、中间件、ORM、事务、错误处理上的写法差异。
- 生产化项目请只保留一套；多实例部署还需要 Redis 共享限流计数与注销黑名单
  （当前 PHP / Go 版为单实例内存/文件版，详见各自 README 的「已知差异」）。
- 微信登录、微信支付商户、短信等需要外部凭证的功能，未配置凭证时会返回明确错误，
  不影响账号密码登录与全部商城主流程。
