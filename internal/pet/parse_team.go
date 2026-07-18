package pet

import (
	"google.golang.org/protobuf/proto"

	"github.com/whoisnian/rocom-capture/internal/pb"
	"github.com/whoisnian/rocom-capture/internal/wire"
)

// PTTBigWorld 是 PlayerTeamType.PTT_BIG_WORLD(大世界队伍 team_type)。
const PTTBigWorld = 1

// TeamEntry 是一只宠物在大世界队伍中的位置(team_idx 第几队,pos 队内位置 0 起,每队 6 位)。
type TeamEntry struct {
	Gid     uint32
	TeamIdx int32
	Pos     int32
}

// ParseTeams 在 body 里找大世界队伍(team_type==PTT_BIG_WORLD)的 PetTeamInfo,
// 展开为 gid->(team_idx, pos) 列表。取含宠物数最多的大世界候选以排除误解析。
func ParseTeams(body []byte) []TeamEntry {
	var best *pb.PetTeamInfo
	bestN := 0
	wire.Walk(body, func(v []byte) bool {
		var ti pb.PetTeamInfo
		if proto.Unmarshal(v, &ti) != nil || len(ti.GetTeams()) == 0 {
			return true
		}
		if ti.GetTeamType() == PTTBigWorld {
			n := 0
			for _, t := range ti.GetTeams() {
				for _, pi := range t.GetPetInfos() {
					if pi.GetPetGid() != 0 {
						n++
					}
				}
			}
			if n > bestN {
				bestN, best = n, &ti
			}
		}
		return true
	})
	if best == nil {
		return nil
	}

	// 队号取 teams[] 数组下标(实测 PetTeam.team_idx 恒 0、无队名,故以数组顺序为准)。
	var out []TeamEntry
	for ti, t := range best.GetTeams() {
		for pos, pi := range t.GetPetInfos() {
			if g := pi.GetPetGid(); g != 0 {
				out = append(out, TeamEntry{Gid: g, TeamIdx: int32(ti), Pos: int32(pos)})
			}
		}
	}
	return out
}
