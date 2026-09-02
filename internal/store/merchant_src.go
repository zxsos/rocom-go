package store

import "time"

// 远行商人数据源配置:单行表 id=1(范式同 admin 表),同一时刻只有一个源生效。
//
// 为什么落库而不是只做启动参数:数据源是运行期就要能切换的运维选项(第三方接口
// 随时可能失效或开始要令牌),管理员在面板上切一次就得永久生效。放启动参数意味着
// 每次切换都要改 systemd 配置并重启服务,而重启会打断正在解密的游戏连接。
//
// 空串 = 未配置,由调用方回退到默认源(默认值只在 server 侧定义一处,避免两处不一致)。

// MerchantSource 返回当前配置的数据源标识;没配置过时返回空串。
func (s *Store) MerchantSource() string {
	var src string
	// 读取失败(表不存在/无该行)按「未配置」处理,由调用方回退默认值 ——
	// 这张表是后加的,老库在下次写入前没有这一行属正常。
	_ = s.rdb.QueryRow(`SELECT source FROM merchant_source WHERE id=1`).Scan(&src)
	return src
}

// SetMerchantSource 写入数据源标识(空串=恢复默认),覆盖既有配置。
func (s *Store) SetMerchantSource(src string) error {
	_, err := s.db.Exec(`INSERT INTO merchant_source(id, source, updated_at) VALUES(1,?,?)
		ON CONFLICT(id) DO UPDATE SET source=excluded.source, updated_at=excluded.updated_at`,
		src, time.Now().Unix())
	return err
}

// ClearMerchantSlots 清空全部槽缓存。只在**切换数据源**时用。
//
// 必须清的理由:两个源的货单格式不同(咸鱼源是第三方原始 JSON、好游快爆源是归一化
// 后的 envelope),留着另一份会被当成新源的数据显示,页面顶部的来源标注也在说谎。
// 宁可让历史轮暂时显示「无数据」,也不能拿别的源的数据充数 —— 与「已结束的槽
// 永不回源」是同一条原则:错的货单比没有货单更糟。
//
// 只清槽缓存,**不清** merchant_notified:那是「这批商品已通知过谁」的记录,按商品
// 名去重、与源无关;清掉反而会让同一批商品对订阅者再发一遍提醒。
func (s *Store) ClearMerchantSlots() error {
	_, err := s.db.Exec(`DELETE FROM merchant_slots`)
	return err
}
