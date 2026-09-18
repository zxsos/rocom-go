package store

import "time"

// 远行商人数据源配置:单行表 id=1(范式同 admin 表)。
//
// **当前状态:表保留、但已无人读写。** v4.2.3 移除咸鱼源后商人只剩好游快爆一个源,
// 面板上的切换卡片与后端切换端点一并删掉了,故这张表的历史值(如 'haoyou')只是
// 陈旧的运维记录。
//
// 为什么曾经落库而不是只做启动参数:数据源是运行期就要能切换的运维选项(第三方接口
// 随时可能失效或开始要令牌),管理员在面板上切一次就得永久生效。放启动参数意味着
// 每次切换都要改 systemd 配置并重启服务,而重启会打断正在解密的游戏连接。
//
// 访问器按决定保留(不动 schema、不写迁移);若将来接入第二个源,这张表与下面两个
// 方法可以直接复用 —— 但届时必须同时补回「切源清槽缓存」的逻辑,见本文件末尾的注。

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

// 注:这里曾有 ClearMerchantSlots(切换数据源时清空全部槽缓存),随切换功能一并移除。
// 重新接入第二个源时必须把它补回来:两个源的货单格式不同,留着另一份会被当成当前
// 源的数据显示,页面顶部的来源标注也在说谎。错的货单比没有货单更糟。
//
// 清的时候只清 merchant_slots,**不要清** merchant_notified:那是「这批商品已通知过
// 谁」的记录,按商品名去重、与源无关;清掉反而会让同一批商品对订阅者再发一遍提醒。
