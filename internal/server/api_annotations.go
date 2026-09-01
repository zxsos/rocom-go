package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/whoisnian/rocom-capture/internal/store"
)

// 标注模式 API:对协议里查不到名字的技能/特性 id,玩家提交名字与描述,
// 管理员审核通过后全服可见(众包图鉴)。
//
// 交互流程(试炼页/宠物详情页):
//  1. 前端对未知 id 展示为「未知,点击标注」;
//  2. 点击后搜索候选(技能查 skills.json、特性查 features.json 词典、试炼事件查
//     精灵形态名单),选中并提交;
//  3. 管理员在 #/admin 面板审核,通过后 GET /api/annotations 全服下发。
//
// 数据结构与表设计见 internal/store/annotations.go。

// annotationItem 是下发的一条标注。submitter 只对管理员可见:
// 全服玩家拿到的这份是共享图鉴,不该带「谁标的」的个人信息;
// 管理员面板的待审列表里提交者反而是审核依据,必须给。
type annotationItem struct {
	ID        int64  `json:"id"`
	Kind      string `json:"kind"`           // skill / feature
	Code      int64  `json:"code"`           // 协议 id
	Name      string `json:"name"`           // 名字
	Desc      string `json:"desc,omitempty"` // 描述
	CreatedAt int64  `json:"createdAt"`      // 提交时间(Unix 秒)
}

// annotationAdminItem 是管理员视角的标注(多出 submitter 与审核状态)。
type annotationAdminItem struct {
	annotationItem
	Submitter  string `json:"submitter"`
	Status     string `json:"status"`               // pending / approved / rejected
	ReviewedBy string `json:"reviewedBy,omitempty"` // 审核管理员(空=未审)
	ReviewedAt int64  `json:"reviewedAt,omitempty"` // 审核时间(0=未审)
}

// handleGetAnnotations 返回全服已审核通过的标注(按 kind 过滤)。
// 玩家侧拿它做 id -> 名字 的映射,前端加载时拉一次缓存。
func (s *Server) handleGetAnnotations(w http.ResponseWriter, r *http.Request) {
	kind, ok := annotationKind(r.URL.Query().Get("kind"))
	if !ok {
		http.Error(w, "kind 须为 skill / feature / event", http.StatusBadRequest)
		return
	}
	items, err := s.store.ApprovedAnnotations(kind)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]annotationItem, 0, len(items))
	for _, a := range items {
		out = append(out, annotationItem{
			ID: a.ID, Kind: a.Kind, Code: a.Code, Name: a.Name,
			Desc: a.Desc, CreatedAt: a.CreatedAt,
		})
	}
	// 刻意不带 account:标注是全服共享表(见 store/annotations.go),带上会让人
	// 以为它按账号隔离。ts 是响应时刻,与 trial 等接口一致。
	writeJSON(w, map[string]any{"ts": time.Now().Unix(), "items": out})
}

// handleSubmitAnnotation 玩家提交一条标注。
// 校验:kind 合法、code>0、name 非空且不长;同一玩家对同一 (kind,code,name) 重复提交返回 409。
func (s *Server) handleSubmitAnnotation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind string `json:"kind"`
		Code int64  `json:"code"`
		Name string `json:"name"`
		Desc string `json:"desc"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	kind, ok := annotationKind(req.Kind)
	if !ok {
		http.Error(w, "kind 须为 skill / feature / event", http.StatusBadRequest)
		return
	}
	req.Name = cleanAnnotationName(req.Name)
	req.Desc = strings.TrimSpace(req.Desc)
	if req.Code <= 0 {
		http.Error(w, "code 须为正整数", http.StatusBadRequest)
		return
	}
	if req.Name == "" || len(req.Name) > 50 {
		http.Error(w, "name 不能为空且不超过 50 字", http.StatusBadRequest)
		return
	}
	if len(req.Desc) > 500 {
		http.Error(w, "desc 不超过 500 字", http.StatusBadRequest)
		return
	}

	submitted, err := s.store.SubmitAnnotation(store.Annotation{
		Kind: kind, Code: req.Code, Name: req.Name, Desc: req.Desc,
		Submitter: s.acct(r),
	})
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			http.Error(w, "你已提交过这条标注", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"ok": true,
		"item": annotationAdminItem{
			annotationItem: annotationItem{
				ID: submitted.ID, Kind: submitted.Kind, Code: submitted.Code,
				Name: submitted.Name, Desc: submitted.Desc, CreatedAt: submitted.CreatedAt,
			},
			Submitter: submitted.Submitter,
			Status:    submitted.Status,
		},
	})
}

// handleListPendingAnnotations 管理员查看待审标注(按 kind 过滤)。
func (s *Server) handleListPendingAnnotations(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	kind, ok := annotationKind(r.URL.Query().Get("kind"))
	if !ok {
		http.Error(w, "kind 须为 skill / feature / event", http.StatusBadRequest)
		return
	}
	items, err := s.store.PendingAnnotations(kind)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]annotationAdminItem, 0, len(items))
	for _, a := range items {
		out = append(out, annotationAdminItem{
			annotationItem: annotationItem{
				ID: a.ID, Kind: a.Kind, Code: a.Code, Name: a.Name,
				Desc: a.Desc, CreatedAt: a.CreatedAt,
			},
			Submitter: a.Submitter, Status: a.Status,
			ReviewedBy: a.ReviewedBy, ReviewedAt: a.ReviewedAt,
		})
	}
	writeJSON(w, map[string]any{"items": out})
}

// handleReviewAnnotation 管理员审核一条标注。body: {"approve": true|false}。
// approve=true 时同 code 的其余 pending 自动转 rejected(见 store.ReviewAnnotation)。
func (s *Server) handleReviewAnnotation(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "id 非法", http.StatusBadRequest)
		return
	}
	var req struct {
		Approve bool `json:"approve"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := s.store.ReviewAnnotation(id, req.Approve, "admin"); err != nil {
		if strings.Contains(err.Error(), "标注不存在") {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// 广播让所有在线页面重拉标注:标注是全服共享的,而前端只在 App 挂载时拉一次,
	// 不广播的话审核通过了对别人**不可见** —— 管理员审完,玩家刷新前仍显示裸 id,
	// 众包就失去意义(提交的人看不到反馈,下一个遇到同 id 的人又会再标一次)。
	// account 传空:共享数据不按账号分发,谁收到都该刷新。payload 不带内容 ——
	// 前端只当信号用,重拉 GET /api/annotations 拿全量(量小,没必要做增量)。
	s.hub.Broadcast("annotations", "", map[string]any{"id": id, "approve": req.Approve})
	writeJSON(w, map[string]any{"ok": true})
}

// cleanAnnotationName 规范化标注名字:**移除所有空白字符**。
//
// 起因:wiki 图鉴页为排版在文字间插了空格,玩家照着抄(或手输时带空格)就把
// 「魔法增效」提交成了「魔 法 增 效」,末尾还常带一个空格。这类脏名字进库后
// 全服可见 —— 标注本来就是为了给别人看名字,名字错了等于白标。
//
// 为什么敢直接移除**所有**空白而不只是压缩+去首尾:
// 现有技能名(617)与特性名(190)共 807 个,含空格的为 **0** —— 这是中文游戏,
// 技能/特性名里没有合法的空格。压缩成单空格只能把「魔 法 增 效」变成
// 「魔 法 增 效」(仍是脏的),必须整个去掉才是「魔法增效」。
//
// 用 unicode.IsSpace 而非只看 ' ':能一并处理全角空格(U+3000)与制表符 ——
// 从网页复制文本时全角空格很常见。
// 若将来真出现带空格的合法名字(如英文名),这里要改成「压缩为单空格」,
// 并另行提供去字间空格的入口。
func cleanAnnotationName(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1 // 丢弃
		}
		return r
	}, s)
}

// annotationKind 校验 kind 参数取值(skill / feature / event),返回规范化后的值。
//
// event 是草系试炼特有的第三类:对象是**事件**而非协议里的某个 id ——
// GrassTrialNodeEvent 只给 event_conf_id,对应哪只精灵协议不下发(映射表在游戏
// 配置里,未解包),只能由玩家照着游戏画面标。标完试炼页就能显示头像与名字,
// 还能顺着「精灵 → 特性」表把该精灵的特性 id 一起认出来(见 gamedata.FeatureNameOfBase)。
func annotationKind(v string) (string, bool) {
	switch strings.TrimSpace(v) {
	case "skill":
		return "skill", true
	case "feature":
		return "feature", true
	case "event":
		return "event", true
	}
	return "", false
}

// annotationCandidate 是标注弹窗里的一条搜索候选。
// skill 候选来自 gamedata.SkillCatalog(带协议 id);feature 候选来自
// gamedata.Features(wiki 词典只有名字,没有 id —— 见 features.go 文件头)。
//
// Pets 是「wiki 上哪些精灵带这个特性」。玩家面对 288135 这种裸 id 时其实没有线索,
// 只能凭战斗表现去猜;知道「助燃 = 火花/火神/火舞」往往能立刻印证,故一并下发
// (前端在候选行的悬浮提示里展示,不占列表空间)。
type annotationCandidate struct {
	ID   uint32 `json:"id,omitempty"`
	Name string `json:"name"`
	Desc string `json:"desc,omitempty"`
	// Pets: wiki 上带这个特性的精灵(仅 feature 候选有),给玩家判断用的线索。
	Pets []string `json:"pets,omitempty"`
	// Img: 头像路径(仅 event 候选有)。
	//
	// event 候选是精灵形态,同名形态极多(「棋契陛下」有 10 个形态),
	// 只有名字的话列出来一模一样、无从分辨,玩家只能瞎选 ——
	// 标注数据因此不可信。故这里必须带图。
	Img string `json:"img,omitempty"`
}

// handleAnnotationCandidates 返回标注模式的候选词典(skill=全量技能目录 / feature=特性词典)。
// 供前端标注弹窗搜索选取;数据低频且一次性,全量下发即可。
func (s *Server) handleAnnotationCandidates(w http.ResponseWriter, r *http.Request) {
	kind, ok := annotationKind(r.URL.Query().Get("kind"))
	if !ok {
		http.Error(w, "kind 须为 skill / feature / event", http.StatusBadRequest)
		return
	}
	var out []annotationCandidate
	switch kind {
	case "skill":
		for _, c := range s.db.SkillCatalog() {
			out = append(out, annotationCandidate{ID: c.ID, Name: c.Name})
		}
	case "feature":
		for _, f := range s.db.Features() {
			out = append(out, annotationCandidate{Name: f.Name, Desc: f.Desc, Pets: f.Pets})
		}
	case "event":
		// 精灵形态名单,按图鉴号排序(与游戏内图鉴同序,玩家好找)。
		// 带 base id 是为了让后端能把标注的名字换回形态、取到头像 ——
		// 但标注提交只存名字,故名字必须**逐字**取自这份名单(PetFullName 口径,
		// 形态名用全角括号),手打的同义名反查不到,头像也就出不来。
		for _, p := range s.db.PetForms() {
			out = append(out, annotationCandidate{ID: p.Base, Name: p.Name, Img: p.Img})
		}
	}
	writeJSON(w, map[string]any{"kind": kind, "items": out})
}
