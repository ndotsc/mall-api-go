package service

import (
	"fmt"
	"strconv"
)

// 优惠券规则：与 Node 版 src/utils/coupon.js 逐字对齐。
// 所有金额单位都是「分」。
type CouponRule struct {
	Type        string
	Threshold   int
	Value       int
	MaxDiscount *int
}

func toInt(v any) int {
	switch n := v.(type) {
	case int64:
		return int(n)
	case int:
		return n
	case float64:
		return int(n)
	case bool:
		if n {
			return 1
		}
		return 0
	}
	return 0
}

// ToInt：导出版，供 httpapi 层把数据库行里的数字取出来
func ToInt(v any) int { return toInt(v) }

// RuleFromMap：把数据库行（map[string]any）转成规则，供「我的券/下单」复用。
func RuleFromMap(m map[string]any) CouponRule {
	r := CouponRule{
		Type:      str(m["type"]),
		Threshold: toInt(m["threshold"]),
		Value:     toInt(m["value"]),
	}
	if v, ok := m["maxDiscount"]; ok && v != nil {
		n := toInt(v)
		r.MaxDiscount = &n
	}
	return r
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// ComputeDiscount：返回优惠额与应付（分）。
// 门槛不满足时 discount=0 表示「券不可用」；过期/归属/已用由调用方判断。
func ComputeDiscount(r CouponRule, subtotal int) (discount, payable int) {
	switch r.Type {
	case "full_reduction":
		// 满减：小计达到门槛才减
		if subtotal >= r.Threshold {
			discount = r.Value
		}
	case "instant":
		// 立减：无门槛
		discount = r.Value
	case "discount":
		// 折扣：value=90 表示 9 折；maxDiscount 可封顶
		discount = subtotal * (100 - r.Value) / 100
		if r.MaxDiscount != nil && discount > *r.MaxDiscount {
			discount = *r.MaxDiscount
		}
	}
	if discount < 0 {
		discount = 0
	}
	if discount > subtotal {
		discount = subtotal // 优惠额不超过小计
	}
	return discount, subtotal - discount
}

// yuan：分转「元」文案，与 Node 版 (fen/100).toFixed(fen%100===0?0:2) 一致
func yuan(fen int) string {
	if fen%100 == 0 {
		return strconv.Itoa(fen / 100)
	}
	return fmt.Sprintf("%.2f", float64(fen)/100)
}

// CouponLabel：券的人类可读标签，前端直接展示。
func CouponLabel(r CouponRule, name string) string {
	switch r.Type {
	case "full_reduction":
		return "满" + yuan(r.Threshold) + "减" + yuan(r.Value)
	case "instant":
		return "立减" + yuan(r.Value)
	case "discount":
		rate := ""
		if r.Value%10 == 0 {
			rate = strconv.Itoa(r.Value / 10)
		} else {
			rate = fmt.Sprintf("%.1f", float64(r.Value)/10)
		}
		cap := ""
		if r.MaxDiscount != nil {
			cap = "（最高减" + yuan(*r.MaxDiscount) + "）"
		}
		return rate + "折" + cap
	}
	return name
}
