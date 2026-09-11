package pipeline

import (
	"testing"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/zxsos/rocom-go/internal/capture"
	"github.com/zxsos/rocom-go/internal/gcp"
	"github.com/zxsos/rocom-go/internal/trial"
)

// 本文件锁住「局级天生特性(#33)要跟着建局一起落进 run」。
//
// 背景(实测,见 docs/pcap-20260831-grass-trial.md):
// #33 只出现在 challenge_data 全量快照里,而一局只有 4 次(3 章开头 + BOSS 前),
// 且**换章会重建 run**(openRun 认 chapter_id 变了就 newRun)。原先只有 merge
// 那条路带 initialFeatures,newRun 漏了 —— 于是第一章整章、以及每次换章之后,
// initial 都是空的:拆分(天生/获得)失效、特性名也无从桥接,页面退回裸 id
// 「特性 288135」,而且**不报错、golden 契约也照样绿**(缺的是数据不是结构)。
//
// 故这里直接喂开局包与换章包,断言推给前端的载荷里天生/获得两组都在、
// 且天生那条有名字。

// trialPetBody 拼 GrassTrialPetData:field2=base_conf_id,
// field11=acquired_feature_ids(非 packed,已获得的流水)。
func trialPetBody(base uint32, feats ...uint32) []byte {
	b := protowire.AppendTag(nil, 2, protowire.VarintType)
	b = protowire.AppendVarint(b, uint64(base))
	for _, f := range feats {
		b = protowire.AppendTag(b, 11, protowire.VarintType)
		b = protowire.AppendVarint(b, uint64(f))
	}
	return b
}

// challengeDataBody 拼 GrassTrialChallengeData:field2=trial_conf_id /
// field3=current_chapter_id / field5=trial_pet_data /
// field33=initial_feature_ids(局级天生特性)。
func challengeDataBody(trialConf, chapter uint32, pet []byte, initial ...uint32) []byte {
	var b []byte
	b = protowire.AppendTag(b, 2, protowire.VarintType)
	b = protowire.AppendVarint(b, uint64(trialConf))
	b = protowire.AppendTag(b, 3, protowire.VarintType)
	b = protowire.AppendVarint(b, uint64(chapter))
	if len(pet) > 0 {
		b = protowire.AppendTag(b, 5, protowire.BytesType)
		b = protowire.AppendBytes(b, pet)
	}
	for _, f := range initial {
		b = protowire.AppendTag(b, 33, protowire.VarintType)
		b = protowire.AppendVarint(b, uint64(f))
	}
	return b
}

// startChallengeRspBody 拼 0x1951 开局回包:field2=challenge_data。
func startChallengeRspBody(cd []byte) []byte {
	b := protowire.AppendTag(nil, 2, protowire.BytesType)
	return protowire.AppendBytes(b, cd)
}

// challengeSyncBody 拼 0x195c 同步:field1=challenge_data。
func challengeSyncBody(cd []byte) []byte {
	b := protowire.AppendTag(nil, 1, protowire.BytesType)
	return protowire.AppendBytes(b, cd)
}

// TestTrialStartCarriesInitialFeatures 锁住开局包(0x1951)这一路。
//
// 这是**页面最常见的一路**:一局的第一份快照就是它,而它后面紧跟着的 0x195c
// 只有在同章时才会走 merge 补上 initial —— 章号一变就又是 newRun。
// 漏了这里,玩家开局看到的就是「特性 288135」而不是「特性·天生 预警」。
func TestTrialStartCarriesInitialFeatures(t *testing.T) {
	p, _ := newTestPipeline(t)
	base, featName := findPetWithFeature(t, p.db)
	const innate, gained = uint32(288135), uint32(288001)

	p.handleTrial(capture.Message{
		Direction: gcp.S2C, Opcode: trial.OpStartChallengeRsp,
		AppBody: startChallengeRspBody(
			challengeDataBody(10002, 3000, trialPetBody(base, innate, gained), innate)),
	}, testAcc)

	run := p.acct(testAcc).trial().run
	if run == nil {
		t.Fatal("开局包没有建起 run")
	}
	out := p.trialRunPayload(run, nil)
	if out.Pet == nil {
		t.Fatal("载荷里没有宠物")
	}
	if len(out.Pet.InnateFeatures) != 1 || out.Pet.InnateFeatures[0] != innate {
		t.Fatalf("天生特性 = %v,期望 [%d] —— initial 没带上就只剩裸 id",
			out.Pet.InnateFeatures, innate)
	}
	if len(out.Pet.GainedFeatures) != 1 || out.Pet.GainedFeatures[0] != gained {
		t.Errorf("试炼中获得的特性 = %v,期望 [%d]", out.Pet.GainedFeatures, gained)
	}
	// 天生那条要能桥出名字 —— 页面显示「特性·天生 预警」而非数字,靠的就是它
	if got := out.Pet.FeatureNames[innate]; got != featName {
		t.Errorf("天生特性 %d 名 = %q,期望 %q(base=%d)", innate, got, featName, base)
	}
	if _, ok := out.Pet.FeatureNames[gained]; ok {
		t.Error("试炼中获得的特性不该有名字 —— 它是节点随机给的,与精灵本身无关")
	}
}

// TestTrialSyncWithoutInitialKeepsPrevious 锁住 merge 那侧的「空值不覆盖」。
//
// 与上面两条互补:那条路防的是「没带上」,这条防的是「被清掉」。#33 是局级字段,
// apply 类的增量回包(推进节点/选完奖励后的同步)里没有它,此时 merge 若无条件
// 赋值就会把建局时取到的那份抹成空 —— 页面正看着的特性名会中途消失。
// 这条测试就是为它而写:去掉 merge 里的 len() 守卫,这里必须红。
func TestTrialSyncWithoutInitialKeepsPrevious(t *testing.T) {
	p, _ := newTestPipeline(t)
	base, featName := findPetWithFeature(t, p.db)
	const innate, gained = uint32(288135), uint32(288001)

	p.handleTrial(capture.Message{
		Direction: gcp.S2C, Opcode: trial.OpStartChallengeRsp,
		AppBody: startChallengeRspBody(
			challengeDataBody(10002, 3000, trialPetBody(base, innate), innate)),
	}, testAcc)
	// 同章同步(未换章 → 走 merge 而非 newRun),且这份快照没有 #33
	p.handleTrial(capture.Message{
		Direction: gcp.S2C, Opcode: trial.OpChallengeDataSync,
		AppBody: challengeSyncBody(
			challengeDataBody(10002, 3000, trialPetBody(base, innate, gained))),
	}, testAcc)

	out := p.trialRunPayload(p.acct(testAcc).trial().run, nil)
	if len(out.Pet.InnateFeatures) != 1 || out.Pet.InnateFeatures[0] != innate {
		t.Fatalf("增量同步后天生特性 = %v,期望仍为 [%d] —— 被空快照覆盖了",
			out.Pet.InnateFeatures, innate)
	}
	if got := out.Pet.FeatureNames[innate]; got != featName {
		t.Errorf("增量同步后天生特性 %d 名 = %q,期望 %q", innate, got, featName)
	}
	if len(out.Pet.GainedFeatures) != 1 || out.Pet.GainedFeatures[0] != gained {
		t.Errorf("获得的特性 = %v,期望 [%d] —— 增量同步照常要生效", out.Pet.GainedFeatures, gained)
	}
}

// TestTrialChapterChangeKeepsInitialFeatures 锁住换章(重建 run)这一路。
//
// 换章时 openRun 见 chapter_id 变了会 newRun,全量快照里那份 #33 若没跟着进新
// run,第二章起特性名就整章消失 —— 而且要到下次同章同步才可能回来。
func TestTrialChapterChangeKeepsInitialFeatures(t *testing.T) {
	p, _ := newTestPipeline(t)
	base, featName := findPetWithFeature(t, p.db)
	const innate, gained = uint32(288135), uint32(288001)

	p.handleTrial(capture.Message{
		Direction: gcp.S2C, Opcode: trial.OpStartChallengeRsp,
		AppBody: startChallengeRspBody(
			challengeDataBody(10002, 3000, trialPetBody(base, innate), innate)),
	}, testAcc)
	// 换到第 2 章:新快照换了 chapter_id,run 会重建
	p.handleTrial(capture.Message{
		Direction: gcp.S2C, Opcode: trial.OpChallengeDataSync,
		AppBody: challengeSyncBody(
			challengeDataBody(10002, 3001, trialPetBody(base, innate, gained), innate)),
	}, testAcc)

	run := p.acct(testAcc).trial().run
	if run == nil {
		t.Fatal("换章后没有 run")
	}
	out := p.trialRunPayload(run, nil)
	if len(out.Pet.InnateFeatures) != 1 || out.Pet.InnateFeatures[0] != innate {
		t.Fatalf("换章后天生特性 = %v,期望 [%d]", out.Pet.InnateFeatures, innate)
	}
	if got := out.Pet.FeatureNames[innate]; got != featName {
		t.Errorf("换章后天生特性 %d 名 = %q,期望 %q", innate, got, featName)
	}
	if len(out.Pet.GainedFeatures) != 1 || out.Pet.GainedFeatures[0] != gained {
		t.Errorf("换章后获得的特性 = %v,期望 [%d]", out.Pet.GainedFeatures, gained)
	}
}
