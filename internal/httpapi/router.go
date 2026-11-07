package httpapi

import (
	"github.com/gin-gonic/gin"
)

// NewRouter：与 Node 版 src/routes/index.js 的挂载结构一一对应。
// 全部挂在 /api 下；静态路由（check / mine / me）必须注册在参数路由之前，避免被 :id 抢先匹配。
func NewRouter(d *Deps) *gin.Engine {
	r := gin.New()
	r.Use(Recovery(), BodyLimit(), CORS(d.Cfg.CORSOrigins))
	r.NoRoute(NoRoute)

	// 上传的静态文件：/uploads/xxx
	r.Static("/uploads", d.Cfg.UploadDir)

	api := r.Group("/api")

	// 健康检查
	api.GET("/health", Wrap(d.Health))

	// Swagger 文档（与 Node 版 /api/docs 相同路径，直接复用导出的 openapi.json）
	api.GET("/docs", Wrap(d.DocsHTML))
	api.GET("/docs/openapi.json", Wrap(d.DocsJSON))

	// ---- 认证 ----
	auth := api.Group("/auth")
	auth.POST("/register", RateLimit(), Wrap(d.Register))
	auth.POST("/login", RateLimit(), Wrap(d.Login))
	auth.POST("/wx-login", RateLimit(), Wrap(d.WxLogin))
	auth.POST("/wx-oauth", RateLimit(), Wrap(d.WxOAuth))
	auth.POST("/refresh", RateLimit(), Wrap(d.Refresh))
	auth.GET("/me", Auth(d.JWT), Wrap(d.Me))
	auth.POST("/logout", Auth(d.JWT), Wrap(d.Logout))

	// ---- 商品 ----
	products := api.Group("/products")
	products.GET("", Wrap(d.ProductList))
	products.GET("/:id", Wrap(d.ProductDetail))
	products.POST("", Auth(d.JWT), Admin(), Wrap(d.ProductCreate))
	products.PUT("/:id", Auth(d.JWT), Admin(), Wrap(d.ProductUpdate))
	products.DELETE("/:id", Auth(d.JWT), Admin(), Wrap(d.ProductDelete))

	// ---- 分类 ----
	api.GET("/categories", Wrap(d.CategoryList))

	// ---- 文章 ----
	articles := api.Group("/articles")
	articles.GET("", Wrap(d.ArticleList))
	articles.GET("/:id", Wrap(d.ArticleDetail))
	articles.POST("", Auth(d.JWT), Admin(), Wrap(d.ArticleCreate))
	articles.PUT("/:id", Auth(d.JWT), Admin(), Wrap(d.ArticleUpdate))
	articles.DELETE("/:id", Auth(d.JWT), Admin(), Wrap(d.ArticleDelete))

	// ---- 上传 ----
	api.POST("/upload", Wrap(d.Upload))

	// ---- 购物车 ----
	cart := api.Group("/cart", Auth(d.JWT))
	cart.GET("", Wrap(d.CartList))
	cart.POST("", Wrap(d.CartAdd))
	cart.PUT("/:id", Wrap(d.CartUpdate))
	cart.DELETE("/:id", Wrap(d.CartRemove))
	cart.DELETE("", Wrap(d.CartClear))

	// ---- 收货地址 ----
	addresses := api.Group("/addresses", Auth(d.JWT))
	addresses.GET("", Wrap(d.AddressList))
	addresses.POST("", Wrap(d.AddressCreate))
	addresses.PUT("/:id", Wrap(d.AddressUpdate))
	addresses.DELETE("/:id", Wrap(d.AddressDelete))

	// ---- 订单 ----
	orders := api.Group("/orders", Auth(d.JWT))
	orders.GET("", Wrap(d.OrderList))
	orders.POST("", Wrap(d.OrderCreate))
	orders.GET("/:orderNo", Wrap(d.OrderDetail))
	orders.PATCH("/:orderNo/status", Wrap(d.OrderStatus))

	// ---- 评价 ----
	reviews := api.Group("/reviews")
	reviews.GET("", Wrap(d.ReviewList))
	reviews.GET("/me", Auth(d.JWT), Wrap(d.ReviewMine))
	reviews.POST("", Auth(d.JWT), Wrap(d.ReviewCreate))

	// ---- 收藏 ----
	favorites := api.Group("/favorites", Auth(d.JWT))
	favorites.GET("", Wrap(d.FavoriteList))
	favorites.GET("/check", Wrap(d.FavoriteCheck))
	favorites.POST("", Wrap(d.FavoriteAdd))
	favorites.DELETE("/:productId", Wrap(d.FavoriteRemove))

	// ---- 优惠券 ----
	coupons := api.Group("/coupons")
	coupons.GET("", Auth(d.JWT), Wrap(d.CouponList))
	coupons.GET("/mine", Auth(d.JWT), Wrap(d.CouponMine))
	coupons.POST("", Auth(d.JWT), Admin(), Wrap(d.CouponCreate))
	coupons.POST("/:id/claim", Auth(d.JWT), Wrap(d.CouponClaim))

	return r
}
