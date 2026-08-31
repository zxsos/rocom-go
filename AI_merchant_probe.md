# AI_merchant_probe.md —— 远行商人「货单滞后探测」临时测试模式

> **本文件是临时沟通文件**,拿到数据、定下 `merchantRefetch` / `merchantRefetchWin` 之后
> 连同探测代码一起删除(删除清单见文末 §5)。长期结论请沉淀到 `docs/`。

## §1 为什么要探测

远行商人数据来自第三方 API(`https://apii.xianyuw.cn/api/v1/rocom-merchant`)。
**第三方自己有缓存**——轮次整点(8/12/16/20)之后,它返回的货单往往还是**上一轮**的,
要过一阵才切成新轮。

`internal/server/merchant.go` 里这两个常量就是为这个滞后准备的,但它们是**拍的**:

```go
merchantRefetch    = 10 * time.Minute  // 进行中的槽两次回源的最小间隔
merchantRefetchWin = 90 * time.Minute  // 槽开始后允许重查的窗口,过了就固化
```

2026-08-30 实测:20:00 开轮,第三方那份快照到 **20:56** 才补全 3 件轮次专属货
(魔力果/火系粉尘/萌系粉尘),滞后 **56 分钟**——已经逼近 90 分钟窗口的边缘。
说明这两个常量的取值没有依据。

**探测的目的就是把这个滞后的真实分布测出来**,让这两个常量有据可依。

## §2 怎么跑

加一个启动参数即可,其余参数不变:

```bash
# auto:对准下一个档期整点(8/12/16/20)自动开始 —— 服务可以先起着等
./rocom-capture ... -merchant-probe=auto

# now:立即开始(把「现在」当作整点)—— 用于随时手动测一轮
./rocom-capture ... -merchant-probe=now
```

要点:

- **必须同时配 `-egg-api-key` 与 SMTP**(`-merchant-smtp-user` / `-merchant-smtp-pass`),
  否则探测拿不到数据、也发不出邮件。
- 服务要**一直开着**跑完整个探测(最长 90 分钟),中途退出数据就断了。
- 探测期间常规功能照常:页面能看、订阅能发。定时补查(`merchantEnsure`)被短路,
  回源由探测独占,避免污染时间线。
- 启动时日志会打印对准了哪个档期、距现在多久,**务必核对**这一行。

探测节奏(在 `internal/server/merchant_probe.go` 的 `probeIntervals`):

```
整点 +0s  +5s  +15s  +20s  +30s  +35s  +45s  +50s | +60s(下一分钟)...
```

即 5s / 10s 交替,**每分钟 8 次**。交替而非固定间隔,是为了在同样次数预算下让头 30 秒更密
——滞后若发生在秒级,固定 10s 会看不清。

## §3 数据在哪

### 3.1 邮件(给人看的,共两种)

**(a) 新货提醒**——本轮新货上架时照常发,**正文副标题下多了一行**:

```
数据获取于 20:03:20 (整点后 3 分 20 秒)
```

滞后 ≥ 1 分钟时这一行标红,这就是"第三方滞后多久"的直接读数。

注意:**探测期间不会误发上一轮的旧货**——`merchantNotify` 里「本营业日更早轮」的
`seen` 集合会挡住它们,只有真正切到本轮新货时才发信。

**(b) 探测汇总**——探测结束时**必定**发一封,标题带 `[探测汇总]` 前缀,内容形如:

```
远行商人货单滞后探测汇总

- 探测档期: 2026-08-31 20:00(北京时间)
- 探测次数: 39 次(5s/10s 交替,8 次/分)
- 结论: 货单在整点后 3 分 20 秒 切换
- 切换时刻: 20:03:20

第三方滞后的真实值就是上面这个数。它是 merchantRefetch /
merchantRefetchWin 该设多大的唯一依据(见 merchant.go)。

- 原始时间线: /path/to/merchant-probe-20260831-2000.log
```

**转交邮件时请优先转这一封**——它在任何情况下都会发(切没切单都有),且直接给出结论。
新货提醒那封则要「货单已切换」才存在。

汇总邮件走 `s.smtp` 直接发,不经 `merchantNotify`,因此**不会写 `merchant_notified`**、
不污染商品级去重。它发给全部订阅者;若无人订阅或 SMTP 未配置,则只打日志。

### 3.2 日志文件(给 AI 分析的原始数据,**更重要**)

工作目录下的 `merchant-probe-<YYYYMMDD-HHMM>.log`,例如 `merchant-probe-20260831-2000.log`。
每次探测后立即 `f.Sync()`,崩了也不丢已采数据。

```
# 远行商人货单滞后探测
# 档期整点: 2026-08-31 20:00:00 (Unix 1756641600, 时区 CST)
# 探测节奏: 5s / 10s 交替 = 每分钟 8 次;上限 1h30m0s;切换后延测 5m0s
#
# 列: 探测时刻 | 距整点(秒) | 回源 | 有货 | 商品数 | 指纹 | time_label | 商品名
# 读法: 指纹发生变化的那一行 = 第三方把货单切到本轮的时刻,
#       那一行的「距整点」就是滞后秒数,即 merchantRefetchWin 的下界。
#       回源=fail 表示网络/HTTP 层失败(该次未写库,指纹沿用上次)。

20:00:00.142  |    +0.1 | ok   | 有货 |     4 | 3f2a1b | 16:00-20:00 | 残缺魔镜,魔力果,火花兽,洛克贝袋
20:00:05.318  |    +5.3 | ok   | 有货 |     4 | 3f2a1b | 16:00-20:00 | 残缺魔镜,魔力果,火花兽,洛克贝袋
...
20:03:20.771* |  +200.8 | ok   | 有货 |     7 | d4e5f6 | 20:00-24:00 | 残缺魔镜,魔力果,火系粉尘,萌系粉尘,...
```

- 行首 `*` = 该行指纹与基线(**首次成功探测**的结果)不同,即切单。
- `time_label` 列是判定货单归属哪个时段的直接依据(见 §4 的 B 方案)。
- 指纹 = 商品名排序后取 sha256 前 6 位;排序是为了消除第三方返回顺序不稳定的影响。

### 3.3 退出条件

- 检测到切单后再延测 **5 分钟**(`probeSettle`)确认稳定,然后结束。
- 一直没切单则跑满 **90 分钟**(`probeMaxDuration`)结束。

结束时日志会打印结论行,例如:

```
远行商人探测结束: 货单在整点后 200 秒切换(20:03:20),共 39 次探测,日志 /path/to/xxx.log
```

## §4 拿到数据后怎么改(给下个 AI 的判断规则)

先看日志里**第一个 `*` 行的「距整点」**,记为 L。再看 `*` 出现的**次数**。

### 情形 A: L 很小(< 60 秒)

第三方几乎是实时刷新的。那么 `merchantRefetchWin = 90min` 严重过剩,
可缩到 10~15 分钟;`merchantRefetch = 10min` 则**太粗**——整点首查若恰好撞上
切换前的一瞬,下一次重查要等 10 分钟。建议 `merchantRefetch` 降到 2~3 分钟。

### 情形 B: L 中等(1~30 分钟)

滞后可预期。`merchantRefetchWin` 设为 **L + 余量**(至少 2 倍 L,且 ≥ 30 分钟);
`merchantRefetch` 设为 **L 的 1/3 左右**,保证窗口内至少能重查 3 次。

### 情形 C: L 很大(> 30 分钟)或跑满 90 分钟都没切单

**不要简单地把 `merchantRefetchWin` 调大。** 这会引入一个更糟的问题:

整点那次回源拿到的是**上一轮**的货单,而现在的代码会把它**写进本轮槽**
(`merchantFetch` 里 `PutMerchantSlot(slotStart, ...)`),于是:

1. 页面上本轮显示的是上一轮的货(整点后很长一段时间是错的);
2. `merchantNotify` 的 `seen` 只挡「本营业日更早轮的缓存」——若更早轮没查过
   (服务当轮才启动),旧货会被当成新货**误报**给订阅者;
3. 误报后 `merchant_notified` 记下商品名,真货单里同名的全天货会被**永久吞掉**。

所以情形 C 必须先做**货单归属校验**,再谈窗口:

> `merchantFetch` 写入本轮槽之前,用每个商品的 `time_label`(缺失时回落
> `start_time`/`end_time`)判断它属于哪个时段——**代码里已有现成的 `merchantItemSlots`**
> (`internal/server/merchant_mail.go`,前端 `web/src/pages/merchant/format.js` 的
> `parseSlots` 是同一套逻辑),现在只用于邮件分组,没用于判定归属。
>
> - 只保留覆盖本轮时段的商品写入本轮槽(全天货覆盖四段,自然通过);
> - 一件都不属于本轮 → 判定为陈旧货单,**不写库、不通知**,只 `TouchMerchantSlot`
>   推回源时刻,等下次重查。

这条做完之后,「已结束的槽永不回源」(`merchantShouldFetch` 里 `merchantSlotLive`
那条硬规则)就不再必要了——因为能判断手里的货单属于哪个槽,不会写错。
届时可以在轮次结束后继续回源补历史槽,把迟到很久的货单补进去。

### 情形 D: `*` 出现多次(切换后还有变化)

说明**补货是分批到达的**。这印证了按**商品**去重(而非按槽)的必要性——
`merchant_notified` 记的是商品名清单,`merchantNotify` 每次只发「上次没发过的」。
若发现分批补的货仍被漏掉,检查 `merchantClaim` 的 10 分钟冷却是否在挡(生产模式下它是必要的)。

### 另外:试试 `refresh=true`

`merchantFetch` 现在写死 `refresh=false`。第三方大概率支持 `refresh=true` 绕过它自己的缓存。
**如果支持,滞后问题的根因直接消失**,上面 A/B/C 都不用做了。这个验证成本极低,建议先做。

## §5 删除清单(拿到数据后)

探测代码全部集中在 `internal/server/merchant_probe.go`,但它对生产代码有 5 处侵入。
按下表删干净:

| # | 位置 | 删什么 |
| --- | --- | --- |
| 1 | `internal/server/merchant_probe.go` | **整个文件删除** |
| 2 | `cmd/rocom-capture/main.go` | `-merchant-probe` 的 flag 定义(1 行);`if *probe != "" { srv.StartMerchantProbe(...) }` 块(3 行) |
| 3 | `internal/server/merchant_notify.go` | `merchantClaim` 开头 `if merchantProbeOn.Load() { return true }`(含注释 4 行) |
| 4 | `internal/server/merchant.go` | `merchantEnsure` 开头 `if merchantProbeOn.Load() && len(force) == 0 { return }`(含注释 3 行) |
| 5 | 邮件「数据获取时间」 | 可选:保留无害且对排查有用。要删则改 `internal/server/merchant_mail.go` 的 `merchantMailContent`(去掉 `slotStart, fetchedAt` 两个参数与那个 `if` 块,删掉 `fmtDuration`),并同步回退 `internal/server/merchant_mail_test.go` 里两处调用(去掉两个 `time.Time{}` 参数与 `time` import)、`internal/server/merchant_notify.go` 里的调用点与 `fetchedAt` 取值 |
| 6 | `AI_merchant_probe.md` | **本文件删除**;结论沉淀到 `docs/data.md` 或 `docs/` 下新文件 |

### 验证删干净了

```bash
grep -rn "merchantProbeOn\|StartMerchantProbe\|probeIntervals\|merchant-probe" internal/ cmd/
```

应无输出。`go build ./...` 与 `go test ./internal/server/` 都应通过。

注意:删掉第 3、4 项后,`merchantEnsure` / `merchantClaim` 必须恢复到探测前的语义
(定时补查恢复、认领冷却恢复),否则会出现重复发信或白烧 token。

## §6 本次探测的已知限制

- 探测当天第三方可能被限流(每分钟 8 次、最长 90 分钟)。日志里的 `fail` 行会体现,
  若 `fail` 密集则说明打太狠了,下轮应放宽节奏。
- 只测一个档期(20:00)。滞后在不同档期可能不同(例如开张首轮 8:00 与末轮 20:00
  行为未必一致),有条件应多测几轮。
- `probeMaxDuration = 90min` 是上限。若日志显示跑满 90 分钟仍未切单,
  需要把上限调大重测一轮(改常量重新编译即可)。
