package store

import (
	"database/sql"
	"fmt"
	"time"
)

// 全服共享标注(众包 + 管理员审核,见 server/api_annotations.go)。
//
// 背景:游戏协议只下发 技能 id / 特性 id(288xxx),不带名字。内置库兜不到时
// (技能查不到 skills.json、特性名表本来就没有 id),玩家在 web 端看到一串 id。
// 「标注模式」让玩家提交名字与描述,管理员在面板审核通过后**全服可见**。
//
// 表设计:
//   - kind 区分两类对象(skill=技能 / feature=特性),共用一张表;
//   - 同一 (kind, code) 允许多条待审(不同玩家各有猜测),审核通过时**同 code 的
//     其余 pending 自动转 rejected** —— 答案只能有一个,避免前端显示歧义;
//   - 查询侧永远只看 approved,status 的历史意义是让管理员知道审过什么;
//   - UNIQUE(kind, code, name, submitter) 防止同一玩家对同一 id 反复刷相同答案。
type Annotation struct {
	ID         int64
	Kind       string // skill / feature
	Code       int64  // 协议 id(技能 base_skill_id / 特性 288xxx)
	Name       string
	Desc       string
	Submitter  string
	Status     string // pending / approved / rejected
	CreatedAt  int64
	ReviewedBy string
	ReviewedAt int64
}

const (
	AnnotationPending  = "pending"
	AnnotationApproved = "approved"
	AnnotationRejected = "rejected"
)

// SubmitAnnotation 提交一条标注。同一玩家对同一 (kind,code) 已提交过相同名字时
// 返回错误(调用方转 409),其余情况按待审插入。
func (s *Store) SubmitAnnotation(a Annotation) (Annotation, error) {
	res, err := s.db.Exec(
		`INSERT INTO annotations(kind, code, name, desc, submitter, status, created_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?)`,
		a.Kind, a.Code, a.Name, a.Desc, a.Submitter, AnnotationPending, time.Now().Unix(),
	)
	if err != nil {
		return Annotation{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Annotation{}, err
	}
	a.ID = id
	a.Status = AnnotationPending
	a.CreatedAt = time.Now().Unix()
	return a, nil
}

// ApprovedAnnotations 返回某类(kind)已审核通过的标注,全服可见、数量小,直接全量。
func (s *Store) ApprovedAnnotations(kind string) ([]Annotation, error) {
	return s.queryAnnotations(
		`SELECT id, kind, code, name, desc, submitter, status, created_at, reviewed_by, reviewed_at
		 FROM annotations WHERE kind = ? AND status = ? ORDER BY id`,
		kind, AnnotationApproved,
	)
}

// PendingAnnotations 返回某类(kind)待审核的标注(管理员面板用)。
func (s *Store) PendingAnnotations(kind string) ([]Annotation, error) {
	return s.queryAnnotations(
		`SELECT id, kind, code, name, desc, submitter, status, created_at, reviewed_by, reviewed_at
		 FROM annotations WHERE kind = ? AND status = ? ORDER BY id DESC`,
		kind, AnnotationPending,
	)
}

// ReviewAnnotation 审核一条标注。approve=true 时把它置 approved,同时把同 (kind,code)
// 的其余 pending 全部转 rejected(答案唯一,见文件头注释);approve=false 仅置 rejected。
func (s *Store) ReviewAnnotation(id int64, approve bool, reviewer string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // 提交成功后 Rollback 是空操作

	var kind string
	var code int64
	err = tx.QueryRow(
		`SELECT kind, code FROM annotations WHERE id = ?`, id,
	).Scan(&kind, &code)
	if err == sql.ErrNoRows {
		return fmt.Errorf("标注不存在: id=%d", id)
	}
	if err != nil {
		return err
	}

	now := time.Now().Unix()
	status := AnnotationRejected
	if approve {
		status = AnnotationApproved
		// 同 code 的其他待审全部拒绝:同一 id 只能有一个被认可的答案
		if _, err := tx.Exec(
			`UPDATE annotations SET status = ?, reviewed_by = ?, reviewed_at = ?
			 WHERE kind = ? AND code = ? AND status = ? AND id != ?`,
			AnnotationRejected, reviewer, now, kind, code, AnnotationPending, id,
		); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(
		`UPDATE annotations SET status = ?, reviewed_by = ?, reviewed_at = ?
		 WHERE id = ?`,
		status, reviewer, now, id,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) queryAnnotations(q string, args ...any) ([]Annotation, error) {
	rows, err := s.rdb.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Annotation
	for rows.Next() {
		var a Annotation
		if err := rows.Scan(&a.ID, &a.Kind, &a.Code, &a.Name, &a.Desc, &a.Submitter,
			&a.Status, &a.CreatedAt, &a.ReviewedBy, &a.ReviewedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
