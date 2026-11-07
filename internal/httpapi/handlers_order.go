package httpapi

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"mall-api-go/internal/db"
	"mall-api-go/internal/service"
)

// attachOrderItems：订单列表/详情都要带 items（对应 Prisma 的 include: { items: true }）。
// 用一次 IN 查询批量取，避免 N+1。
func (d *Deps) attachOrderItems(orders []map[string]any) error {
	if len(orders) == 0 {
		return nil
	}
	ph := make([]string, len(orders))
	args := make([]any, len(orders))
	byID := map[int][]map[string]any{}
	for i, o := range orders {
		id := toInt(o["id"])
		ph[i], args[i] = "?", id
		byID[id] = []map[string]any{}
	}
	items, err := db.QueryAll(d.DB,
		"SELECT * FROM `OrderItem` WHERE `orderId` IN ("+strings.Join(ph, ",")+") ORDER BY `id` ASC", args...)
	if err != nil {
		return err
	}
	for _, it := range items {
		oid := toInt(it["orderId"])
		byID[oid] = append(byID[oid], it)
	}
	for _, o := range orders {
		o["items"] = byID[toInt(o["id"])]
	}
	return nil
}

func (d *Deps) orderWhere(c *gin.Context, orderNo string) (string, []any) {
	cond := []string{}
	args := []any{}
	if orderNo != "" {
		cond, args = append(cond, "`orderNo`=?"), append(args, orderNo)
	}
	if urole(c) != 2 {
		cond, args = append(cond, "`userId`=?"), append(args, uid(c))
	}
	if raw := queryTrim(c, "status"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			cond, args = append(cond, "`status`=?"), append(args, n)
		}
	}
	if len(cond) == 0 {
		return "1=1", args
	}
	return strings.Join(cond, " AND "), args
}

// OrderList：普通用户只看自己的，管理员看全部；支持 status 过滤 + 分页（默认 20，上限 100）
func (d *Deps) OrderList(c *gin.Context) error {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	where, args := d.orderWhere(c, "")
	total, err := db.Count(d.DB, "SELECT COUNT(*) FROM `Order` WHERE "+where, args...)
	if err != nil {
		return err
	}
	list, err := db.QueryAll(d.DB,
		fmt.Sprintf("SELECT * FROM `Order` WHERE %s ORDER BY `createdAt` DESC LIMIT ? OFFSET ?", where),
		append(args, pageSize, (page-1)*pageSize)...)
	if err != nil {
		return err
	}
	if err := d.attachOrderItems(list); err != nil {
		return err
	}
	OK(c, map[string]any{"list": list, "total": total, "page": page, "pageSize": pageSize})
	return nil
}

func (d *Deps) OrderDetail(c *gin.Context) error {
	where, args := d.orderWhere(c, c.Param("orderNo"))
	o, err := db.QueryOne(d.DB, "SELECT * FROM `Order` WHERE "+where, args...)
	if err != nil {
		return err
	}
	if o == nil {
		return Err(404, "订单不存在")
	}
	list := []map[string]any{o}
	if err := d.attachOrderItems(list); err != nil {
		return err
	}
	OK(c, o)
	return nil
}

func (d *Deps) OrderStatus(c *gin.Context) error {
	var req statusReq
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	if req.Status == nil || *req.Status < 1 || *req.Status > 5 {
		return Err(400, "状态无效（1-5）")
	}
	where, args := d.orderWhere(c, c.Param("orderNo"))
	o, err := db.QueryOne(d.DB, "SELECT `id` FROM `Order` WHERE "+where, args...)
	if err != nil {
		return err
	}
	if o == nil {
		return Err(404, "订单不存在")
	}
	if _, err := d.DB.Exec("UPDATE `Order` SET `status`=? WHERE `id`=?", *req.Status, toInt(o["id"])); err != nil {
		return err
	}
	updated, err := db.QueryOne(d.DB, "SELECT * FROM `Order` WHERE `id`=?", toInt(o["id"]))
	if err != nil {
		return err
	}
	list := []map[string]any{updated}
	if err := d.attachOrderItems(list); err != nil {
		return err
	}
	OK(c, updated)
	return nil
}

// OrderCreate：服务端按库内价重算金额（绝不信任前端传价），
// 校验并核销用户券，在**同一个事务**内建订单 + 商品快照 + 标记券已用。
func (d *Deps) OrderCreate(c *gin.Context) error {
	var req orderReq
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	if len(req.Items) == 0 {
		return Err(400, "订单中没有商品")
	}
	if strings.TrimSpace(req.Info.Name) == "" || strings.TrimSpace(req.Info.Phone) == "" || strings.TrimSpace(req.Info.Address) == "" {
		return Err(400, "收货人 / 手机号 / 地址必填")
	}

	// 1) 批量取商品（含软删除过滤），按库内价算小计
	ph := make([]string, len(req.Items))
	ids := make([]any, len(req.Items))
	for i, it := range req.Items {
		ph[i], ids[i] = "?", it.ProductID
	}
	products, err := db.QueryAll(d.DB,
		"SELECT * FROM `Product` WHERE `id` IN ("+strings.Join(ph, ",")+") AND `deletedAt` IS NULL", ids...)
	if err != nil {
		return err
	}
	pmap := map[int]map[string]any{}
	for _, p := range products {
		pmap[toInt(p["id"])] = p
	}
	subtotal := 0
	type snapshot struct {
		productID, price, number int
		name, cover              string
	}
	snaps := make([]snapshot, 0, len(req.Items))
	for _, it := range req.Items {
		p, ok := pmap[it.ProductID]
		if !ok {
			return Err(400, "商品不存在或已下架")
		}
		n := it.Number
		if n < 1 {
			n = 1
		}
		if n > 99 {
			n = 99
		}
		price := toInt(p["price"])
		subtotal += price * n
		cover := ""
		if p["cover"] != nil {
			cover = toStr(p["cover"])
		}
		snaps = append(snaps, snapshot{it.ProductID, price, n, toStr(p["name"]), cover})
	}

	// 2) 优惠券校验：归属 / 未用 / 未过期 / 门槛（与 Node 版 order.js 逐条一致）
	discountAmount := 0
	userCouponID := -1
	if req.CouponID != nil {
		uc, err := db.QueryOne(d.DB, "SELECT * FROM `UserCoupon` WHERE `id`=?", *req.CouponID)
		if err != nil {
			return err
		}
		if uc == nil || toInt(uc["userId"]) != uid(c) {
			return Err(404, "优惠券不存在")
		}
		if toStr(uc["status"]) != "unused" {
			return Err(400, "优惠券已使用")
		}
		coupon, err := db.QueryOne(d.DB, "SELECT * FROM `Coupon` WHERE `id`=?", toInt(uc["couponId"]))
		if err != nil {
			return err
		}
		if coupon == nil {
			return Err(404, "优惠券不存在")
		}
		if vt := toTime(coupon["validTo"]); !vt.IsZero() && vt.Before(time.Now()) {
			return Err(400, "优惠券已过期")
		}
		discount, _ := service.ComputeDiscount(service.RuleFromMap(coupon), subtotal)
		if discount <= 0 {
			return Err(400, "优惠券不满足使用条件（如未满门槛）")
		}
		discountAmount = discount
		userCouponID = toInt(uc["id"])
	}
	payable := subtotal - discountAmount
	orderNo := "NO" + strconv.FormatInt(time.Now().UnixMilli(), 10) + strconv.Itoa(time.Now().Nanosecond()%1000)

	// 3) 事务：订单 + 明细快照 + 核销券，任一步失败全部回滚
	tx, err := d.DB.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	payType := req.PayType
	if payType == "" {
		payType = "wechat"
	}
	var userCouponArg any = nil
	if userCouponID > 0 {
		userCouponArg = userCouponID
	}
	res, err := tx.Exec(
		"INSERT INTO `Order` (`userId`,`orderNo`,`totalAmount`,`payType`,`status`,`receiverName`,`receiverPhone`,`address`,`remark`,`discountAmount`,`userCouponId`) VALUES (?,?,?,?,1,?,?,?,?,?,?)",
		uid(c), orderNo, payable, payType, req.Info.Name, req.Info.Phone, req.Info.Address, req.Remark, discountAmount, userCouponArg)
	if err != nil {
		return err
	}
	orderID, _ := res.LastInsertId()
	for _, s := range snaps {
		if _, err := tx.Exec(
			"INSERT INTO `OrderItem` (`orderId`,`productId`,`name`,`price`,`cover`,`number`) VALUES (?,?,?,?,?,?)",
			orderID, s.productID, s.name, s.price, s.cover, s.number); err != nil {
			return err
		}
	}
	if userCouponID > 0 {
		// 条件更新 + 影响行数校验，防止并发下同一张券被用两次
		r, err := tx.Exec("UPDATE `UserCoupon` SET `status`='used',`usedAt`=UTC_TIMESTAMP(),`orderId`=? WHERE `id`=? AND `status`='unused'",
			orderID, userCouponID)
		if err != nil {
			return err
		}
		n, _ := r.RowsAffected()
		if n != 1 {
			return Err(400, "优惠券已使用")
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	OK(c, map[string]any{"orderNo": orderNo, "totalAmount": payable, "discountAmount": discountAmount})
	return nil
}
