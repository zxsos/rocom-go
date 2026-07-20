# 宠物音频与宠物的关联

解包目录里有完整的宠物叫声,但它们**不带宠物 id**——音频是一堆数字命名的 `.wem`,
要落到「哪只宠物的哪种情绪」需要跨四张表拼接。本文记录这条关联链路。

音频本身**不进仓库、也不参与 `go build`**(`internal/gamedata` 只 embed 图片),
这里只记录解包侧的对应关系,供按需导出使用。

## 1. 音频在哪

`NRC/Content/NewRoco/WwiseAudio/Windows/`,共 93376 项:

| 类型 | 数量 | 说明 |
| --- | --- | --- |
| `.bnk` | 2229 | Wwise SoundBank,**只有结构不含音频数据** |
| `.wem` | 91147 | 实际音频,数字命名(如 `446101444.wem`) |

该目录在 `scripts/unpack/Program.cs` 的 `defaultExcludes` 里,**默认不导出**
(纯客户端运行时资源,占比大)。要取音频需显式关闭排除:

```bash
scripts/unpack.sh --no-exclude --no-post \
  --filter NRC/Content/NewRoco/WwiseAudio/Windows --out ~/Downloads/rocom/audio
```

约 3.3G / 3.7 秒。用完可删,随时可重新导出。

## 2. bnk 按拼音分类

每只宠物一个 bnk,文件名是**宠物拼音**(非 conf_id):

| 前缀 | 数量 | 内容 |
| --- | --- | --- |
| `Pet_Vo_<拼音>.bnk` | 621 | **叫声**(情绪 / 战斗 / 进化) |
| `Pet_Action_<拼音>.bnk` | 650 | 动作音效 |
| `Pet_Ride_<拼音>.bnk` | 420 | 坐骑 |
| `Pet_Skill_<拼音>.bnk` | 50 | 技能 |
| `Pet_Eco_<拼音>.bnk` | 39 | 生态 / 环境 |
| `Pet_Buff_<拼音>.bnk` | 1 | 状态 |

## 3. 关联链路总览

```
dataconfig_audio.bytes ── 事件名 Pet_Vo_<拼音>_<分类>
                                │ FNV-1 32bit(小写)
                                ▼
Pet_Vo_<拼音>.bnk ── HIRC ── Event(4) → Action(3) → 容器(5/6/7) → Sound(2)
                                                                    │ sourceID
                                                                    ▼
                                                            <sourceID>.wem
        拼音
          │ PET_NAME_MAP_CONF(name → id)
          ▼
       conf_id
          │ PETBASE_CONF(name 中文名 / pictorial_book_id 图鉴号)
          ▼
   中文名 + 图鉴号
```

## 4. 事件名:唯一的语义来源

`NRC/Content/ScriptC/Data/Audio/dataconfig_audio.bytes` 里有全部事件名的明文字符串。
该文件**不是 RocoBinData 格式**(`bin2json.py` 解不了),但 `strings` 直接可抽:

```bash
strings -n 6 dataconfig_audio.bytes | grep -E '^Pet_Vo_[A-Za-z0-9_]+$' | sort -u
```

得 12213 条(全部 `Pet_*` 事件名 27243 条),形如:

```
Pet_Vo_MiaoMiao_Common_Happy   Pet_Vo_MiaoMiao_Fight_Attack_1   Pet_Vo_MiaoMiao_World_Evo
```

分类后缀共 84 种,主要为:

- `Common_` — `Happy` / `Sad` / `Anger` / `Fear` / `Alert` / `Relax` / `Shock` / `Show`
- `Fight_` — `Attack_N` / `Skill_N` / `Hit_S|M|L` / `Die` / `CallOut`
- `World_` — `Evo`(进化)等

> **后缀大小写不统一**:源数据里混有 `Common_SAd`、`Common_SHock`、`Fight_CallOut` /
> `Fight_Callout`。按语义归类前必须先归一化,否则同一分类会被拆成多个。

## 5. 解析 bnk 拿到 wem

bnk 里**没有音频数据**(无 `DIDX`/`DATA` 段),所有 Sound 都是 `streamType=2`(流式),
音频在同目录独立 `.wem` 里。需要解析 `HIRC` 段的对象图。

本作 `BKHD.version = 135`(Wwise 2021.1),以下偏移**均为实测**:

| 对象 | type | 关键字段 |
| --- | --- | --- |
| Sound | 2 | `pluginID`@4(恒 `0x40001` = Vorbis)、`streamType`@8(恒 2)、**`sourceID`@9** |
| EventAction | 3 | 目标 id @6 |
| Event | 4 | action 数量 @4(**uint8**)、随后为 actionID 数组 |
| RandomSequence | 5 | 容器 |
| Switch | 6 | 容器 |
| ActorMixer | 7 | 容器 |

事件名经 **FNV-1 32bit(名字先转小写)** 得到 Event 对象 id:

```python
def fnv(n):
    h = 2166136261
    for b in n.lower().encode():
        h = ((h * 16777619) & 0xFFFFFFFF) ^ b
    return h
```

从 Event 出发沿 `Action → 容器 → Sound` 下行,收集 `sourceID` 即为 wem 文件名。

### 下行靠 directParentID,不要扫描 id

容器的子节点列表位置随类型而变,可靠做法是读每个节点 `NodeBaseParams` 里的
**`directParentID`** 反建父子树:

- 偏移 = 前缀 + `nFX` 块 + 2 + 4,其中 Sound 前缀 14B、容器(5/6/7)前缀 0
- `nFX = 0` 时实测落在:**Sound @25、容器 @11**

> **踩过的坑**:曾用「扫描对象体里所有 4 字节、凡命中已知 id 即视为子节点」的启发式,
> 结果三个不同事件返回**完全相同的 67 个 wem**——引用会一路爬到 ActorMixer 根,
> 把整个 bnk 的 Sound 全吞进来。改用 `directParentID` 后每个事件干净地给出 3 个随机变体。

以 `Pet_Vo_MiaoMiao.bnk` 为例:336 个 HIRC 对象(Sound 181 / Action 19 / Event 19 /
RandomSeq 57 / Switch 19 / ActorMixer 4),19 个事件各对应 3 个随机变体。

全量解析结果:**621 只宠物 / 11587 个事件 / 34514 个唯一 wem**,平均每只约 60 段。

## 6. 拼音 → conf_id

`Bin/BinDataCompressed/PET_NAME_MAP_CONF.json` 的 `RocoDataRows`:

```json
{ "3001": { "id": 3001, "name": "MiaoMiao" }, "3004": { "id": 3004, "name": "DiMo" } }
```

667 条 / 618 个唯一拼音。621 个 `Pet_Vo_` bnk 中 **582 个能对上**。

- 匹配需**大小写不敏感**:表里混有 `Huohua` 与 `HuoHua` 两种写法
- 39 个 bnk 查无此 id(疑似未上线 / 非图鉴宠),如 `LuoKaDe`、`DuDu`
- 23 个拼音对应多个 conf_id(同名不同形态)

## 7. conf_id → 中文名与图鉴号

`Bin/BinDataCompressed/PETBASE_CONF.json`:

| 字段 | 含义 | 覆盖 |
| --- | --- | --- |
| `name` | 中文名 | 1128 条 |
| `pictorial_book_id` | **图鉴号** | 668 条 |

两点容易踩:

- **`conf_id` ≠ 图鉴号**:conf 3001「喵喵」的图鉴号是 2,conf 3004「迪莫」才是 1。
- **`PETBASE_CONF.name` 比 `names.json` 的 `species` 全**:后者缺 173 只有叫声宠物的中文名,
  取中文名应以 `PETBASE_CONF` 为准。

一个图鉴号可挂**多个 conf_id 形态变体**,各有独立叫声。例如图鉴 11「鸭吉吉」有 6 个
(`PangYaJiJi` / `ShouYaJiJi` / `JiJiYa` / `KunYa` / `RanleYa` / `DengYiDengYa`),
实测 6 段音频互不相同。按图鉴号命名文件时必须再带拼音去重。

同拼音存在多个 conf_id 时,应**优先取有 `pictorial_book_id` 的那个**,否则会拿到无图鉴号的分身。

## 8. 解码

`.wem` 是 RIFF/WAVE 但 codec tag `0xFFFF`(Wwise Vorbis,码本被剥离),**ffmpeg 解不了**,
需 [vgmstream](https://github.com/vgmstream/vgmstream):

```bash
vgmstream-cli -o out.wav 446101444.wem
ffmpeg -i out.wav -ac 1 -ar 48000 -c:a aac -b:a 48k -movflags +faststart out.m4a
```

> vgmstream 输出的 wav **不能用管道喂给 ffmpeg**(头部大小字段流式不可靠,ffmpeg 报错
> 退出码 183),须落临时文件。

批量转码时务必给 `subprocess.run` 加 `check=True`——wem 缺失会让 vgmstream 静默返回空,
下游生成出**零长度音频**且无任何报错。

## 9. 校验对应关系是否正确

比对「切出的音频」与「已知正确的单段」时,**不要用原始波形相关度**:几毫秒的相位差就会让
高频波形相关度崩到 0.1 以下,看着像错的其实是对的。应改用 **10ms RMS 包络相关**:

- 对应正确:0.87 ~ 0.998
- 故意取错的对照段:0.14 ~ 0.62

区分度足够清晰,且对小偏移不敏感。

## 10. 相关文档

- 解包流程与其它数据源:[data.md](data.md)
- 工具与开源项目清单:[reference.md](reference.md)
