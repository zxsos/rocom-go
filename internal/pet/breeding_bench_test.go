package pet

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/whoisnian/rocom-capture/internal/gamedata"
)

// 培育页建议计算的基准护栏。
//
// 为什么留它在仓库里:宠物上千只时 `GET /api/breeding` 会明显变慢,而慢的成因
// (组合数是母本数 × 父本数)不是读代码能看出来的 —— 几百只宠物就上万组合,建议
// 计算若是平方级就会到秒级。基准把这件事钉成可复现的数字,改算法前后各跑一次即可。
//
// 构造的是**假数据**,不加载 gamedata:ChainRef 可以直接给 Evo + Bases 赋值
// (见 ChainRef.Match —— 有链时只查 Bases,Species 不参与),蛋组也只要 Name。

// benchPets 造 n 只宠物:一半 ♀ 一半 ♂,同一形态、同一蛋组,嗓音与体重各不相同。
//
// 同形态同蛋组是**最坏情况**:每个母本都能配上所有父本,组合数 = 母本数 × 父本数,
// 平方级的那部分开销会被完整暴露。真实库里品种分散、组合数反而更少。
func benchPets(n int) []*Pet {
	out := make([]*Pet, 0, n)
	for i := 0; i < n; i++ {
		gender := "♂"
		if i%2 == 0 {
			gender = "♀"
		}
		wp := float64(i % 100)
		out = append(out, &Pet{
			Gid: uint32(1000 + i), Species: "火神", BaseConfID: 3006, Gender: gender,
			Voice: int32(i%200 - 100), Nature: "固执", WeightPct: &wp,
			EggGroups: []gamedata.EggGroup{{Name: "龙"}},
		})
	}
	return out
}

// benchRef 品种引用:与 benchPets 的 BaseConfID 对应。
func benchRef() ChainRef { return ChainRef{Evo: 1, Bases: map[uint32]bool{3006: true}} }

// benchGoal 三项目标全填 —— 打分与性格命中都要走,才是真实开销。
func benchGoal() BreedingGoal {
	v, w := int32(96), 98.0
	return BreedingGoal{Voice: &v, WeightPct: &w, Nature: "固执"}
}

func BenchmarkBreedPool(b *testing.B) {
	for _, n := range []int{200, 1000} {
		pets := benchPets(n)
		ref := benchRef()
		b.Run(fmt.Sprint("pets=", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				BreedPool(ref, pets)
			}
		})
	}
}

func BenchmarkSuggest(b *testing.B) {
	goal := benchGoal()
	// 200 只 = 100 母 × 100 父 = 1 万组合;1000 只 = 500 × 500 = 25 万组合,
	// 单次迭代要到几十毫秒(默认 1s 的 benchtime 只跑得动十几轮),看清趋势够用了。
	for _, n := range []int{200, 1000} {
		pets := benchPets(n)
		pool := BreedPool(benchRef(), pets)
		b.Run(fmt.Sprint("pets=", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				Suggest(pool, goal, 5, nil)
			}
		})
	}
}

// BenchmarkPredict 单个组合的预测开销:目标集合若每次都重新解析,这里就能看出来。
func BenchmarkPredict(b *testing.B) {
	goal := benchGoal()
	m, f := ParentSnapshot(benchPets(1)[0]), ParentSnapshot(benchPets(1)[0])
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Predict(m, f, goal)
	}
}

func BenchmarkVoiceReachOf(b *testing.B) {
	pets := benchPets(1000)
	pool := BreedPool(benchRef(), pets)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		VoiceReachOf(pool, 96)
	}
}

// benchPetBlobs 造 n 条「整只 Pet 的 JSON」,模拟库里 pets.data 列的内容。
//
// 为什么要这个基准:`GET /api/breeding` 每次请求都要把整个宠物库从 data 列读出来
// 再逐条 Unmarshal(见 store.ListAllPets),而培育页只用到其中十来个字段 —— 六维、
// 奖牌、系别、特长、捕捉时间这些培育用不上的东西,反序列化照样要付钱。
// 这条基准把「付了多少钱」钉成可复现的数字,窄投影改完再跑一次即可对比。
//
// 字段按真实宠物的**量级**填(两个系别、两个蛋组、五个勋章 id、六个六维、盒位)——
// 空壳 Pet 的 JSON 只有真实数据的一小部分,测出来会低估好几倍。
func benchPetBlobs(n int) [][]byte {
	out := make([][]byte, 0, n)
	for i := 0; i < n; i++ {
		hp, wp := 61.42, 53.21
		p := Pet{
			Gid: uint32(100000 + i), ConfID: 3006, BaseConfID: 3006,
			Species: "罗隐", Book: 128, Stage: 3, Name: "小火猴", Level: 60,
			NatureID: 2, Nature: "固执", Gender: "♂",
			Types: []string{"火", "格斗"}, TypeIcons: []string{"type/1.png", "type/2.png"},
			BloodID: 3, Blood: "火", BloodIcon: "blood/3.png",
			EggGroups: []gamedata.EggGroup{
				{ID: 1, Name: "龙", Desc: "龙族的蛋组,官方描述文本"},
				{ID: 5, Name: "兽", Desc: "兽族的蛋组,官方描述文本"},
			},
			HeightM: 1.23, WeightKg: 45.6,
			HeightMin: 1, HeightMax: 1.5, HeightPct: &hp,
			WeightMin: 30, WeightMax: 60, WeightPct: &wp,
			Voice:           int32(i%200 - 100),
			TalentRank:      "优秀",
			Medal:           "勇气勋章", MedalDesc: "佩戴后物攻提升若干", MedalIcon: "medal/1.png",
			WearMedalConfID: 12, MedalIDs: []uint32{1, 2, 3, 4, 5},
			PartnerMark: "首领", PartnerMarkIcon: "mark/1.png",
			Speciality: "物攻", SpecialityID: 3,
			CatchTime: 1700000000, Shiny: i%7 == 0,
			Image: gamedata.PetImage{
				Head: "HeadIcon/3006.webp", BigHead: "BigHeadIcon256/3006.webp",
				PortraitSmall: "Pet256/JL_3006.webp",
			},
			Box:        &PetBoxLoc{BoxID: 3, Slot: 12, BoxName: "常用", Mark: "首领"},
			HP:         Stat{Value: 320, TalentLv: 8, Nature: 1},
			Attack:     Stat{Value: 280, TalentLv: 9, Nature: 1},
			Defense:    Stat{Value: 240, TalentLv: 7, Nature: 0},
			SpAttack:   Stat{Value: 150, TalentLv: 5, Nature: 0},
			SpDefense:  Stat{Value: 210, TalentLv: 6, Nature: -1},
			Speed:      Stat{Value: 260, TalentLv: 8, Nature: 0},
		}
		blob, err := json.Marshal(&p)
		if err != nil {
			panic(err)
		}
		out = append(out, blob)
	}
	return out
}

// BenchmarkListAllPetsUnmarshal 当前读法的开销:整条 data → 整只 Pet。
//
// 规模取 1000 与 7545 —— 后者是本库实测的宠物总数(见 store.ListAllPets 的注释),
// 单次迭代已到几十毫秒,默认 benchtime 只跑得动十几轮。
func BenchmarkListAllPetsUnmarshal(b *testing.B) {
	for _, n := range []int{1000, 7545} {
		blobs := benchPetBlobs(n)
		bytes := 0
		for _, bl := range blobs {
			bytes += len(bl)
		}
		b.Run(fmt.Sprint("pets=", n), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(bytes))
			for i := 0; i < b.N; i++ {
				out := make([]*Pet, 0, len(blobs))
				for _, data := range blobs {
					var p Pet
					if json.Unmarshal(data, &p) == nil {
						out = append(out, &p)
					}
				}
			}
		})
	}
}
