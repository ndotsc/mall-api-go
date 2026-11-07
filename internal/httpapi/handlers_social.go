package httpapi

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"mall-api-go/internal/db"
	"mall-api-go/internal/service"
)

// review 的默认分页是 10（与 Node 版 review.js 一致），商品/收藏是 50、订单是 20
func pageDefault(c *gin.Context, def int) (page, pageSize int) {
	page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ = strconv.Atoi(c.DefaultQuery("pageSize", strconv.Itoa(def)))
	if pageSize < 1 {
		pageSize = def
	}
	if pageSize > 50 {
		pageSize = 50
	}
	return
}

// ---------- 商品评价 ----------

// ReviewList：公开，productId 必填，附统计平均评分（保留 1 位小数）
func (d *Deps) ReviewList(c *gin.Context) error {
	productID, _ := strconv.Atoi(c.Query("productId"))
	if productID <= 0 {
		return Err(400, "缺少 productId")
	}
	page, pageSize := pageDefault(c, 10)
	total, err := db.Count(d.DB, "SELECT COUNT(*) FROM `Review` WHERE `productId`=?", productID)
	if err != nil {
		return err
	}
	list, err := db.QueryAll(d.DB,
		"SELECT * FROM `Review` WHERE `productId`=? ORDER BY `createdAt` DESC LIMIT ? OFFSET ?",
		productID, pageSize, (page-1)*pageSize)
	if err != nil {
		return err
	}
	var avg float64
	row := d.DB.QueryRowx("SELECT COALESCE(AVG(`rating`),0) FROM `Review` WHERE `productId`=?", productID)
	_ = row.Scan(&avg)
	average := math.Round(avg*10) / 10
	OK(c, map[string]any{
		"list": list, "total": total, "page": page, "pageSize": pageSize,
		"stats": map[string]any{"total": total, "averageRating": average},
	})
	return nil
}

func (d *Deps) ReviewMine(c *gin.Context) error {
	page, pageSize := pageDefault(c, 10)
	total, err := db.Count(d.DB, "SELECT COUNT(*) FROM `Review` WHERE `userId`=?", uid(c))
	if err != nil {
		return err
	}
	list, err := db.QueryAll(d.DB,
		"SELECT * FROM `Review` WHERE `userId`=? ORDER BY `createdAt` DESC LIMIT ? OFFSET ?",
		uid(c), pageSize, (page-1)*pageSize)
	if err != nil {
		return err
	}
	OK(c, map[string]any{"list": list, "total": total, "page": page, "pageSize": pageSize})
	return nil
}

func (d *Deps) ReviewCreate(c *gin.Context) error {
	var req reviewReq
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	if req.ProductID <= 0 {
		return Err(400, "商品 ID 无效")
	}
	if req.Rating < 1 || req.Rating > 5 {
		return Err(400, "评分需在 1-5 之间")
	}
	if len(req.Content) > 500 {
		return Err(400, "评价内容过长（≤500 字）")
	}
	p, err := db.QueryOne(d.DB, "SELECT `id` FROM `Product` WHERE `id`=? AND `deletedAt` IS NULL", req.ProductID)
	if err != nil {
		return err
	}
	if p == nil {
		return Err(404, "商品不存在")
	}
	// 用户名快照，避免展示时 join 含敏感字段的 User 表
	u, err := db.QueryOne(d.DB, "SELECT `username` FROM `User` WHERE `id`=? AND `deletedAt` IS NULL", uid(c))
	if err != nil {
		return err
	}
	name := "匿名用户"
	if u != nil {
		name = toStr(u["username"])
	}
	var content any = nil
	if req.Content != "" {
		content = req.Content
	}
	res, err := d.DB.Exec(
		"INSERT INTO `Review` (`userId`,`reviewerName`,`productId`,`rating`,`content`) VALUES (?,?,?,?,?)",
		uid(c), name, req.ProductID, req.Rating, content)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	row, err := db.QueryOne(d.DB, "SELECT * FROM `Review` WHERE `id`=?", int(id))
	if err != nil {
		return err
	}
	OK(c, row)
	return nil
}

// ---------- 收藏 ----------

func (d *Deps) FavoriteList(c *gin.Context) error {
	page, pageSize := pageDefault(c, 50)
	total, err := db.Count(d.DB, "SELECT COUNT(*) FROM `Favorite` WHERE `userId`=?", uid(c))
	if err != nil {
		return err
	}
	list, err := db.QueryAll(d.DB,
		"SELECT * FROM `Favorite` WHERE `userId`=? ORDER BY `createdAt` DESC LIMIT ? OFFSET ?",
		uid(c), pageSize, (page-1)*pageSize)
	if err != nil {
		return err
	}
	ids := make([]int, 0, len(list))
	for _, f := range list {
		ids = append(ids, toInt(f["productId"]))
	}
	snaps, err := d.productSnapshots(ids)
	if err != nil {
		return err
	}
	for _, f := range list {
		if p, ok := snaps[toInt(f["productId"])]; ok {
			f["product"] = p
		} else {
			f["product"] = nil // 商品已下架
		}
	}
	OK(c, map[string]any{"list": list, "total": total, "page": page, "pageSize": pageSize})
	return nil
}

func (d *Deps) FavoriteCheck(c *gin.Context) error {
	productID, _ := strconv.Atoi(c.Query("productId"))
	if productID <= 0 {
		return Err(400, "productId 无效")
	}
	row, err := db.QueryOne(d.DB, "SELECT `id` FROM `Favorite` WHERE `userId`=? AND `productId`=?", uid(c), productID)
	if err != nil {
		return err
	}
	OK(c, map[string]any{"favorited": row != nil})
	return nil
}

// FavoriteAdd：幂等（已收藏直接返回已有记录）
func (d *Deps) FavoriteAdd(c *gin.Context) error {
	var req favoriteReq
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	if req.ProductID <= 0 {
		return Err(400, "商品 ID 无效")
	}
	p, err := db.QueryOne(d.DB, "SELECT `id` FROM `Product` WHERE `id`=? AND `deletedAt` IS NULL", req.ProductID)
	if err != nil {
		return err
	}
	if p == nil {
		return Err(404, "商品不存在")
	}
	existing, err := db.QueryOne(d.DB, "SELECT * FROM `Favorite` WHERE `userId`=? AND `productId`=?", uid(c), req.ProductID)
	if err != nil {
		return err
	}
	if existing != nil {
		OK(c, existing)
		return nil
	}
	// 复合唯一约束 [userId,productId] 兜底并发重复插入
	_, err = d.DB.Exec("INSERT IGNORE INTO `Favorite` (`userId`,`productId`) VALUES (?,?)", uid(c), req.ProductID)
	if err != nil {
		return err
	}
	row, err := db.QueryOne(d.DB, "SELECT * FROM `Favorite` WHERE `userId`=? AND `productId`=?", uid(c), req.ProductID)
	if err != nil {
		return err
	}
	OK(c, row)
	return nil
}

// FavoriteRemove：幂等，未收藏也返回成功
func (d *Deps) FavoriteRemove(c *gin.Context) error {
	productID, _ := strconv.Atoi(c.Param("productId"))
	if productID <= 0 {
		return Err(400, "productId 无效")
	}
	existing, err := db.QueryOne(d.DB, "SELECT `id` FROM `Favorite` WHERE `userId`=? AND `productId`=?", uid(c), productID)
	if err != nil {
		return err
	}
	if existing != nil {
		if _, err := d.DB.Exec("DELETE FROM `Favorite` WHERE `userId`=? AND `productId`=?", uid(c), productID); err != nil {
			return err
		}
	}
	OK(c, map[string]any{"productId": productID, "removed": existing != nil})
	return nil
}

// ---------- 优惠券 ----------

// couponView：把模板行转成前端展示对象（带 label）
func couponView(m map[string]any) map[string]any {
	label := service.CouponLabel(service.RuleFromMap(m), toStr(m["name"]))
	return map[string]any{
		"id": m["id"], "name": m["name"], "type": m["type"],
		"threshold": m["threshold"], "value": m["value"], "maxDiscount": m["maxDiscount"],
		"validFrom": m["validFrom"], "validTo": m["validTo"], "label": label,
	}
}

// CouponList：领券中心，只列领取时间窗内的券，附 remaining / claimed
func (d *Deps) CouponList(c *gin.Context) error {
	rows, err := db.QueryAll(d.DB,
		"SELECT * FROM `Coupon` WHERE `validFrom`<=UTC_TIMESTAMP() AND `validTo`>=UTC_TIMESTAMP() ORDER BY `createdAt` DESC")
	if err != nil {
		return err
	}
	list := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		cid := toInt(row["id"])
		taken, err := db.Count(d.DB, "SELECT COUNT(*) FROM `UserCoupon` WHERE `couponId`=?", cid)
		if err != nil {
			return err
		}
		mine, err := db.QueryOne(d.DB,
			"SELECT `id` FROM `UserCoupon` WHERE `userId`=? AND `couponId`=? LIMIT 1", uid(c), cid)
		if err != nil {
			return err
		}
		v := couponView(row)
		if toInt(row["total"]) == -1 {
			v["remaining"] = -1 // -1 表示不限量
		} else {
			rem := toInt(row["total"]) - int(taken)
			if rem < 0 {
				rem = 0
			}
			v["remaining"] = rem
		}
		v["claimed"] = mine != nil
		list = append(list, v)
	}
	OK(c, map[string]any{"list": list})
	return nil
}

// CouponCreate：管理员建券模板
func (d *Deps) CouponCreate(c *gin.Context) error {
	var req couponReq
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	if strings.TrimSpace(req.Name) == "" || req.Value == nil {
		return Err(400, "券名称与 value 必填")
	}
	if req.Type != "full_reduction" && req.Type != "instant" && req.Type != "discount" {
		return Err(400, "券类型必须是 full_reduction / discount / instant")
	}
	if req.Type == "discount" && (*req.Value < 1 || *req.Value > 99) {
		return Err(400, "折扣券 value 需在 1-99 之间（90 表示 9 折）")
	}
	threshold := 0
	if req.Threshold != nil {
		threshold = *req.Threshold
	}
	if req.Type == "full_reduction" && threshold <= 0 {
		return Err(400, "满减券必须设置大于 0 的门槛")
	}
	from, err := toMySQLTime(req.ValidFrom)
	if err != nil {
		return err
	}
	to, err := toMySQLTime(req.ValidTo)
	if err != nil {
		return err
	}
	if to <= from {
		return Err(400, "结束时间必须晚于开始时间")
	}
	total, perUser := -1, 1
	if req.Total != nil {
		total = *req.Total
	}
	if req.PerUserLimit != nil {
		perUser = *req.PerUserLimit
	}
	var maxDiscount any = nil
	if req.MaxDiscount != nil {
		maxDiscount = *req.MaxDiscount
	}
	res, err := d.DB.Exec(
		"INSERT INTO `Coupon` (`name`,`type`,`threshold`,`value`,`maxDiscount`,`validFrom`,`validTo`,`total`,`perUserLimit`) VALUES (?,?,?,?,?,?,?,?,?)",
		req.Name, req.Type, threshold, *req.Value, maxDiscount, from, to, total, perUser)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	row, err := db.QueryOne(d.DB, "SELECT * FROM `Coupon` WHERE `id`=?", int(id))
	if err != nil {
		return err
	}
	OK(c, row)
	return nil
}

// CouponClaim：校验时间窗 / 每人限领 / 总量；幂等于「已有未使用的券」
func (d *Deps) CouponClaim(c *gin.Context) error {
	cid, _ := strconv.Atoi(c.Param("id"))
	if cid <= 0 {
		return Err(400, "couponId 无效")
	}
	coupon, err := db.QueryOne(d.DB, "SELECT * FROM `Coupon` WHERE `id`=?", cid)
	if err != nil {
		return err
	}
	if coupon == nil {
		return Err(404, "优惠券不存在")
	}
	now := time.Now()
	if vf := toTime(coupon["validFrom"]); !vf.IsZero() && now.Before(vf) {
		return Err(400, "优惠券尚未开始领取")
	}
	if vt := toTime(coupon["validTo"]); !vt.IsZero() && now.After(vt) {
		return Err(400, "优惠券已结束领取")
	}
	myCount, err := db.Count(d.DB, "SELECT COUNT(*) FROM `UserCoupon` WHERE `userId`=? AND `couponId`=?", uid(c), cid)
	if err != nil {
		return err
	}
	if int(myCount) >= toInt(coupon["perUserLimit"]) {
		return Err(400, "已达领取上限")
	}
	if toInt(coupon["total"]) != -1 {
		taken, err := db.Count(d.DB, "SELECT COUNT(*) FROM `UserCoupon` WHERE `couponId`=?", cid)
		if err != nil {
			return err
		}
		if int(taken) >= toInt(coupon["total"]) {
			return Err(400, "优惠券已抢光")
		}
	}
	existing, err := db.QueryOne(d.DB,
		"SELECT * FROM `UserCoupon` WHERE `userId`=? AND `couponId`=? AND `status`='unused' LIMIT 1", uid(c), cid)
	if err != nil {
		return err
	}
	if existing != nil {
		OK(c, existing) // 幂等
		return nil
	}
	res, err := d.DB.Exec("INSERT INTO `UserCoupon` (`userId`,`couponId`) VALUES (?,?)", uid(c), cid)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	row, err := db.QueryOne(d.DB, "SELECT * FROM `UserCoupon` WHERE `id`=?", int(id))
	if err != nil {
		return err
	}
	OK(c, row)
	return nil
}

// CouponMine：我的券，status=unused(默认)/used/expired/all；expired 由 validTo 实时算
func (d *Deps) CouponMine(c *gin.Context) error {
	status := c.DefaultQuery("status", "unused")
	all, err := db.QueryAll(d.DB,
		"SELECT * FROM `UserCoupon` WHERE `userId`=? ORDER BY `claimedAt` DESC", uid(c))
	if err != nil {
		return err
	}
	// 批量取券模板，避免 N+1
	cids := make([]any, 0, len(all))
	for _, uc := range all {
		cids = append(cids, toInt(uc["couponId"]))
	}
	couponMap := map[int]map[string]any{}
	if len(cids) > 0 {
		ph := make([]string, len(cids))
		args := make([]any, len(cids))
		for i, id := range cids {
			ph[i], args[i] = "?", id
		}
		rows, err := db.QueryAll(d.DB,
			fmt.Sprintf("SELECT * FROM `Coupon` WHERE `id` IN (%s)", strings.Join(ph, ",")), args...)
		if err != nil {
			return err
		}
		for _, r := range rows {
			couponMap[toInt(r["id"])] = r
		}
	}
	now := time.Now()
	list := make([]map[string]any, 0, len(all))
	for _, uc := range all {
		cp := couponMap[toInt(uc["couponId"])]
		if cp == nil {
			continue
		}
		effective := "unused"
		if toStr(uc["status"]) == "used" {
			effective = "used"
		} else if vt := toTime(cp["validTo"]); !vt.IsZero() && vt.Before(now) {
			effective = "expired"
		}
		if status != "all" && effective != status {
			continue
		}
		list = append(list, map[string]any{
			"id": uc["id"], "status": effective,
			"claimedAt": uc["claimedAt"], "usedAt": uc["usedAt"], "orderId": uc["orderId"],
			"coupon": couponView(cp),
		})
	}
	OK(c, map[string]any{"list": list, "total": len(list)})
	return nil
}
