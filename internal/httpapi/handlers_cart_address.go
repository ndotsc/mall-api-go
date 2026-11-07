package httpapi

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"mall-api-go/internal/db"
)

// ---------- 购物车 ----------

// productSnapshots：一次 IN 查询取齐商品快照（对应 cart.js 的 pmap），
// 商品被软删时 map 里没有该 id，列表项的 product 就是 null。
func (d *Deps) productSnapshots(ids []int) (map[int]map[string]any, error) {
	out := map[int]map[string]any{}
	if len(ids) == 0 {
		return out, nil
	}
	ph := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		ph[i], args[i] = "?", id
	}
	rows, err := db.QueryAll(d.DB,
		"SELECT `id`,`name`,`price`,`cover` FROM `Product` WHERE `id` IN ("+strings.Join(ph, ",")+") AND `deletedAt` IS NULL", args...)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[toInt(r["id"])] = r
	}
	return out, nil
}

// CartList：GET /api/cart —— 每项附 product 快照
func (d *Deps) CartList(c *gin.Context) error {
	items, err := db.QueryAll(d.DB, "SELECT * FROM `Cart` WHERE `userId`=? ORDER BY `id` DESC", uid(c))
	if err != nil {
		return err
	}
	ids := make([]int, 0, len(items))
	for _, it := range items {
		ids = append(ids, toInt(it["productId"]))
	}
	snaps, err := d.productSnapshots(ids)
	if err != nil {
		return err
	}
	for _, it := range items {
		if p, ok := snaps[toInt(it["productId"])]; ok {
			it["product"] = p
		} else {
			it["product"] = nil
		}
	}
	OK(c, items)
	return nil
}

// CartAdd：同用户+同商品数量累加，number 默认 1、上限 99
func (d *Deps) CartAdd(c *gin.Context) error {
	var req cartAddReq
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	if req.ProductID <= 0 {
		return Err(400, "商品 ID 无效")
	}
	if req.Number <= 0 {
		req.Number = 1
	}
	if req.Number > 99 {
		req.Number = 99
	}
	p, err := db.QueryOne(d.DB, "SELECT `id` FROM `Product` WHERE `id`=? AND `deletedAt` IS NULL", req.ProductID)
	if err != nil {
		return err
	}
	if p == nil {
		return Err(400, "商品不存在或已下架")
	}
	exists, err := db.QueryOne(d.DB, "SELECT * FROM `Cart` WHERE `userId`=? AND `productId`=?", uid(c), req.ProductID)
	if err != nil {
		return err
	}
	if exists != nil {
		// 与 Node 版一致：累加，不额外封顶（前端与 zod 已限制单次 number ≤ 99）
		newNum := toInt(exists["number"]) + req.Number
		if _, err := d.DB.Exec("UPDATE `Cart` SET `number`=? WHERE `id`=?", newNum, toInt(exists["id"])); err != nil {
			return err
		}
		exists["number"] = newNum
		OK(c, exists)
		return nil
	}
	res, err := d.DB.Exec("INSERT INTO `Cart` (`userId`,`productId`,`number`) VALUES (?,?,?)", uid(c), req.ProductID, req.Number)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	row, err := db.QueryOne(d.DB, "SELECT * FROM `Cart` WHERE `id`=?", int(id))
	if err != nil {
		return err
	}
	OK(c, row)
	return nil
}

// CartUpdate：id 是购物车项 id（不是商品 id）
func (d *Deps) CartUpdate(c *gin.Context) error {
	id, _ := strconv.Atoi(c.Param("id"))
	var req cartNumberReq
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	if req.Number < 1 || req.Number > 99 {
		return Err(400, "数量需在 1-99 之间")
	}
	row, err := db.QueryOne(d.DB, "SELECT `id` FROM `Cart` WHERE `id`=? AND `userId`=?", id, uid(c))
	if err != nil {
		return err
	}
	if row == nil {
		return Err(404, "购物车项不存在")
	}
	if _, err := d.DB.Exec("UPDATE `Cart` SET `number`=? WHERE `id`=?", req.Number, id); err != nil {
		return err
	}
	updated, err := db.QueryOne(d.DB, "SELECT * FROM `Cart` WHERE `id`=?", id)
	if err != nil {
		return err
	}
	OK(c, updated)
	return nil
}

func (d *Deps) CartRemove(c *gin.Context) error {
	id, _ := strconv.Atoi(c.Param("id"))
	row, err := db.QueryOne(d.DB, "SELECT `id` FROM `Cart` WHERE `id`=? AND `userId`=?", id, uid(c))
	if err != nil {
		return err
	}
	if row == nil {
		return Err(404, "购物车项不存在")
	}
	if _, err := d.DB.Exec("DELETE FROM `Cart` WHERE `id`=?", id); err != nil {
		return err
	}
	OK(c, map[string]any{"id": id})
	return nil
}

func (d *Deps) CartClear(c *gin.Context) error {
	res, err := d.DB.Exec("DELETE FROM `Cart` WHERE `userId`=?", uid(c))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	OK(c, map[string]any{"count": int(n)})
	return nil
}

// ---------- 收货地址 ----------

var phoneRE = regexp.MustCompile(`^1[3-9]\d{9}$`)

func validateAddress(name, phone, detail string) error {
	if strings.TrimSpace(name) == "" || len(name) > 20 {
		return Err(400, "收货人必填且不超过 20 字")
	}
	if !phoneRE.MatchString(phone) {
		return Err(400, "手机号格式不正确（须为大陆 11 位手机号）")
	}
	if strings.TrimSpace(detail) == "" || len(detail) > 200 {
		return Err(400, "详细地址必填且不超过 200 字")
	}
	return nil
}

// AddressList：默认地址排最前
func (d *Deps) AddressList(c *gin.Context) error {
	list, err := db.QueryAll(d.DB,
		"SELECT * FROM `Address` WHERE `userId`=? ORDER BY `isDefault` DESC, `id` ASC", uid(c))
	if err != nil {
		return err
	}
	OK(c, list)
	return nil
}

// AddressCreate：isDefault=true 时事务内先把该用户其它默认地址取消，保证唯一
func (d *Deps) AddressCreate(c *gin.Context) error {
	var req addressReq
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	if err := validateAddress(req.Name, req.Phone, req.Detail); err != nil {
		return err
	}
	isDefault := 0
	if req.IsDefault != nil && *req.IsDefault {
		isDefault = 1
	}
	tx, err := d.DB.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if isDefault == 1 {
		if _, err := tx.Exec("UPDATE `Address` SET `isDefault`=0 WHERE `userId`=? AND `isDefault`=1", uid(c)); err != nil {
			return err
		}
	}
	res, err := tx.Exec(
		"INSERT INTO `Address` (`userId`,`name`,`phone`,`detail`,`isDefault`) VALUES (?,?,?,?,?)",
		uid(c), req.Name, req.Phone, req.Detail, isDefault)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	if err := tx.Commit(); err != nil {
		return err
	}
	row, err := db.QueryOne(d.DB, "SELECT * FROM `Address` WHERE `id`=?", int(id))
	if err != nil {
		return err
	}
	OK(c, row)
	return nil
}

// AddressUpdate：字段均可选；isDefault=true 时排除自身取消其它默认
func (d *Deps) AddressUpdate(c *gin.Context) error {
	id, _ := strconv.Atoi(c.Param("id"))
	exists, err := db.QueryOne(d.DB, "SELECT * FROM `Address` WHERE `id`=? AND `userId`=?", id, uid(c))
	if err != nil {
		return err
	}
	if exists == nil {
		return Err(404, "地址不存在")
	}
	var req addressReq
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	// 只校验「实际要改」的字段：合并现有值后再校验，避免只改默认标记时被误判
	name, phone, detail := req.Name, req.Phone, req.Detail
	if name == "" {
		name = toStr(exists["name"])
	}
	if phone == "" {
		phone = toStr(exists["phone"])
	}
	if detail == "" {
		detail = toStr(exists["detail"])
	}
	if err := validateAddress(name, phone, detail); err != nil {
		return err
	}
	sets, args := []string{}, []any{}
	if req.Name != "" {
		sets, args = append(sets, "`name`=?"), append(args, req.Name)
	}
	if req.Phone != "" {
		sets, args = append(sets, "`phone`=?"), append(args, req.Phone)
	}
	if req.Detail != "" {
		sets, args = append(sets, "`detail`=?"), append(args, req.Detail)
	}
	setDefault := req.IsDefault != nil && *req.IsDefault
	if req.IsDefault != nil {
		val := 0
		if setDefault {
			val = 1
		}
		sets, args = append(sets, "`isDefault`=?"), append(args, val)
	}
	tx, err := d.DB.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if setDefault {
		if _, err := tx.Exec("UPDATE `Address` SET `isDefault`=0 WHERE `userId`=? AND `isDefault`=1 AND `id`<>?", uid(c), id); err != nil {
			return err
		}
	}
	if len(sets) > 0 {
		if _, err := tx.Exec("UPDATE `Address` SET "+strings.Join(sets, ",")+" WHERE `id`=?", append(args, id)...); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	row, err := db.QueryOne(d.DB, "SELECT * FROM `Address` WHERE `id`=?", id)
	if err != nil {
		return err
	}
	OK(c, row)
	return nil
}

// AddressDelete：删掉默认地址后，把剩余第一条自动提升为默认
func (d *Deps) AddressDelete(c *gin.Context) error {
	id, _ := strconv.Atoi(c.Param("id"))
	exists, err := db.QueryOne(d.DB, "SELECT * FROM `Address` WHERE `id`=? AND `userId`=?", id, uid(c))
	if err != nil {
		return err
	}
	if exists == nil {
		return Err(404, "地址不存在")
	}
	tx, err := d.DB.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM `Address` WHERE `id`=?", id); err != nil {
		return err
	}
	if toBool(exists["isDefault"]) {
		// MySQL 支持 UPDATE ... ORDER BY ... LIMIT，比子查询规避 1093 错误更直接
		if _, err := tx.Exec(
			"UPDATE `Address` SET `isDefault`=1 WHERE `userId`=? ORDER BY `id` ASC LIMIT 1",
			uid(c)); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	OK(c, map[string]any{"id": id})
	return nil
}
