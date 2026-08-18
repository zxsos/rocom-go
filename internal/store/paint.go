package store

import "time"

// PaintGrid 是一张涂地覆盖位图(见 docs/data.md 3.8):w*h 个格子,每格一位(1=已扫过),
// 每字节 8 格、低位在前。按账号 + 场景(scene_res)+ 分层各存一张,格子尺寸由 server 定。
type PaintGrid struct {
	W, H  int
	Cells []byte
}

// LoadPaint 取某账号某场景某层的覆盖位图;无记录返回 ok=false。
func (s *Store) LoadPaint(account string, res, layer int32) (PaintGrid, bool) {
	var g PaintGrid
	err := s.rdb.QueryRow(`SELECT w, h, cells FROM paint WHERE account=? AND res=? AND layer=?`,
		account, res, layer).Scan(&g.W, &g.H, &g.Cells)
	if err != nil || g.W <= 0 || g.H <= 0 {
		return PaintGrid{}, false
	}
	return g, true
}

// SavePaint 覆盖写入某账号某场景某层的覆盖位图。
func (s *Store) SavePaint(account string, res, layer int32, g PaintGrid) error {
	_, err := s.db.Exec(`INSERT INTO paint(account, res, layer, w, h, cells, updated_at) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(account, res, layer) DO UPDATE SET w=excluded.w, h=excluded.h,
			cells=excluded.cells, updated_at=excluded.updated_at`,
		account, res, layer, g.W, g.H, g.Cells, time.Now().Unix())
	return err
}

// ClearPaint 删除某账号某场景某层的覆盖位图(涂地重置)。
func (s *Store) ClearPaint(account string, res, layer int32) error {
	_, err := s.db.Exec(`DELETE FROM paint WHERE account=? AND res=? AND layer=?`, account, res, layer)
	return err
}
