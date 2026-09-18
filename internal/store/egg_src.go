package store

import "time"

// 随机蛋「猜猜孵出谁」的数据源配置:单行表 id=1(范式同 merchant_source)。
//
// **当前状态:表保留、但已无人读写。** v4.2.3 移除咸鱼源后查蛋只剩本地源,面板上的
// 切换卡片与后端切换端点一并删掉了。线上这张表本来就是空的(即从未被切过源)。
//
// 为什么曾经落库而不是只做启动参数:与远行商人同一个理由 —— 数据源是运行期就要能
// 切换的运维选项(第三方接口随时可能失效或开始要令牌),管理员在面板上切一次就得
// 永久生效。放启动参数意味着每次切换都要改 systemd 配置并重启服务,而重启会打断
// 正在解密的游戏连接。
//
// 访问器按决定保留(不动 schema、不写迁移);将来若接入第二个源,这张表与下面两个
// 方法可直接复用。空串 = 未配置,由调用方回退默认源。

// EggSource 返回当前配置的数据源标识;没配置过时返回空串。
func (s *Store) EggSource() string {
	var src string
	// 读取失败(表不存在/无该行)按「未配置」处理,由调用方回退默认值 ——
	// 这张表是后加的,老库在下次写入前没有这一行属正常。
	_ = s.rdb.QueryRow(`SELECT source FROM egg_source WHERE id=1`).Scan(&src)
	return src
}

// SetEggSource 写入数据源标识(空串=恢复默认),覆盖既有配置。
func (s *Store) SetEggSource(src string) error {
	_, err := s.db.Exec(`INSERT INTO egg_source(id, source, updated_at) VALUES(1,?,?)
		ON CONFLICT(id) DO UPDATE SET source=excluded.source, updated_at=excluded.updated_at`,
		src, time.Now().Unix())
	return err
}
