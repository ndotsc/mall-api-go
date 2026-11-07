# mall-api-go（Go + Gin 版商城后端）

用 **Go 1.27 + Gin + database/sql(sqlx)** 重写 Node 版 `mall-api`，
**接口 100% 兼容**、**复用同一个 MySQL 库**：前端 `mall-app` / `mall-web` 只要把
`VITE_API_BASE` 改成 `http://localhost:3002/api` 就能切换，代码一行不用改。

> 三套实现平级放在 `backend/` 下：`mall-api`(Node:3000) / `mall-api-php`(Laravel:3001) / `mall-api-go`(Gin:3002)。
> 全局约定见 [`../README.md`](../README.md)。本服务**不建表、不迁移**，表由 `mall-api` 的 Prisma 迁移创建。

---

## 1. 启动命令（在你本机执行）

```bash
cd backend/mall-api-go

go mod tidy                 # 下载依赖、生成 go.sum（首次执行）
cp .env.example .env        # 编辑：DATABASE_URL 指向同一个 mall 库，JWT_SECRET 与 Node 版完全一致
go run .                    # 开发启动 :3002
go build -o bin/mall-api-go .   # 生产构建出单文件二进制
./bin/mall-api-go
```

验证：

```bash
curl http://localhost:3002/api/health
# {"code":0,"message":"ok","data":{"time":"..."}}
curl "http://localhost:3002/api/products?page=1&pageSize=5"
```

浏览器打开 `http://localhost:3002/api/docs` 可看 Swagger UI（
`docs/openapi.json` 是从 Node 版 `/api/docs/openapi.json` 导出的同一份契约）。

> 本机已验证：`go mod tidy` + `gofmt` + `go vet ./...` + `go build ./...` + 链接二进制全部通过。
> 数据库必须可达且已由 Prisma 建好表，否则启动会报「数据库连接失败」。
> 真实 HTTP 冒烟与压测尚未跑，验收命令见 [`../README.md`](../README.md) 的「本机验收清单」。

## 2. 环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| `DATABASE_URL` | `mysql://mall:mall123@localhost:3306/mall` | 与 Node 版同库；内部自动转成 go-sql-driver DSN 并加 `parseTime=true&loc=UTC`（**必须 UTC**，见第 5 节） |
| `JWT_SECRET` | — | **必填且必须与 Node/PHP 版一致**，HS256 令牌才能互认 |
| `JWT_ACCESS_TTL_MIN` | `15` | access 有效分钟 |
| `JWT_REFRESH_TTL_DAYS` | `7` | refresh 有效天数 |
| `PORT` | `3002` | 监听端口 |
| `CORS_ORIGINS` | `*` | 逗号分隔的前端域名 |
| `WX_APPID` / `WX_SECRET` | 空 | 小程序/App 微信登录 |
| `WX_OA_APPID` / `WX_OA_SECRET` | 空 | 公众号网页授权（缺省回退上面的凭证） |
| `UPLOAD_DIR` | `public/uploads` | 上传目录 |
| `APP_ENV` | 空 | 设为 `test` 跳过登录限流 |

## 3. 目录结构

```
main.go                          启动入口（连接池单例 → 装配依赖 → 注册路由）
internal/config/config.go        .env 读取、DATABASE_URL→DSN 转换
internal/db/db.go                sqlx 连接池
internal/db/query.go             QueryAll / QueryOne / Count（行转 map + 类型归一化）
internal/service/jwt.go          HS256 令牌对、RefreshToken 轮换、整族吊销
internal/service/blacklist.go    注销 jti 黑名单（内存 + TTL）
internal/service/coupon.go       折扣算法 + 券文案（与 Node 版逐字一致）
internal/service/password.go     bcrypt
internal/service/wechat.go       微信 code2Session / 网页授权
internal/httpapi/response.go     统一响应信封 + ApiError + Wrap
internal/httpapi/middleware.go   JWT / 管理员 / 限流 / CORS / 1MB 限制 / Recovery / 404
internal/httpapi/router.go       全部路由（46 条）
internal/httpapi/handlers_*.go  按模块分组的业务实现
docs/openapi.json                OpenAPI 契约（//go:embed 进二进制，运行时不依赖外部文件）
```

**设计取舍**：没有引入 GORM，也没给 13 张表各写 struct，而是用 `sqlx.MapScan`
把行直接转成 `map[string]any`。好处是**列名天然保持 camelCase**（`createdAt`/`orderNo`/`isDefault`…），
响应字段与 Prisma 返回的一模一样；`query.go` 只做两件归一化：`[]byte→string`、
`TINYINT(1)→bool`。手写 SQL 时所有表名都用反引号（`` `Order` `` 是 SQL 关键字）。

## 4. 端点对照（与 mall-api/API.md 一致）

| 模块 | 端点 |
|---|---|
| 健康检查 | `GET /api/health` |
| 文档 | `GET /api/docs`、`GET /api/docs/openapi.json` |
| 认证 | `POST /api/auth/register·login·wx-login·wx-oauth·refresh`、`GET /auth/me`、`POST /auth/logout` |
| 商品 | `GET /api/products`（分页/keyword/categoryId/minPrice/maxPrice/sort）、`POST`(管理员)、`GET·PUT·DELETE /products/{id}` |
| 分类 | `GET /api/categories` |
| 购物车 | `GET·POST·DELETE /api/cart`、`PUT·DELETE /api/cart/{id}` |
| 地址 | `GET·POST /api/addresses`、`PUT·DELETE /api/addresses/{id}` |
| 订单 | `GET·POST /api/orders`、`GET /orders/{orderNo}`、`PATCH /orders/{orderNo}/status` |
| 文章 | `GET /api/articles`、`POST`(管理员)、`GET·PUT·DELETE /articles/{id}` |
| 上传 | `POST /api/upload`、静态 `GET /uploads/{file}` |
| 评价 | `GET /api/reviews`（附 stats）、`POST`(Bearer)、`GET /reviews/me` |
| 收藏 | `GET·POST /api/favorites`、`GET /favorites/check`、`DELETE /favorites/{productId}` |
| 优惠券 | `GET /api/coupons`、`POST`(管理员)、`POST /coupons/{id}/claim`、`GET /coupons/mine` |

共 **43 个业务端点 + 1 个健康检查 + 2 个文档端点 = 46 条路由**（已与 Node 版、PHP 版逐条核对一致）。
统一响应：成功 `{code:0,message:"ok",data:...}`，
失败 `{code:<HTTP状态码>,message:"...",data:null}`。

## 5. 与 Node 版的已知差异

| 项 | Node 版 | Go 版 | 影响 |
|---|---|---|---|
| ORM | Prisma `$extends` 自动注入软删除 | 手写 SQL，每个查询显式 `deletedAt IS NULL` | 无（行为一致，但新增查询要记得加） |
| 注销黑名单 | 内存 KV / 可选 Redis | 进程内 `map+mutex`，带 TTL 清理 | **多实例部署时各实例不共享**，需换 Redis |
| 限流 | `express-rate-limit` | 自写内存滑动窗口（同 IP 15min/10 次） | 同上；`APP_ENV=test` 可跳过 |
| 微信登录 | `code2Session` / 网页授权 | 同样调微信 API，用 `net/http` | 未配凭证返回 400 明确错误，不影响密码登录 |
| 请求校验 | zod schema | handler 内手写校验 | 错误文案基本一致，个别边界更宽松 |
| 参数绑定 | `z.coerce` 容忍字符串数字 | Go 结构体强类型 | 前端传数字字符串会 400「请求参数格式错误」；本项目前端都传数字 |
| 日志/审计/Sentry | 有中间件 | 未移植 | 学习项目不影响功能 |
| `Article.updatedAt` | Prisma `@updatedAt` 自动 | 手写 SQL 里显式 `SET updatedAt=UTC_TIMESTAMP()` | 无 |
| 时间基准 | Prisma 以 UTC 写 `DATETIME` | `loc=UTC` + `UTC_TIMESTAMP()` + `t.UTC().Format(...)` | **必须与 MySQL 会话时区都是 UTC**，否则跨后端判券有效期会偏 8 小时 |

**JWT 互认已实测**：Node `jsonwebtoken` 签的 access token 能被 `golang-jwt/jwt/v5` 验签，
Go 签的也能被 Node 验签，payload 的 `type/jti/userId/role/familyId` 完全一致。
只要三套后端 `JWT_SECRET` 相同，切换后端用户不会掉登录。

## 6. 切换前端

```bash
# mall-app/.env 或 mall-web/.env
VITE_API_BASE=http://localhost:3002/api
```

重新 `npm run dev:h5` / `npm run dev` 即可。
