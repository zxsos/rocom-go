package pipeline

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/whoisnian/rocom-capture/internal/gamedata"
	"github.com/whoisnian/rocom-capture/internal/gcp"
	"github.com/whoisnian/rocom-capture/internal/scene"
)

// 本文件锁住一件事:野生宠的标记**必须带上形态编号 baseConfId**。
//
// 为什么单独测:大地图色卡的「在 rkpet 看 3D 效果」外链靠它拼 URL,缺了这个字段按钮
// 就不显示。而契约 golden(contract/wildpets.json)里的标记是**测试夹具直接构造**的、
// 不走 pipeline 这条路径,所以 golden 守护不到它 —— 字段即使被删,契约测试照样绿。
// 这是「golden 是样本而非完备清单」的又一实例(见 docs/api/README.md)。
//
// 故这里走**真实路径**:喂一条 actor_enter 让 observeWilds 判定并广播,再读回快照断言。

// TestWildMarkCarriesBaseConfID 广播的每只稀有野生宠都要带形态编号,且是**形态编号**
// 而非 npc_cfg_id。
func TestWildMarkCarriesBaseConfID(t *testing.T) {
	p, srv := newTestPipeline(t)
	login(t, p, 1)
	p.handle(msg(gcp.S2C, scene.OpEnterSceneRsp, enterSceneBody(1001, testRes, 0)))

	p.handle(msg(gcp.S2C, scene.OpPlayActsNotify, actorEnterBody(777, 100, 200)))
	// 推进刷窗:wilds 广播是消息驱动的,没有下一条消息就发不出去(见 wildsDebounce)。
	p.handle(msg(gcp.S2C, scene.OpEnterSceneRsp, enterSceneBody(1001, testRes, 0)))

	want, ok := p.db.NpcPetBase(uint32(testNpcCfgID))
	if !ok {
		t.Fatalf("前置失败:NPC_CONF.id=%d 在 NpcPetBase 里查不到形态编号", testNpcCfgID)
	}
	if want == uint32(testNpcCfgID) {
		t.Logf("注意:本配置下形态编号恰与 npc_cfg_id 相同(%d),该断言区分不了二者", want)
	}

	payload := srv.GetLastWildPets(testAcc)
	if payload == nil {
		t.Fatal("没有 wildpets 快照(广播没触发?)")
	}
	if len(payload.Pets) == 0 {
		t.Fatalf("稀有通道一只标记都没有(普通通道 %d 只),无法验证", len(payload.AllPets))
	}
	for _, m := range payload.Pets {
		if m.BaseConfID == 0 {
			t.Errorf("标记 %s(%s) 缺 baseConfId —— 大地图色卡的「看 3D 效果」按钮不会显示",
				m.ID, m.Name)
			continue
		}
		// 必须是形态编号:塞成 npc_cfg_id 会静默地让外链指向错宠物,页面却一切正常。
		if m.BaseConfID != want {
			t.Errorf("标记 %s 的 baseConfId = %d, 期望形态编号 %d(npc_cfg_id 是 %d,别塞错)",
				m.ID, m.BaseConfID, want, testNpcCfgID)
		}
	}

	// 字段必须真的出现在 JSON 里:omitempty 遇上零值会让整个键消失,前端拿到 undefined。
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("序列化: %v", err)
	}
	var decoded struct {
		Pets []map[string]any `json:"pets"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("解析: %v", err)
	}
	if len(decoded.Pets) == 0 {
		t.Fatal("序列化后 pets 为空")
	}
	if _, ok := decoded.Pets[0]["baseConfId"]; !ok {
		t.Errorf("JSON 里没有 baseConfId 字段(被 omitempty 吞了?): %s", raw)
	}
}

// TestWildMarkBaseConfIDNotDerivedFromImage 形态编号不得从头像路径反推。
//
// 这条防的是「解析 HeadIcon/<n>.webp 拿编号」那种取巧实现。头像只是图片**文件名**,
// 与形态编号之间没有换算关系 —— 多个形态常共用同一张素材:实测 names.json 的 images
// 里 1112 个形态中 461 个的 h 与形态编号不同(如 3242 的图是 3012),220 个有异色头像的
// 里 75 个 sh 不以形态编号开头。反推会静默地把 3D 外链指向别的宠物,页面却一切正常。
//
// 故本用例断言:即使该形态的异色头像是另一张(存在两套文件名),下发的编号仍必须等于
// NpcPetBase 的结果,而不是头像里的那个号。
func TestWildMarkBaseConfIDNotDerivedFromImage(t *testing.T) {
	p, srv := newTestPipeline(t)
	login(t, p, 1)
	p.handle(msg(gcp.S2C, scene.OpEnterSceneRsp, enterSceneBody(1001, testRes, 0)))

	want, ok := p.db.NpcPetBase(uint32(testNpcCfgID))
	if !ok {
		t.Fatal("前置失败:NpcPetBase 查不到形态编号")
	}
	shinyHead := p.db.PetImageByBase(want, true).Head
	plainHead := p.db.PetImageByBase(want, false).Head
	if shinyHead == plainHead {
		t.Skipf("该形态异色与普通头像相同(%s),本用例无法用它区分两套编号", plainHead)
	}

	// 一只**异色炫彩**宠(炫彩色卡最常出现的情形),走真实广播路径。
	p.conn(testSess).wilds.pets[888] = &wildPet{
		actorID:    888,
		cfgID:      int32(testNpcCfgID),
		seenAt:     time.Now(),
		glassType:  gamedata.GlassCommon,
		glassValue: 131073,
		mutation:   1, // MutationShiny
	}
	p.handle(msg(gcp.S2C, scene.OpEnterSceneRsp, enterSceneBody(1001, testRes, 0))) // 推进刷窗

	payload := srv.GetLastWildPets(testAcc)
	if payload == nil {
		t.Fatal("没有 wildpets 快照")
	}
	var found bool
	for _, m := range payload.Pets {
		if m.ID != "888" {
			continue
		}
		found = true
		if m.GlassType == 0 {
			t.Fatalf("前置失败:标记 888 不是炫彩(glassType=%d),本用例失去意义", m.GlassType)
		}
		if m.BaseConfID == 0 {
			t.Fatalf("异色炫彩宠缺 baseConfId —— 正是大地图色卡要用的字段")
		}
		if m.BaseConfID != want {
			t.Errorf("异色炫彩宠 baseConfId = %d, 期望 %d", m.BaseConfID, want)
		}
		// shiny 必须给:rkpet 外链靠它加 shiny=1,否则 3D 模型是普通配色 ——
		// 而异色炫彩恰恰是炫彩里最常见的情形,缺了它链接就指向错配色。
		if !m.Shiny {
			t.Errorf("异色炫彩宠 shiny = false —— rkpet 外链会指向普通配色的 3D 模型")
		}
		// 反例锚点:头像用的是异色那套文件名,若实现去解析头像路径就会得到它。
		// 这里断言的是「形态编号 != 头像里的编号」这一事实成立时,我们给的仍是形态编号。
		t.Logf("异色头像 %s / 普通头像 %s / 形态编号 %d / shiny=%v",
			m.Img, plainHead, m.BaseConfID, m.Shiny)
	}
	if !found {
		t.Fatal("标记 888 没进稀有通道(构造方式失效?)")
	}
}
