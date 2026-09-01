package server

// 远行商人「整点抢单」临时模式(-merchant-probe):整点后每 10s 砸一次第三方 API,
// 直到拿到本轮(时段标签匹配当前槽)的货单,或整点后 30 分钟仍无本轮响应则放弃。
//
// 目的:第三方自己有缓存,轮次整点后货单往往滞后才切成新轮(实测 6~56 分钟不等)。
// 这一模式不做任何滞后测量/指纹比对,只是**无脑高频回源**,在滞后期间尽快拿到本轮
// 货单并触发订阅提醒——回源本身幂等(merchantFetch 每次覆盖写槽),多砸几次无害。
//
// 停止条件只有两个:拿到「本轮」货单(商品 time_label 以当前槽起点开头),或 30 分钟超时。
// 整点瞬间第三方仍返回上一轮货单时不应算「响应」,否则会提前停砸而漏掉本轮——
// 用 time_label 前缀匹配当前槽起点即可区分(这是读 API 既有的字段,不是新的测量策略)。
//
// 删除清单(本模式仍是临时测试,结束后整体移除):
//   - 本文件
//   - cmd/rocom-capture/main.go 的 -merchant-probe flag 与调用
//   - AI_merchant_probe.md
// merchant.go / merchant_notify.go 已无探测相关短路(通知走既有按商品/按槽去重,安全)。

import (
	"encoding/json"
	"log"
	"time"
)

const (
	probeEvery    = 10 * time.Second // 整点后每 10s 回源一次
	probeMaxAfter = 30 * time.Minute // 整点后最多砸 30 分钟,仍无本轮响应则放弃
)

// nextProbeSlot 返回下一个档期开始时刻(8/12/16/20 中晚于 now 的第一个)。
// 今天四档都过了则返回明天 8 点(打烊槽 0/4 点不在候选内)。
func nextProbeSlot(now time.Time) time.Time {
	day := merchantDayStart(now)
	for _, st := range merchantDaySlots(day)[:4] { // 只看前四个上架轮,0/4 点是打烊槽
		if st.After(now) {
			return st
		}
	}
	return merchantDaySlots(day.AddDate(0, 0, 1))[0]
}

// StartMerchantProbe 启动抢单。mode:
//   - "auto":对准下一个档期整点(8/12/16/20)开始,服务可以先起着等;
//   - "now" :立即开始(把「现在」当作整点,用于随时手动测一轮)。
func (s *Server) StartMerchantProbe(mode string) {
	now := time.Now()
	var slot time.Time
	switch mode {
	case "now":
		slot = now
	case "auto":
		slot = nextProbeSlot(now)
		if slot.Sub(now) > 6*time.Hour { // 今天四档都没了(0-8 点打烊),等太久没意义
			log.Printf("远行商人探测: 距下一档期 %v,过久,已放弃。改用 -merchant-probe=now 手动测", slot.Sub(now).Truncate(time.Second))
			return
		}
	default:
		log.Fatalf("-merchant-probe 只接受 auto / now,收到 %q", mode)
	}
	log.Printf("远行商人探测已启用: 档期 %s(距现在 %v),整点后每 %v 回源,最多 %v",
		slot.Format("2006-01-02 15:04:05"), slot.Sub(now).Truncate(time.Second), probeEvery, probeMaxAfter)
	go s.runMerchantProbe(slot)
}

// runMerchantProbe 整点后每 10s 回源,直到拿到本轮货单或超时。
//
// 在独立 goroutine 跑,不阻塞服务启动;探测期间页面与订阅提醒照常工作
// (merchantEnsure 的 15 分钟定时补查与探测互补,回源幂等、通知按商品去重,不会重复打扰)。
func (s *Server) runMerchantProbe(slot time.Time) {
	// 对齐到档期整点再开砸(auto 模式可能要等一阵)。
	if wait := time.Until(slot); wait > 0 {
		time.Sleep(wait)
	}
	deadline := slot.Add(probeMaxAfter)
	startLabel := slot.Format("15:04") // "20:00"
	var n int
	for {
		now := time.Now()
		if now.After(deadline) {
			log.Printf("远行商人探测结束: 整点后 %v 内未拿到本轮(%s)货单,放弃", probeMaxAfter, startLabel)
			return
		}
		n++

		s.merchantMu.Lock()
		// 探测的目的就是「盯准点那一刻第三方什么时候给出本轮货单」,必须强制回源
		// (refresh=true):拿缓存快照的话看到的永远是旧数据,探不出真实更新时间。
		ok, empty := s.merchantFetch(slot, true)
		s.merchantMu.Unlock()

		// 拿到非空的本轮货单才算「响应」:读回刚写的槽体,看商品 time_label 是否以
		// 当前槽起点开头(第三方 "20:00-24:00" 对槽标签起点 "20:00")。匹配才停砸并提醒;
		// 否则(整点瞬间仍是上一轮货 / 瞬时空响应)继续每 10s 重试。
		if ok && !empty {
			if _, body, _, has := s.store.GetMerchantSlot(slot.Unix()); has && probeRoundLive(body, startLabel) {
				log.Printf("远行商人探测结束: 整点后 %.0f 秒拿到本轮(%s)货单,共 %d 次回源",
					now.Sub(slot).Seconds(), startLabel, n)
				go s.merchantNotify(slot) // 锁外发信,避免阻塞下一轮回源
				return
			}
		}
		time.Sleep(probeEvery)
	}
}

// probeRoundLive 判断刚写入槽的货单是否属当前轮:任一商品的 time_label 以当前槽起点开头。
// 仅在匹配时才算「本轮已响应」,避免整点瞬间第三方仍返回上一轮货单时被误判为成功、
// 提前停砸而漏掉本轮。time_label 是 API 既有字段,这里只做前缀匹配,不做滞后测量。
func probeRoundLive(body string, startLabel string) bool {
	var out struct {
		Data struct {
			Items []struct {
				TimeLabel string `json:"time_label"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return false
	}
	for _, it := range out.Data.Items {
		if len(it.TimeLabel) >= len(startLabel) && it.TimeLabel[:len(startLabel)] == startLabel {
			return true
		}
	}
	return false
}
