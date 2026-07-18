package gamedata

import "strconv"

// UI 图标查找:语义键/枚举值 → embed 的 webp 相对路径(生成流程见 scripts/gen_icons.py)。

// iconPath 由「原始文件名」拼出 <group>/<name>.webp;name 为空或未 embed 时返回空串。
func (db *DB) iconPath(group, name string) string {
	if name == "" {
		return ""
	}
	p := group + "/" + name + ".webp"
	if !db.imgFiles[p] {
		return ""
	}
	return p
}

// filterIcon 查 filter_icons 索引(组名 + 枚举整数值)拿原名,拼 filter/<原名>.webp。
func (db *DB) filterIcon(group string, v int32) string {
	return db.iconPath("filter", db.filterIcons[group][strconv.FormatInt(int64(v), 10)])
}

// SkillDamTypeIcon 返回系别(属性)图标路径(SkillDamType enum 整数值)。
func (db *DB) SkillDamTypeIcon(v int32) string { return db.filterIcon("skill_dam_type", v) }

// SkillDamTypeIcons 返回系别中文名 -> 图标路径(供前端系别筛选按钮显示图标)。
func (db *DB) SkillDamTypeIcons() map[string]string {
	out := make(map[string]string, len(db.skillDamType))
	for k, name := range db.skillDamType {
		if v, err := strconv.ParseInt(k, 10, 32); err == nil {
			if p := db.SkillDamTypeIcon(int32(v)); p != "" {
				out[name] = p
			}
		}
	}
	return out
}

// AttributeTypeIcon 返回六维属性图标路径(AttributeType enum 整数值;1-6 即六维编号,
// 79-84 为对应增益类)。
func (db *DB) AttributeTypeIcon(v int32) string { return db.filterIcon("attribute_type", v) }

// PartnerMarkIcon 返回搭档标记图标路径(PetPartnerMarkType enum 整数值)。
func (db *DB) PartnerMarkIcon(v int32) string { return db.filterIcon("partner_mark", v) }

// BloodIcon 返回血脉主图标路径 blood/<原名>.webp(PET_BLOOD_CONF.blood,1-24);无图或未 embed 时空串。
func (db *DB) BloodIcon(bloodID uint32) string {
	return db.iconPath("blood", db.bloodIcons[key(bloodID)])
}

// BloodName 返回血脉中文短名(普通/草/火…;PET_BLOOD_CONF.blood_name)。
func (db *DB) BloodName(bloodID uint32) string { return db.bloodNames[key(bloodID)] }

// StaticIcon 返回杂项静态图标路径 static/<原名>.webp(语义键:shiny/colorful/shiny_colorful/
// pollution/partner_frame);未知键或未 embed 时空串。
func (db *DB) StaticIcon(sem string) string { return db.iconPath("static", db.staticIcons[sem]) }

// MedalIcon 返回奖牌小图路径 medal/<原名>.webp(MEDAL_CONF.id → icon(BagItem));无图或未 embed 时空串。
func (db *DB) MedalIcon(medalID uint32) string {
	return db.iconPath("medal", db.medalIcons[key(medalID)])
}

// POIIcon 返回 POI 图层的图标路径 worldmap/<原名>.webp;未 embed 时空串。
func (db *DB) POIIcon(kind POIKind) string { return db.iconPath("worldmap", kind.Icon) }
