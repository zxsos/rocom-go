package server

import (
	"fmt"
	"html"
	"io/fs"
	"mime"
	"regexp"
	"strings"
	"time"

	"github.com/whoisnian/rocom-capture/internal/gamedata"
)

const merchantMailFromName = "远哥来了"

// merchantMailHTMLTpl 邮件正文模板:纯白背景 + 浅色卡片 + 金色标题栏。
const merchantMailHTMLTpl = `<!DOCTYPE html>
<html lang="zh-CN"><body style="margin:0;padding:0;background:#ffffff;">
<div style="background:#ffffff;padding:36px 16px;font-family:-apple-system,'PingFang SC','Microsoft YaHei',sans-serif;">
  <div style="max-width:560px;margin:0 auto;background:#fffaf0;border-radius:18px;overflow:hidden;box-shadow:0 12px 40px rgba(0,0,0,.4);">
    <div style="background:linear-gradient(135deg,#f0b429,#d99a1e);padding:22px 28px;">
      <div style="font-size:20px;font-weight:800;color:#3a2505;">远行商人</div>
      <div style="font-size:12px;color:#7a5a15;margin-top:4px;">新货上架提醒</div>
    </div>
    <div style="padding:26px 28px;color:#3a2a14;font-size:14px;line-height:1.9;">%s</div>
    <div style="background:#f3e7cc;padding:14px 28px;font-size:12px;color:#8a6d3b;text-align:center;line-height:1.7;">
      本邮件由「远行商人」新货提醒自动发送<br>如需退订,请到站点「远行商人」页取消订阅
    </div>
  </div>
</div>
</body></html>`

// from 生成带中文显示名的 From 头。
func (m *smtpSender) from() string {
	return mime.QEncoding.Encode("utf-8", merchantMailFromName) + " <" + m.user + ">"
}

// merchantMailImg 邮件内嵌图片附件(cid 引用 + 原始 webp 字节)。
type merchantMailImg struct {
	cid  string
	data []byte
}

// merchantMailBody 把纯文本正文转成模板包裹的 HTML(保留换行与前导空格,列表行转 •;
// 行内 @img:<path> 商品图标记渲染为 <img src="cid:...">,图片字节收集到 imgs,
// 由 sendMerchantMail 以 multipart/related 附件发送)。
func merchantMailBody(body string, imgs *[]merchantMailImg) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		lead := len(line) - len(trimmed)
		if strings.HasPrefix(trimmed, "- ") {
			trimmed = "• " + strings.TrimPrefix(trimmed, "- ")
		}
		esc := merchantMailEscaped(trimmed, imgs)
		if lead > 0 {
			esc = strings.Repeat("&nbsp;", lead) + esc
		}
		// 单行断到 ≤850 字节:防止某一行本身(商品名/描述极端长)超限。
		lines[i] = merchantMailWrap(esc, 850)
	}
	// 行间用 <br>\r\n 连接而非裸 <br>:HTML 里 CRLF 渲染为空白,不产生可见换行
	// (可见换行由 <br> 负责),但能让每条商品独占一行,避免商品一多整段正文
	// 拼成一个超 998 字节的巨型单行触发 RFC 5321 拒绝。
	// 用 Replace 而非 Sprintf:模板背景渐变色里有裸 %(0%,55%,100%),
	// Sprintf 会把它当格式 verb 误解析导致正文占位符拿不到参数(%!s(MISSING))。
	return strings.Replace(merchantMailHTMLTpl, "%s", strings.Join(lines, "<br>\r\n"), 1)
}

// merchantMailWrap 把转义后的单行 HTML 在累计字节 ≥ maxBytes 处插入 CRLF 断行,
// 防止极端长行(超长商品名/描述)突破 RFC 5321 的 998 字节单行限制。
// 断点落在 rune 边界,不断开 UTF-8 字符;HTML 里裸 CRLF 渲染为空白,
// 不产生可见换行(可见换行由 <br> 负责),对收件端视觉无影响。
func merchantMailWrap(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	var b strings.Builder
	var n int
	for _, r := range s {
		size := len(string(r))
		if n > 0 && n+size > maxBytes {
			b.WriteString("\r\n")
			n = 0
		}
		b.WriteRune(r)
		n += size
	}
	return b.String()
}

// merchantMailEscaped 转义一行文本,并把行内的 @img:<path> 商品图标记替换成 <img>(不转义)。
func merchantMailEscaped(s string, imgs *[]merchantMailImg) string {
	if !strings.Contains(s, "@img:") {
		return html.EscapeString(s)
	}
	var b strings.Builder
	rest := s
	for {
		i := strings.Index(rest, "@img:")
		if i < 0 {
			b.WriteString(html.EscapeString(rest))
			break
		}
		b.WriteString(html.EscapeString(rest[:i]))
		rest = rest[i+len("@img:"):]
		j := strings.IndexAny(rest, " \t")
		path := rest
		if j >= 0 {
			path, rest = rest[:j], rest[j:]
		} else {
			rest = ""
		}
		if src := merchantImgHTML(path, imgs); src != "" {
			b.WriteString(src)
		}
	}
	return b.String()
}

// merchantImgHTML 把商品图路径渲染为邮件 <img>:http(s) 外链直接引用;
// 本地相对路径(本站 /img/ 前缀)读 embed 的 webp,以 CID 引用收集到 imgs
// (收件端无需访问本站即可显示,且不受 SMTP 单行 998 字节限制)。
// 读不到图片时返回空串(不显示)。
func merchantImgHTML(src string, imgs *[]merchantMailImg) string {
	if src == "" {
		return ""
	}
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		return merchantImgTag(src)
	}
	if b, err := fs.ReadFile(gamedata.ImageFS(), src); err == nil && len(b) > 0 {
		cid := fmt.Sprintf("merchant%d", len(*imgs)+1)
		*imgs = append(*imgs, merchantMailImg{cid: cid, data: b})
		return merchantImgTag("cid:" + cid)
	}
	return ""
}

// merchantImgTag 生成商品图 <img> 标签(src 是最终可用的 URL 或 cid: 引用)。
func merchantImgTag(src string) string {
	return `<img src="` + src + `" alt="" style="width:56px;height:56px;object-fit:contain;border-radius:10px;vertical-align:middle;margin:0 10px 0 2px;">`
}

// merchantKindTextMap 第三方商品 kind(英文) → 中文显示;未收录的未知值原样返回,
// 宁可不译不错译(邮件里出现新值可再补表)。
var merchantKindTextMap = map[string]string{
	"prop": "道具", "pet": "宠物", "egg": "精灵蛋", "fragment": "碎片",
	"skin": "皮肤", "cloth": "装扮", "material": "材料", "seed": "种子",
	"fruit": "果实", "food": "食物", "gem": "宝石", "diamond": "钻石",
	"ticket": "票券", "tool": "工具", "equip": "装备", "consumable": "消耗品",
	"furniture": "家具", "card": "卡片", "scroll": "卷轴", "key": "钥匙",
	"medal": "奖牌", "suit": "套装", "decoration": "装饰", "coin": "洛克贝",
}

func merchantKindText(kind string) string {
	if t, ok := merchantKindTextMap[kind]; ok {
		return t
	}
	return kind
}

// merchantMailSlots 标准售卖时段(北京时间),与前端 Merchant.jsx 的 SLOTS 一致。
var merchantMailSlots = []string{"08:00-12:00", "12:00-16:00", "16:00-20:00", "20:00-24:00"}

var merchantSlotRe = regexp.MustCompile(`^\d{2}:\d{2}-\d{2}:\d{2}$`)

// merchantItemSlots 解析商品售卖时段(与前端 parseSlots 一致):优先 time_label
// ("08:00-12:00 / …" 用 / 分割 + 正则校验),为空/格式不符时按 start_time/end_time
// (毫秒,北京时间语义)推断为单个时段串。
func merchantItemSlots(it merchantItem) []string {
	if raw := strings.TrimSpace(it.TimeLabel); raw != "" {
		slots := []string{}
		for _, s := range strings.Split(raw, "/") {
			if s = strings.TrimSpace(s); merchantSlotRe.MatchString(s) {
				slots = append(slots, s)
			}
		}
		if len(slots) > 0 {
			return slots
		}
	}
	if it.StartTime > 0 && it.EndTime > 0 {
		st := time.UnixMilli(it.StartTime).In(merchantLoc)
		et := time.UnixMilli(it.EndTime).In(merchantLoc)
		s, e := st.Format("15:04"), et.Format("15:04")
		if e == "00:00" {
			e = "24:00"
		}
		if s == e {
			return nil
		}
		return []string{s + "-" + e}
	}
	return nil
}

// merchantAllDay 是否覆盖全部四个标准时段(全天售卖)。
func merchantAllDay(slots []string) bool {
	for _, s := range merchantMailSlots {
		found := false
		for _, x := range slots {
			if x == s {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// merchantGroup 邮件正文的时段分组:全天 / 标准时段 / 其他。
type merchantGroup struct {
	Title string
	Items []merchantItem
}

// merchantGroupItems 把新增商品按时段分组(与前端 groupBySlot 一致):
// 覆盖全部四段的归「全天」,其余按各自时段归组,不在标准四段内的归「其他」。
func merchantGroupItems(items []merchantItem) (allDay []merchantItem, groups []merchantGroup, other []merchantItem) {
	slotGroups := make([][]merchantItem, len(merchantMailSlots))
	slotIdx := make(map[string]int, len(merchantMailSlots))
	for i, s := range merchantMailSlots {
		slotIdx[s] = i
	}
	for _, it := range items {
		slots := merchantItemSlots(it)
		if len(slots) > 0 && merchantAllDay(slots) {
			allDay = append(allDay, it)
			continue
		}
		hit := false
		for _, s := range slots {
			if i, ok := slotIdx[s]; ok {
				slotGroups[i] = append(slotGroups[i], it)
				hit = true
			}
		}
		if !hit {
			other = append(other, it)
		}
	}
	for i, s := range merchantMailSlots {
		if len(slotGroups[i]) > 0 {
			groups = append(groups, merchantGroup{Title: s, Items: slotGroups[i]})
		}
	}
	return allDay, groups, other
}

// merchantMailContent 构造新货提醒的内容区 HTML(嵌入 merchantMailHTMLTpl 的 %s):
// 商人名大标题 + 营业日/本轮,商品按「全天 / 标准时段 / 其他」分组展示。
// 全天商品不打具体时间点(组标题已表达),商品图以 CID 引用收集到 imgs。
func merchantMailContent(name, day, slot string, items []merchantItem, imgs *[]merchantMailImg) string {
	var b strings.Builder
	// 每行以 CRLF 结尾:HTML 里裸 CRLF 渲染为空白,不产生可见换行,但保证
	// 任何单行(组标题/商品行)都不超过 RFC 5321 的 998 字节限制。
	b.WriteString(`<div style="text-align:center;padding:2px 0 0;">` + "\r\n")
	b.WriteString(`<span style="font-size:22px;font-weight:800;color:#3a2505;">` + html.EscapeString(name) + `</span></div>` + "\r\n")
	b.WriteString(`<div style="text-align:center;font-size:12px;color:#8a6d3b;margin:4px 0 2px;">营业日 ` + html.EscapeString(day) + ` · 本轮 ` + html.EscapeString(slot) + `</div>` + "\r\n")
	allDay, groups, other := merchantGroupItems(items)
	merchantMailGroup(&b, "全天售卖", allDay, imgs)
	for _, g := range groups {
		merchantMailGroup(&b, g.Title, g.Items, imgs)
	}
	merchantMailGroup(&b, "其他时段", other, imgs)
	return b.String()
}

// merchantMailGroup 输出一个时段分组:金色小标题 + 商品行列表。
func merchantMailGroup(b *strings.Builder, title string, items []merchantItem, imgs *[]merchantMailImg) {
	if len(items) == 0 {
		return
	}
	// 断行必须写成 `...` + "\r\n" 拼接:raw string(反引号)不做转义,直接写进
	// 模板里的 \r\n 是四个字面字符,收件端会把它当文本显示出来。
	fmt.Fprintf(b, `<div style="font-size:13px;font-weight:700;color:#a06a10;margin:16px 0 8px;padding-left:8px;border-left:3px solid #f0b429;">%s</div>`+"\r\n", html.EscapeString(title))
	for _, it := range items {
		merchantMailItemRow(b, it, imgs)
	}
}

// merchantMailItemRow 输出单个商品行:图 + 名称 + (类型 · 价格 · 限购)。
func merchantMailItemRow(b *strings.Builder, it merchantItem, imgs *[]merchantMailImg) {
	imgTag := ""
	if src := merchantMailItemImg(it, imgs); src != "" {
		imgTag = `<img src="` + src + `" alt="" style="width:44px;height:44px;border-radius:8px;object-fit:cover;flex:none;background:#f7efdb;">`
	}
	var meta []string
	if k := merchantKindText(it.Kind); k != "" {
		meta = append(meta, k)
	}
	meta = append(meta, fmt.Sprintf("%d 洛克贝", it.Price))
	if it.Limit > 0 {
		meta = append(meta, fmt.Sprintf("限购 %d", it.Limit))
	}
	b.WriteString(`<div style="display:flex;align-items:center;gap:10px;background:#fff;border:1px solid #f2e6c8;border-radius:10px;padding:9px 12px;margin:7px 0;">` + "\r\n")
	if imgTag != "" {
		b.WriteString(imgTag + "\r\n")
	}
	b.WriteString(`<div style="flex:1;min-width:0;">` + "\r\n")
	b.WriteString(`<div style="font-size:14px;font-weight:700;color:#3a2a14;">` + html.EscapeString(it.Name) + `</div>` + "\r\n")
	if len(meta) > 0 {
		b.WriteString(`<div style="font-size:12px;color:#8a6d3b;margin-top:2px;">` + html.EscapeString(strings.Join(meta, " · ")) + `</div>` + "\r\n")
	}
	b.WriteString(`</div></div>` + "\r\n")
}

// merchantMailItemImg 商品图 URL:http(s) 外链原样;本地 embed 路径读 webp 以 CID 收集。
func merchantMailItemImg(it merchantItem, imgs *[]merchantMailImg) string {
	if it.Image == "" {
		return ""
	}
	if strings.HasPrefix(it.Image, "http://") || strings.HasPrefix(it.Image, "https://") {
		return html.EscapeString(it.Image)
	}
	if data, err := fs.ReadFile(gamedata.ImageFS(), it.Image); err == nil && len(data) > 0 {
		cid := fmt.Sprintf("merchant%d", len(*imgs)+1)
		*imgs = append(*imgs, merchantMailImg{cid: cid, data: data})
		return "cid:" + cid
	}
	return ""
}
