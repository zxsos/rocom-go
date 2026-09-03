package store

import "time"

// 随机蛋「猜猜孵出谁」的数据源配置:单行表 id=1(范式同 merchant_source),
// 同一时刻只有一个源生效。
//
// 为什么落库而不是只做启动参数:与远行商人同一个理由 —— 数据源是运行期就要能切换的
// 运维选项(第三方接口随时可能失效或开始要令牌),管理员在面板上切一次就得永久生效。
// 放启动参数意味着每次切换都要改 systemd 配置并重启服务,而重启会打断正在解密的
// 游戏连接。
//
// 空串 = 未配置,由调用方回退到默认源(默认值只在 server 侧定义一处,避免两处不一致)。

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
