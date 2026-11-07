# mall 后端：三语言实现总览

同一个商城业务，用三套后端分别实现，**共用同一个 MySQL 库**（`mall`），接口完全兼容，
前端 `mall-app` / `mall-web` 只需改 `VITE_API_BASE` 就能在三者之间无缝切换。

| 目录 | 技术栈 | 默认端口 | 说明 |
|---|---|---|---|
| `mall-api/` | Node.js · Express 5 + Prisma 6 | **3000** | 原版（基准实现），接口文档 `API.md` / `GET /api/docs` |
| `mall-api-php/` | PHP · Laravel 11 | **3001** | 本轮新增，Query Builder 直接读写现有表（不用 Eloquent 模型） |
| `mall-api-go/` | Go · Gin | **3002** | 本轮新增，database/sql + sqlx 手写 SQL（不用 GORM） |

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

接口清单以 **`mall-api/API.md`** 为准。三套实现已核对为
**43 个业务端点 + 1 个健康检查 + 2 个文档端点 = 46 条路由，逐条一一对应**；
OpenAPI 契约快照（29 paths）三处同源：`mall-api/src/routes/docs.js`、
`mall-api-php/docs/openapi.json`、`mall-api-go/docs/openapi.json`。
各实现的 `README.md` 里也附了端点对照表与「与 Node 版的已知差异」。

---

## 分别启动（命令给你自己执行，别一次全起，注意端口）

```bash
# 原版 Node（基准）
cd backend/mall-api
pnpm install
./node_modules/.bin/prisma generate      # 别用 pnpm prisma:generate，会被 packageManager 下载检查卡死
./node_modules/.bin/prisma migrate deploy
node src/server.js                       # :3000

# Laravel 版
cd backend/mall-api-php
composer install
cp .env.example .env
php artisan key:generate
# 编辑 .env：DB_* 指向同一个 mall 库，JWT_SECRET 与 Node 版一致
php artisan serve --port=3001            # :3001

# Gin 版
cd backend/mall-api-go
go mod tidy
cp .env.example .env
# 编辑 .env：DATABASE_URL 指向同一个 mall 库，JWT_SECRET 与 Node 版一致
go run .                                 # :3002
```

切换前端：把 `mall-app/.env` 或 `mall-web/.env` 的 `VITE_API_BASE` 改成对应地址即可
（如 `http://localhost:3001/api` / `http://localhost:3002/api`），**代码一行都不用改**。

> 三套同时连同一个库时，注意 `JWT_SECRET` 必须一致，且不要同时用不同实现跑写操作压测，
> 以免互相干扰演示数据。

---

## 本机验收清单（命令留给你自己跑）

> **当前环境的实际状态**：Go 版已通过 `gofmt` / `go vet ./...` / `go build ./...`（含链接二进制），
> 且 Node↔Go 的 JWT 双向互认已实测通过；PHP 版源码写完但**本机没有 `php` / `composer`**，
> 临时 Docker 镜像又被网络策略拦下，所以 **`php -l`、`composer install`、`php artisan serve` 尚未执行过**。
> 下面 A 组命令请在装好 PHP 的机器上跑一遍，跑完才算 PHP 版验收完成。

### A. Laravel 版（需要 PHP 8.2+ / Composer / 扩展 pdo_mysql、mbstring、fileinfo、openssl）

```bash
cd backend/mall-api-php

# 1) 依赖与配置
composer install
cp .env.example .env
php artisan key:generate
# 编辑 .env：DB_* 指向 mall 库；JWT_SECRET 必须与 Node 版完全相同
# 首次排错建议先开 APP_DEBUG=true，500 响应会带上真实报错，同时写 storage/logs/laravel.log

# 2) 语法检查（23 个 PHP 文件，一个都不能报错）
find app bootstrap config routes public database -name '*.php' -print0 | xargs -0 -n1 php -l
php -l artisan

# 3) 路由表：应看到 46 条 /api/* 路由
php artisan route:list --path=api

# 4) 启动
php artisan serve --port=3001
```

> 数据库表由 `mall-api` 的 Prisma 迁移创建，**不要执行 `php artisan migrate`**（见 `database/README.md`）。

### B. Gin 版

```bash
cd backend/mall-api-go
go mod tidy
go vet ./...
go build ./...            # 纯编译检查，不连库
cp .env.example .env
# 编辑 .env：DATABASE_URL 指向同一个 mall 库，JWT_SECRET 与 Node 版一致
go run .                  # :3002，启动时会 Ping 数据库，连不上会直接退出
```

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
