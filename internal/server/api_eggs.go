package server

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/zxsos/roco-go/internal/pet"
	"github.com/zxsos/roco-go/internal/store"
)

// handleEggs 返回当前账号背包里的精灵蛋(库里存的就是背包现状,破壳/送人的行已删)。
//
// 参数:
//
//	search=          按蛋名/物种名模糊
//	sort=quality|obtained  order=asc|desc(复刻游戏内背包的两种排序,见 docs/data.md 3.6)
//
// 页面不分标签页:一次取回全部。在孵的蛋留在最前且不参与背包排序(它们属于孵蛋器),
// 但自己按槽位序排一遍(入孵时刻升序,与背包次序无关,见 pet.SortHatchingEggs)。
// **排序只作用于仓库那部分**:客户端也是先把在孵的蛋摘掉(IsRemoveEggItem)再 table.sort,
// 喂给排序的列表不同,同键蛋的落位就不同。
// 前端还会按孵化进度把标记蛋再分成「在孵(未满)/ 已孵化(满)」两段(残留标记蛋进度恒满,
// 见 web/src/pages/eggs/EggList.jsx)——进度是前端外推的,后端无法判断,故这里只给 hatching 标志。
func (s *Server) handleEggs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sc := s.store.For(s.acct(r))
	eggs, err := sc.ListEggs(store.EggFilter{Search: q.Get("search")})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for i, e := range eggs {
		// 按当前名称库重算(旧行可能是本工具还没有异色标记/排序键时写的),
		// 顺带补上要等双亲快照才算得出的推测嗓音与奖牌。
		eggs[i] = pet.RefreshEggView(e, s.db)
	}
	// 排序键来自上面的重算,故排在其后。在孵的蛋留在最前且不参与背包排序(它们属于孵蛋器),
	// 但自己按槽位顺序排一遍——槽位是入孵时刻升序,与背包次序无关(见 pet.SortHatchingEggs)。
	hatching, bag := []*pet.EggView{}, []*pet.EggView{}
	for _, e := range eggs {
		if e.Hatching {
			hatching = append(hatching, e)
		} else {
			bag = append(bag, e)
		}
	}
	pet.SortHatchingEggs(hatching)
	pet.SortEggs(bag, q.Get("sort"), q.Get("order") == "asc")
	eggs = append(hatching, bag...)
	// 孵化倍率**整个响应一份**而非逐蛋:倍率是全局的,不逐蛋(实测三颗不同 maxSecs
	// 的蛋同秒各 +10s,统一 5.00)。
	//
	// 两个来源,优先级:玩家实测 > 活动倍率时间表。
	//   - 活动倍率(pet.HatchRate):加速日时间表,按时刻算(离线也准,无冷启动),
	//     只有 1x / 5x —— **不含**移动等在线加成,那部分没有可信定值,估了会虚报。
	//   - 实测倍率(hatchSpeed.rate):玩家在页面点「开始测速」后开两次孵蛋器,后端
	//     取两次进度差分。测出的值天然包含此刻的全部加成,是实测而非估计。
	//
	// 前端拿到后:rate>0 就用它外推,否则回落到 activityRate。
	acc := s.acct(r)
	speed, err := s.store.For(acc).GetHatchSpeed()
	if err != nil {
		log.Printf("读孵化测速状态失败: %v", err)
	}
	rate := pet.HatchRate(time.Now().Unix())
	if speed.State == store.HatchSpeedDone && speed.Rate > 0 {
		rate = speed.Rate
	}
	writeJSON(w, map[string]any{
		"eggs":         eggs,
		"hatchRate":    rate,
		"hatchSpeed":   speed,
		"activityRate": pet.HatchActivityRate(time.Now().Unix()),
	})
}

// handleHatchSpeed 开始 / 取消孵化测速(POST /api/hatch/speed)。
//
// body: {"on": true|false} —— true 重置并等第一次采样,false 取消回到 idle。
//
// 为什么需要它:进度只在开孵蛋器(0x0312)时下发、没有被动推送,后端要差分就必须
// 等玩家自己触发两次。故测速**由玩家发起**:他点了按钮,后端才知道接下来两次
// 0x0312 是要配对的,否则会把任意两次开面板当成一次测速(玩家可能只是去看进度)。
func (s *Server) handleHatchSpeed(w http.ResponseWriter, r *http.Request) {
	var body struct {
		On bool `json:"on"`
	}
	json.NewDecoder(r.Body).Decode(&body) // 解析失败按 on=false 处理(取消)
	acc := s.acct(r)
	var err error
	if body.On {
		err = s.store.For(acc).ArmHatchSpeed(time.Now().Unix())
	} else {
		err = s.store.For(acc).CancelHatchSpeed()
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	st, err2 := s.store.For(acc).GetHatchSpeed()
	if err2 != nil {
		http.Error(w, err2.Error(), http.StatusInternalServerError)
		return
	}
	// 广播一次:同账号可能开着多个页面(手机 + 电脑),点了按钮要都跟着变
	s.hub.Broadcast("eggs", acc, map[string]any{"account": acc})
	writeJSON(w, st)
}
