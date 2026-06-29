// Package gamedata 提供从游戏解包数据(nrc/bin)提取的 id->中文名 查找表(编译期 embed)。
package gamedata

import (
	"embed"
	"encoding/json"
	"io/fs"
	"sort"
	"strconv"
)

//go:embed data/names.json
var namesJSON []byte

// 宠物图片(webp,由 scripts/gen_images.py 从 FModel PNG 转出);未生成时仅含占位 .gitkeep。
//
//go:embed all:data/img
var imageFS embed.FS

// ImageFS 返回 embed 的宠物图片文件系统,路径形如 HeadIcon/3001.webp(见 PetImage)。
func ImageFS() fs.FS {
	sub, err := fs.Sub(imageFS, "data/img")
	if err != nil {
		return imageFS
	}
	return sub
}

// Medal 是奖牌的名称与描述。
type Medal struct {
	Name string `json:"name"`
	Desc string `json:"desc"`
}

// imageEntry 是 petbase 形态的图片文件名(头像为数字,全身图去掉 JL_ 前缀)。
type imageEntry struct {
	H  string `json:"h"`  // 小头像文件名
	B  string `json:"b"`  // 大头像文件名
	P  string `json:"p"`  // 全身图拼音键(实际文件名为 JL_<p>)
	PS string `json:"ps"` // 全身缩略拼音键
}

// PetImage 是宠物各尺寸图片的相对路径(相对图片根,空串表示缺图)。
type PetImage struct {
	Head          string `json:"head"`          // 小头像 HeadIcon/<n>.webp
	BigHead       string `json:"bigHead"`       // 大头像 BigHeadIcon256/<n>.webp
	Portrait      string `json:"portrait"`      // 全身图 Pet1024/JL_<x>.webp
	PortraitSmall string `json:"portraitSmall"` // 全身缩略 Pet256/JL_<x>.webp
}

// DB 是只读名称查找库。
type DB struct {
	species      map[string]string
	nature       map[string]string
	skillDamType map[string]string
	talentRate   map[string]string
	partnerMark  map[string]string
	speciality   map[string]string
	medal        map[string]Medal
	opcodes      map[uint16]string
	natureEffect map[string]NatureEffect
	images       map[string]imageEntry // petbase_id -> 文件名
	imageBase    map[string]string     // conf_id -> petbase_id(base==自身者不入表)
}

// NatureEffect 是性格对六维的增减维度(六维编号 1-6:1生命2物攻3魔攻4物防5魔防6速度)。
type NatureEffect struct {
	Pos int32 `json:"pos"` // +10% 维度
	Neg int32 `json:"neg"` // -10% 维度
}

// Load 加载 embed 的名称表。
func Load() (*DB, error) {
	var raw struct {
		Species      map[string]string       `json:"species"`
		Nature       map[string]string       `json:"nature"`
		SkillDamType map[string]string       `json:"skill_dam_type"`
		TalentRate   map[string]string       `json:"talent_rate"`
		PartnerMark  map[string]string       `json:"partner_mark"`
		Speciality   map[string]string       `json:"speciality"`
		Medal        map[string]Medal        `json:"medal"`
		Opcodes      map[string]string       `json:"opcodes"`
		NatureEffect map[string]NatureEffect `json:"nature_effect"`
		Images       map[string]imageEntry   `json:"images"`
		ImageBase    map[string]uint32       `json:"image_base"`
	}
	if err := json.Unmarshal(namesJSON, &raw); err != nil {
		return nil, err
	}
	opcodes := make(map[uint16]string, len(raw.Opcodes))
	for k, v := range raw.Opcodes {
		if n, err := strconv.ParseUint(k, 10, 16); err == nil {
			opcodes[uint16(n)] = v
		}
	}
	imageBase := make(map[string]string, len(raw.ImageBase))
	for k, v := range raw.ImageBase {
		imageBase[k] = key(v)
	}
	return &DB{
		species:      raw.Species,
		nature:       raw.Nature,
		skillDamType: raw.SkillDamType,
		talentRate:   raw.TalentRate,
		partnerMark:  raw.PartnerMark,
		speciality:   raw.Speciality,
		medal:        raw.Medal,
		opcodes:      opcodes,
		natureEffect: raw.NatureEffect,
		images:       raw.Images,
		imageBase:    imageBase,
	}, nil
}

// PetImage 返回宠物各尺寸图片的相对路径(经 base_id 归并到 petbase 形态;缺图为空串)。
func (db *DB) PetImage(confID uint32) PetImage {
	pid, ok := db.imageBase[key(confID)]
	if !ok {
		pid = key(confID) // base==自身,直接按 conf_id 查 petbase
	}
	e, ok := db.images[pid]
	if !ok {
		return PetImage{}
	}
	var img PetImage
	if e.H != "" {
		img.Head = "HeadIcon/" + e.H + ".webp"
	}
	if e.B != "" {
		img.BigHead = "BigHeadIcon256/" + e.B + ".webp"
	}
	if e.P != "" {
		img.Portrait = "Pet1024/JL_" + e.P + ".webp"
	}
	if e.PS != "" {
		img.PortraitSmall = "Pet256/JL_" + e.PS + ".webp"
	}
	return img
}

// NatureEffect 返回性格的 +10%/-10% 维度(六维编号 1-6;0 表示无)。
func (db *DB) NatureEffect(natureID uint32) NatureEffect { return db.natureEffect[key(natureID)] }

// OpcodeNames 返回 opcode 整数到 ZoneSvrCmd 名称的映射。
func (db *DB) OpcodeNames() map[uint16]string { return db.opcodes }

func key(id uint32) string { return strconv.FormatUint(uint64(id), 10) }

// Species 返回种类名(conf_id)。
func (db *DB) Species(confID uint32) string { return db.species[key(confID)] }

// Nature 返回性格名(nature id)。
func (db *DB) Nature(id uint32) string { return db.nature[key(id)] }

// SkillDamType 返回系别名(SkillDamType enum 整数值)。
func (db *DB) SkillDamType(v int32) string { return db.skillDamType[strconv.FormatInt(int64(v), 10)] }

// TalentRate 返回天分评价名(talent_rank)。
func (db *DB) TalentRate(rank uint32) string { return db.talentRate[key(rank)] }

// PartnerMark 返回标记名(PetPartnerMarkType enum 整数值)。
func (db *DB) PartnerMark(v int32) string { return db.partnerMark[strconv.FormatInt(int64(v), 10)] }

// Speciality 返回特长名(speciality_id)。
func (db *DB) Speciality(id uint32) string { return db.speciality[key(id)] }

// Medal 返回奖牌名称与描述(wear_medal_conf_id)。
func (db *DB) Medal(id uint32) (Medal, bool) { m, ok := db.medal[key(id)]; return m, ok }

// MedalEntry 是带 id 的奖牌(用于全量奖牌墙)。
type MedalEntry struct {
	ID   uint32 `json:"id"`
	Name string `json:"name"`
	Desc string `json:"desc"`
}

// AllMedals 返回全部奖牌,按 id 升序(供前端奖牌墙展示全部奖牌)。
func (db *DB) AllMedals() []MedalEntry {
	out := make([]MedalEntry, 0, len(db.medal))
	for k, v := range db.medal {
		id, _ := strconv.ParseUint(k, 10, 32)
		out = append(out, MedalEntry{ID: uint32(id), Name: v.Name, Desc: v.Desc})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// GenderName 返回性别符号。
func GenderName(g uint32) string {
	switch g {
	case 1:
		return "♂"
	case 2:
		return "♀"
	default:
		return ""
	}
}
