package httpapi

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"mall-api-go/internal/db"
)

// ---------- 商品 ----------

// ProductList：GET /api/products
// 分页 / keyword 模糊 / categoryId 精确 / minPrice,maxPrice(分) / sort 白名单排序。
// 空串与非法值一律忽略（避开 Number(”)===0 那个坑），与 Node 版 product.js 一致。
func (d *Deps) ProductList(c *gin.Context) error {
	page, pageSize := pagination(c)
	where := []string{"`deletedAt` IS NULL"}
	args := []any{}

	if kw := queryTrim(c, "keyword"); kw != "" {
		where = append(where, "`name` LIKE ?")
		args = append(args, "%"+kw+"%")
	}
	if raw := queryTrim(c, "categoryId"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			where = append(where, "`categoryId`=?")
			args = append(args, n)
		}
	}
	if raw := queryTrim(c, "minPrice"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			where = append(where, "`price`>=?")
			args = append(args, n)
		}
	}
	if raw := queryTrim(c, "maxPrice"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			where = append(where, "`price`<=?")
			args = append(args, n)
		}
	}
	cond := strings.Join(where, " AND ")

	total, err := db.Count(d.DB, "SELECT COUNT(*) FROM `Product` WHERE "+cond, args...)
	if err != nil {
		return err
	}
	list, err := db.QueryAll(d.DB,
		fmt.Sprintf("SELECT * FROM `Product` WHERE %s ORDER BY %s LIMIT ? OFFSET ?", cond, sortClause(c)),
		append(args, pageSize, (page-1)*pageSize)...)
	if err != nil {
		return err
	}
	OK(c, map[string]any{"list": list, "total": total, "page": page, "pageSize": pageSize})
	return nil
}

func (d *Deps) ProductDetail(c *gin.Context) error {
	id, _ := strconv.Atoi(c.Param("id"))
	p, err := db.QueryOne(d.DB, "SELECT * FROM `Product` WHERE `id`=? AND `deletedAt` IS NULL", id)
	if err != nil {
		return err
	}
	if p == nil {
		return Err(404, "商品不存在")
	}
	OK(c, p)
	return nil
}

func (d *Deps) ProductCreate(c *gin.Context) error {
	var req productReq
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	if strings.TrimSpace(req.Name) == "" || req.Price == nil || *req.Price < 0 {
		return Err(400, "商品名称与价格(分,非负整数)必填")
	}
	// 动态拼可选字段，未传的列走数据库默认值
	cols := []string{"`name`", "`price`"}
	vals := []string{"?", "?"}
	args := []any{req.Name, *req.Price}
	if req.Cover != "" {
		cols, vals, args = append(cols, "`cover`"), append(vals, "?"), append(args, req.Cover)
	}
	if req.Desc != "" {
		cols, vals, args = append(cols, "`desc`"), append(vals, "?"), append(args, req.Desc)
	}
	if req.CategoryID != nil {
		cols, vals, args = append(cols, "`categoryId`"), append(vals, "?"), append(args, *req.CategoryID)
	}
	res, err := d.DB.Exec(fmt.Sprintf("INSERT INTO `Product` (%s) VALUES (%s)",
		strings.Join(cols, ","), strings.Join(vals, ",")), args...)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	p, err := db.QueryOne(d.DB, "SELECT * FROM `Product` WHERE `id`=?", int(id))
	if err != nil {
		return err
	}
	OK(c, p)
	return nil
}

func (d *Deps) ProductUpdate(c *gin.Context) error {
	id, _ := strconv.Atoi(c.Param("id"))
	exists, err := db.QueryOne(d.DB, "SELECT `id` FROM `Product` WHERE `id`=? AND `deletedAt` IS NULL", id)
	if err != nil {
		return err
	}
	if exists == nil {
		return Err(404, "商品不存在")
	}
	var req productReq
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	sets, args := []string{}, []any{}
	if req.Name != "" {
		sets, args = append(sets, "`name`=?"), append(args, req.Name)
	}
	if req.Price != nil {
		if *req.Price < 0 {
			return Err(400, "价格不能为负")
		}
		sets, args = append(sets, "`price`=?"), append(args, *req.Price)
	}
	if req.Cover != "" {
		sets, args = append(sets, "`cover`=?"), append(args, req.Cover)
	}
	if req.Desc != "" {
		sets, args = append(sets, "`desc`=?"), append(args, req.Desc)
	}
	if req.CategoryID != nil {
		sets, args = append(sets, "`categoryId`=?"), append(args, *req.CategoryID)
	}
	if len(sets) > 0 {
		if _, err := d.DB.Exec("UPDATE `Product` SET "+strings.Join(sets, ",")+" WHERE `id`=?", append(args, id)...); err != nil {
			return err
		}
	}
	p, err := db.QueryOne(d.DB, "SELECT * FROM `Product` WHERE `id`=?", id)
	if err != nil {
		return err
	}
	OK(c, p)
	return nil
}

// ProductDelete：软删除，置 deletedAt 而非物理删（与 Prisma $extends 的约定一致）
func (d *Deps) ProductDelete(c *gin.Context) error {
	id, _ := strconv.Atoi(c.Param("id"))
	exists, err := db.QueryOne(d.DB, "SELECT `id` FROM `Product` WHERE `id`=? AND `deletedAt` IS NULL", id)
	if err != nil {
		return err
	}
	if exists == nil {
		return Err(404, "商品不存在")
	}
	if _, err := d.DB.Exec("UPDATE `Product` SET `deletedAt`=UTC_TIMESTAMP() WHERE `id`=?", id); err != nil {
		return err
	}
	OK(c, map[string]any{"id": id, "deleted": true})
	return nil
}

// ---------- 分类 ----------

func (d *Deps) CategoryList(c *gin.Context) error {
	list, err := db.QueryAll(d.DB, "SELECT * FROM `Category`")
	if err != nil {
		return err
	}
	OK(c, list)
	return nil
}

// ---------- 文章 ----------

func (d *Deps) ArticleList(c *gin.Context) error {
	page, pageSize := pagination(c)
	where := "1=1"
	args := []any{}
	if kw := queryTrim(c, "keyword"); kw != "" {
		where = "`title` LIKE ?"
		args = append(args, "%"+kw+"%")
	}
	total, err := db.Count(d.DB, "SELECT COUNT(*) FROM `Article` WHERE "+where, args...)
	if err != nil {
		return err
	}
	list, err := db.QueryAll(d.DB,
		fmt.Sprintf("SELECT * FROM `Article` WHERE %s ORDER BY `createdAt` DESC LIMIT ? OFFSET ?", where),
		append(args, pageSize, (page-1)*pageSize)...)
	if err != nil {
		return err
	}
	OK(c, map[string]any{"list": list, "total": total, "page": page, "pageSize": pageSize})
	return nil
}

func (d *Deps) ArticleDetail(c *gin.Context) error {
	id, _ := strconv.Atoi(c.Param("id"))
	a, err := db.QueryOne(d.DB, "SELECT * FROM `Article` WHERE `id`=?", id)
	if err != nil {
		return err
	}
	if a == nil {
		return Err(404, "文章不存在")
	}
	OK(c, a)
	return nil
}

func (d *Deps) ArticleCreate(c *gin.Context) error {
	var req articleReq
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	if req.Title == "" || req.Cover == "" || req.Summary == "" || req.Content == "" {
		return Err(400, "标题/封面/摘要/正文均为必填")
	}
	status := 1
	if req.Status != nil {
		status = *req.Status
	}
	res, err := d.DB.Exec(
		"INSERT INTO `Article` (`title`,`cover`,`summary`,`content`,`status`) VALUES (?,?,?,?,?)",
		req.Title, req.Cover, req.Summary, req.Content, status)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	a, err := db.QueryOne(d.DB, "SELECT * FROM `Article` WHERE `id`=?", int(id))
	if err != nil {
		return err
	}
	OK(c, a)
	return nil
}

func (d *Deps) ArticleUpdate(c *gin.Context) error {
	id, _ := strconv.Atoi(c.Param("id"))
	var req articleReq
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	sets, args := []string{}, []any{}
	if req.Title != "" {
		sets, args = append(sets, "`title`=?"), append(args, req.Title)
	}
	if req.Cover != "" {
		sets, args = append(sets, "`cover`=?"), append(args, req.Cover)
	}
	if req.Summary != "" {
		sets, args = append(sets, "`summary`=?"), append(args, req.Summary)
	}
	if req.Content != "" {
		sets, args = append(sets, "`content`=?"), append(args, req.Content)
	}
	if req.Status != nil {
		sets, args = append(sets, "`status`=?"), append(args, *req.Status)
	}
	// Article.updatedAt 是 @updatedAt，手写 SQL 必须自己更新（UTC，与 Prisma / PHP gmdate 对齐）
	sets = append(sets, "`updatedAt`=UTC_TIMESTAMP()")
	if _, err := d.DB.Exec("UPDATE `Article` SET "+strings.Join(sets, ",")+" WHERE `id`=?", append(args, id)...); err != nil {
		return err
	}
	a, err := db.QueryOne(d.DB, "SELECT * FROM `Article` WHERE `id`=?", id)
	if err != nil {
		return err
	}
	if a == nil {
		return Err(404, "文章不存在")
	}
	OK(c, a)
	return nil
}

func (d *Deps) ArticleDelete(c *gin.Context) error {
	id, _ := strconv.Atoi(c.Param("id"))
	if _, err := d.DB.Exec("DELETE FROM `Article` WHERE `id`=?", id); err != nil {
		return err
	}
	OK(c, map[string]any{"id": id})
	return nil
}

// ---------- 上传 ----------

var allowedImageExt = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
}

// Upload：POST /api/upload，字段名 file，仅图片 ≤2MB，服务端生成随机文件名防路径穿越
func (d *Deps) Upload(c *gin.Context) error {
	file, err := c.FormFile("file")
	if err != nil {
		return Err(400, "缺少文件字段 file")
	}
	if file.Size > 2<<20 {
		return Err(400, "图片不能超过 2MB")
	}
	ext := strings.ToLower(filepath.Ext(file.Filename))
	if !allowedImageExt[ext] {
		return Err(400, "仅支持 jpg/jpeg/png/gif/webp 图片")
	}
	if err := os.MkdirAll(d.Cfg.UploadDir, 0755); err != nil {
		return err
	}
	name := uuid.NewString() + ext
	if err := c.SaveUploadedFile(file, filepath.Join(d.Cfg.UploadDir, name)); err != nil {
		return err
	}
	OK(c, map[string]any{"url": "/uploads/" + name})
	return nil
}
