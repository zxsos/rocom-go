#!/usr/bin/env python3
"""校验重构「除登记在案的改动外,没动任何东西」。

用法:  uv run python scripts/check_refactor.py

背景:internal/server 的重构先按行区间机械拆分大文件(阶段 1)。
函数体逐字节搬过去不难,难的是**证明**没改坏。本脚本就是那个证明。

覆盖范围:
  - 阶段 1(纯搬家)拆出的各函数:逐字节比对,是最初的用途。
  - 阶段 2(拆 Server 上帝对象)会**有意**改写一批函数(接收者、字段引用、
    调用点)。这些登记在 INTENTIONALLY_CHANGED,只校验存在性;其内容正确性由
    golden 契约测试与 -race 并发测试覆盖,不靠本脚本。

故本脚本回答的问题是「有没有**未登记**的改动」,而不是「有没有改动」。
新增有意改动时,务必连同理由登记进去 —— 否则脚本会退化成噪音,没人再看它的输出。

它比 go build / go vet / golden 测试多查三样 —— 后三者都查不出来:

1. **注释归属**:拆分时最易漏。若某函数的文档注释留在了 A 文件、函数体去了 B 文件,
   编译、vet、gofmt、测试全过,但注释从此指不到自己的函数(阶段 1 真踩过一次:
   handleMerchantSub 的注释被留在 merchant_smtp.go,函数体去了 merchant_sub.go)。
2. **注释完整性**:原始文件的每条注释块都必须出现在某个新文件里,不许凭空消失。
3. **净内容守恒**:除白名单外,不许多出、少掉任何一行。

归一化说明:一律**剥离全部空白**再比对。这是必要的,因为 gofmt 会合法地重排代码与注释
(单行匿名 struct 展开为多行、`//(x` 补空格成 `// (x`、注释内列表项缩进对齐),
这些都不改变语义,却会让「按空白折叠」的朴素比对误报。剥离空白 ≈ 比对 token 流。
代价:字符串字面量内部的空白差异也比对不出来("a b" vs "ab"),对纯搬家校验可接受 ——
本脚本是「有没有动过」的兜底,不是语义等价证明。

扩展:改 REFACTORS 添加其它「原文件 → 若干新文件」的拆分记录。
"""
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

# BASE_REV 是「拆分前」的基线修订,原始文件从它读取。
#
# 必须是固定的 commit 而非 HEAD:拆分提交后 HEAD 里已无那些原文件,
# 用 HEAD 会让脚本失效(且只在提交后才暴露)。
# f920b87 = 后端重构前的 master(含 admin.go 793 行 / api_merchant.go 988 行)。
BASE_REV = "f920b87"

# 原文件(相对仓库根) -> 拆分后的新文件列表
#
# 原文件已删除,内容从 git 历史的 BASE_REV 取。故 BASE_REV **必须**指向拆分前的修订
# (如 0c7a8b7),不能写 HEAD —— 拆分一旦提交,HEAD 里就没有这些文件了,
# 脚本会以「读 git 历史失败」退出。这个坑在拆分提交后才会暴露。
REFACTORS = {
    "internal/server/admin.go": [
        "internal/server/admin.go",
        "internal/server/admin_manage.go",
        "internal/server/admin_inject.go",
    ],
    "internal/server/api_merchant.go": [
        "internal/server/merchant.go",
        "internal/server/merchant_notify.go",
        "internal/server/merchant_mail.go",
        "internal/server/merchant_smtp.go",
        "internal/server/merchant_sub.go",
    ],
}

# 有意整体删除的函数:函数名 -> 删除理由。
# 连同其文档注释与函数体一并从期望中剔除(否则它们会被报成「丢失」)。
ALLOWED_REMOVED_FUNCS = {
    "handleAdminPlaceholder": "孤儿路由,前端已删唯一调用者",
    # fix:第三方滞后补货。缓存判定由「是否有记录」升级为「是否该回源」,该函数被
    # merchantShouldFetch 取代(多了进行中窗口/冷却两个维度,不是原地改写)。
    "merchantCached": "fix:被 merchantShouldFetch 取代(当前槽需按冷却重查)",
}

# 重构后新增的函数:函数名 -> 新增理由。
# 脚本的价值是「除登记在案者外不许变」,新增同样要登记 —— 否则每次新功能都会
# 报「函数多出」,噪音一起,真正未登记的改动就没人看了。
ALLOWED_NEW_FUNCS = {
    "merchantClaim": "fix:同一槽并发触发重复发信,新增进程内认领(带冷却,不锁死失败重试)",
    # fix:第三方滞后补货导致整轮漏商品 —— 当前槽要能按冷却重查,已结束的槽必须永不回源
    "merchantSlotLive": "fix:槽是否仍在进行中(已结束的槽拿回来的是当前货单,不能回源)",
    "merchantShouldFetch": "fix:回源判定(未查过→补查;进行中且过冷却→重查;已结束/过窗口→不查)",
    "merchantCurrentSlot": "fix:从 merchantEnsure 抽出「当前轮」下标,便于无竞争地单测",
    # fix:第三方滞后补货(与上面同批改动,当时漏登记,2026-09-02 按代码补登)
    "merchantShouldForceRefresh": "fix:第三方滞后,开市后 10 分钟内强制回源(refresh=true)以尽快拿到本轮货单",
    "fmtDuration": "fix:第三方滞后(同上),邮件「整点后 X」需要把 Duration 写成「3 分 20 秒」",
    # feat:远行商人双数据源(咸鱼源 / 好游快爆源,可切换)。见 docs/data.md 第 6 节。
    # 两源只有「怎么取」不同,故只抽出两个同签名方法,取到之后完全共用 —— 刻意不做接口化。
    "fetchXianyu": "feat:双数据源,咸鱼源回源从 merchantFetch 抽出,与 fetchHaoyou 同签名供按源分派",
    "merchantSource": "feat:双数据源,当前生效源(内存镜像;回源路径与 HTTP 处理器每次都读)",
    "merchantSourceValid": "feat:双数据源,源标识合法性校验(管理端点写入与启动时载入共用)",
    "merchantNeedKey": "feat:双数据源,该源是否必须配令牌(只有咸鱼源需要,好游快爆抓公开页面)",
    "merchantSourceName": "feat:双数据源,源标识→中文名(映射留后端,免得与前端漂移)",
    "merchantSetSource": "feat:双数据源,切换(落库+清空槽缓存+按新源重抓当前轮)",
    # 同上(feat:远行商人双数据源),管理面板的读写端点:GET 读当前源与源清单,
    # POST 切换。源清单一并下发而非法前端硬编码 —— 合法标识只有后端能校验。
    "handleAdminMerchantSource": "feat:双数据源,管理端点的读写(GET 当前源+源清单 / POST 切换)",
}

# 有意删除的零散代码行(正则,匹配归一化后的行)。两侧都剔除后再比对。
ALLOWED_LINE_PATTERNS = [
    r"typesnapstruct\{",   # sweepInjects 里的死代码 type snap struct{...}
    r"vartodo\[\]snap",
    r"_=todo",
    # 阶段 3:injectEntry.mark 的类型由 map[string]any 改为 *WildMark(字段声明行本身)
    r"markmap\[string\]any//广播给前端的标记载荷\(花种注入仅作记录,不广播wildpets\)",
    # fix:第三方滞后补货导致整轮漏商品(2026-08-30 实测)。常量 merchantFetchURL 由 const
    # 改为 var —— 单元测试要用 httptest 把它换掉,const 换不了,不换就会真打到线上烧 token。
    r"merchantFetchURL=\"https://apii\.xianyuw\.cn/api/v1/rocom-merchant\"",
    # 同上那个 fix:文件头业务模型注释里被改写的 5 行(旧描述「命中缓存不再回源」已不成立)。
    # 只挑不含正则元字符的片段,免得满屏转义。
    r"命中缓存不再回源,防止反复烧第三方token;",
    r"缓存保留2天,写入时顺手清理更早记录;",
    r"按当前时间补查缺失的槽;",
    r"有货槽写入后对比本营业日更早轮找出「新增商品」",
    r"每槽每邮箱只发一次",
    # fix:整点后尽快拿到新货单(2026-09-02)。文件头注释里这句已不成立 —— 轮询
    # 由「每 15 分钟」改为 15 秒一趟,并新增档期刚开始时的 30 秒密集重试。
    r"触发回源两条路径:merchantLoop每15分钟检查当前槽",
]

# 有意改写的注释块(正则,匹配归一化后的注释块文本)。
#
# 与 ALLOWED_REMOVED_FUNCS 同理:脚本的价值是「除登记在案者外不许变」,而**修 bug 时注释
# 本来就该跟着改** —— 把已经不成立的描述留在原地,比删掉更危险(后来人会照着错的注释改代码)。
# 故这里登记旧文案的标志性片段 + 理由,而不是为了过校验而保留过时注释。
ALLOWED_REMOVED_COMMENTS = [
    # fix:当前槽改为按冷却重查(第三方滞后补货),旧文案「命中缓存不再回源」已不成立
    r"命中缓存不再回源,防止反复烧第三方token",
]

# 阶段 2(拆 Server 上帝对象)有意改写的函数。
#
# 阶段 1 是「纯搬家」,可以逐字节比;阶段 2 不是 —— 它把状态从 Server 收进
# snapshotStore / smtpSender 等类型,相关函数的接收者与字段引用必然变化。
# 这些函数因此**只校验存在性,不校验内容**:内容正确性由 golden 测试(契约)
# 与 -race 并发测试(锁)覆盖,不靠本脚本。
#
# 其余函数仍要求逐字节一致 —— 脚本的价值正在于「除登记在案者外不许变」。
INTENTIONALLY_CHANGED = {
    # 阶段 2:SMTP 状态收进 smtpSender,接收者 *Server → *smtpSender
    "smtpSendMail": "阶段2:接收者改为 *smtpSender,凭据改读 m.user/m.pass",
    "sendMerchantMail": "阶段2:接收者改为 *smtpSender",
    "sendMerchantMailHTML": "阶段2:接收者改为 *smtpSender",
    # 阶段 2:调用点改为 s.snap.* / s.smtp.*
    "merchantNotify": "阶段2:发信调用点改为 s.smtp.send*;fix:去重粒度由整槽改为每商品(补货要能补发)",
    "merchantResend": "阶段2:发信调用点改为 s.smtp.send*",
    # fix:第三方滞后补货。改为只回源当前轮(更早的轮永不回源,拿回来的是当前货单=伪造历史),
    # 并按 merchantShouldFetch 判定(取代原先「有缓存就跳过」)。
    "merchantEnsure": "fix:只回源当前轮,判定改走 merchantShouldFetch(支持按冷却重查)",
    # fix:同上。新增「第三方返回空但库里已有货单时保留旧数据,只推回源时刻」的保护,
    # 否则重查一撞上限流就把页面上明明还有的货单清成空。
    "merchantFetch": "fix:空响应保护(保留既有货单,只刷 fetched_at 以免冷却失效)",
    "merchantLoop": "fix:注释随回源规则变更(当前槽会按冷却重查)",
    "merchantSlotsOfDay": "fix:GetMerchantSlot 新增返回 fetched_at,调用点随之改",
    # fix:raw string 不转义,写在模板里的 \r\n 是四个字面字符,收件端可见 —— 改回真 CRLF 拼接
    "merchantMailGroup": "fix:分组标题行的 \\r\\n 原本写在 raw string 里,是四个字面字符",
    "handleMerchantSub": "阶段2:发信调用点改为 s.smtp.send*,配置判定改为 s.smtp.configured()",
    "handleAdminMerchantSubs": "阶段2:配置判定改为 s.smtp.configured()",
    "handleAdminMerchantTestMail": "阶段2:发信调用点改为 s.smtp.send*",
    "handleMerchant": "阶段2:配置判定改为 s.smtp.configured();feat:双数据源,无令牌短路改为仅咸鱼源生效,响应加 source 字段",
    # fix:第三方滞后补货(与 ALLOWED_NEW_FUNCS 里那批同批改动,当时漏登记,2026-09-02 补登):
    # 邮件正文加「数据获取于 …(整点后 X)」一行,让读者自己判断这份货单有多旧。
    "merchantMailContent": "fix:第三方滞后,邮件正文加「数据获取于 …(整点后 X)」一行",
    "from": "阶段2:由 merchantMailFrom 改名并入 smtpSender,凭据改读 m.user",
    # 阶段 2:位置/野生宠/花种快照收进 snapshotStore,读-改-写收敛为原子方法
    "handlePosition": "阶段2:改读 s.snap.getPos()",
    "handleWildPets": "阶段2:改读 s.snap.getWild()",
    "handleHome": "阶段2:改读 s.snap.getHome()",
    "handleFlowers": "阶段2:改读 s.snap.getFlower()",
    "handleDeleteFlowerSlot": "阶段2:读-改-写收敛为 s.snap.dropFlowerWorld()",
    "SetLastPosition": "阶段2:改调 s.snap.setPos()",
    "SetLastWildPets": "阶段2:改调 s.snap.setWild()",
    "SetLastHome": "阶段2:改调 s.snap.setHome()",
    "SetLastFlowers": "阶段2:改调 s.snap.setFlower()",
    "GetLastFlowers": "阶段2:改调 s.snap.getFlower()",
    "InjectFlowerItem": "阶段2:读-改-写收敛为 s.snap.injectFlower()",
    "RemoveFlowerItem": "阶段2:读-改-写收敛为 s.snap.dropFlower()",
    "handleAdminInjectWild": "阶段2:合并标记改调 s.snap.mergeWild()",
    "removeInject": "阶段2:撤销标记改调 s.snap.dropWild()",
    "sweepInjects": "阶段2:改读 s.snap.getPos()",
    "handleDeleteAccount": "阶段2:清理快照改调 s.snap.forget()",
    # 阶段 3:载荷强类型化。injectEntry.mark 由 map[string]any 改为 *WildMark,
    # 读写它的三处随之改为字段访问(原先是字面量键,改错前端读到 undefined 而编译照样过)。
    "handleAdminListInjects": "阶段3:mark 类型改为 *WildMark,改读 e.mark.Name/Kinds",
    "handleAdminInjectFlower": "阶段3:mark 类型改为 *WildMark,改用字段赋值",
    "handleAdminInjectWild": "阶段2+3:mark 改为 *WildMark 且合并标记改调 s.snap.mergeWild()",
    # feat:游玩记录明细加分页(limit/offset/total),默认值随之改为 50、上限 200;
    # 汇总仍按全量算、不随分页变化。合入时漏登记,2026-09-02 按 diff 补登。
    "handleAdminPlaySessions": "feat:游玩记录明细加分页(limit/offset/total),默认 50、上限 200",
}

# 阶段 2 的重命名:旧名 -> 新名。改名后按新名比对内容。
RENAMES = {
    "merchantMailFrom": "from",  # 并入 smtpSender,方法名去重
}


def norm(s):
    """剥离全部空白,近似 token 流比对。"""
    return re.sub(r"\s+", "", s)


def norm_doc(lines):
    """注释文本归一化:剥离空白,并丢掉裸 `//` 行。

    裸 `//` 行是 gofmt 的排版产物 —— 注释里出现列表项后,它会在列表与后续段落之间插入
    一个空注释行。那是纯格式,不是内容,比对时必须忽略(实测 merchantSlotJSON 的注释
    就被插了一行)。
    """
    return norm("".join(l for l in lines if l.strip() != "//"))


def git_show(path):
    r = subprocess.run(["git", "show", f"{BASE_REV}:{path}"], cwd=ROOT,
                       capture_output=True, text=True)
    if r.returncode != 0:
        sys.exit(
            f"读基线 {BASE_REV}:{path} 失败: {r.stderr.strip()}\n"
            f"提示:BASE_REV 必须指向**拆分前**的修订。拆分提交后 HEAD 里就没有这些原文件了,\n"
            f"若你把 BASE_REV 改成了 HEAD,改回拆分前的 commit 即可。\n"
            f"可用 git log --oneline 找到拆分前的修订,再用 "
            f"git cat-file -e <rev>:{path} 确认它含该文件。"
        )
    return r.stdout


def strip_imports(lines):
    """去掉 import 块(goimports 会重排,不参与守恒比对)。"""
    out, in_import = [], False
    for line in lines:
        if re.match(r"^import \(\s*$", line):
            in_import = True
            continue
        if in_import:
            if line.strip() == ")":
                in_import = False
            continue
        out.append(line)
    return out


def parse(src):
    """解析为 (函数表, 残余行)。

    函数表: 函数名 -> {"doc": [注释行], "body": [代码行]}
    残余行: 不属于任何函数的行(包注释、import 已剔、类型/常量/变量、独立注释)
    """
    lines = src.split("\n")
    funcs, pending, cur, buf = {}, [], None, []
    taken = set()  # 被函数占用的行号

    def flush():
        if cur is not None:
            funcs[cur] = {"doc": list(pending), "body": list(buf)}

    for i, line in enumerate(lines):
        m = re.match(r"^func (\([^)]*\) )?(\w+)\(", line)
        if m:
            flush()
            cur, buf = m.group(2), [line]  # 签名行属于函数体
            # 文档注释 = 紧邻函数、向上连续的注释行(含其间空行)
            j = i - 1
            doc = []
            while j >= 0 and (lines[j].strip().startswith("//") or lines[j].strip() == ""):
                doc.insert(0, lines[j])
                j -= 1
            # 去掉末尾多余空行
            while doc and doc[-1].strip() == "":
                doc.pop()
            pending = doc
            taken.update(range(j + 1, i))
            taken.add(i)  # 函数签名行本身也属于函数,别漏进残余行
            continue
        if cur is not None:
            buf.append(line)
            taken.add(i)
            if line == "}":
                flush()
                cur, buf, pending = None, [], []
    flush()

    residual = [l for i, l in enumerate(lines) if i not in taken]
    return funcs, strip_imports(residual)


def block_comments(lines):
    """连续 // 注释块集合(归一化,按块比对而非按行)。"""
    blocks, cur = set(), []
    for line in lines + [""]:
        if line.strip().startswith("//"):
            cur.append(line)
        elif cur:
            blocks.add(norm_doc(cur))
            cur = []
    return blocks


def main():
    failed = False
    seen_whitelist = set()
    for orig_path, new_paths in REFACTORS.items():
        print(f"── {orig_path}")
        of, ores = parse(git_show(orig_path))
        news = {p: (ROOT / p).read_text(encoding="utf-8").split("\n") for p in new_paths}

        nf, nres = {}, []
        for p, lines in news.items():
            f, r = parse("\n".join(lines))
            for k, v in f.items():
                nf[k] = v
            nres += r

        # 0. 重命名:按旧名取期望,按新名比对
        for old, new in RENAMES.items():
            if old in of:
                of[new] = of.pop(old)
                print(f"   已重命名: {old} → {new}")

        # 1. 有意删除的函数:从期望中整体剔除
        for name in sorted(ALLOWED_REMOVED_FUNCS):
            if name in of:
                print(f"   已删除(允许): {name} —— {ALLOWED_REMOVED_FUNCS[name]}")
                del of[name]
                seen_whitelist.add(name)

        # 2. 函数:缺失 / 多出
        for name in sorted(set(of) - set(nf)):
            print(f"   ✗ 函数缺失: {name}")
            failed = True
        for name in sorted(set(nf) - set(of)):
            if name in ALLOWED_NEW_FUNCS:
                print(f"   已新增(允许): {name} —— {ALLOWED_NEW_FUNCS[name]}")
                continue
            print(f"   ✗ 函数多出: {name}")
            failed = True

        # 3. 函数体与文档注释
        #     拼成单串再比:gofmt 会把单行匿名 struct 展开成多行(1 行变 3 行),
        #     逐行比对会被这种纯排版差异绊倒,拼串后则是同一串。
        def clean(body):
            return "".join(
                norm(l) for l in body
                if norm(l) and not any(re.search(p, norm(l)) for p in ALLOWED_LINE_PATTERNS)
            )

        changed_seen = set()
        for name in sorted(set(of) & set(nf)):
            # 阶段 2 有意改写的函数只校验存在性:内容正确性由 golden(契约)与
            # -race 并发测试(锁)覆盖,不靠逐字节比对。
            if name in INTENTIONALLY_CHANGED:
                changed_seen.add(name)
                continue
            ob, nb = clean(of[name]["body"]), clean(nf[name]["body"])
            if ob != nb:
                print(f"   ✗ 函数体有差异: {name}")
                failed = True
            od, nd = norm_doc(of[name]["doc"]), norm_doc(nf[name]["doc"])
            if od and not nd:
                print(f"   ✗ 文档注释丢失: {name}")
                failed = True
            elif od != nd:
                print(f"   ✗ 文档注释被改动: {name}")
                failed = True
        for name in sorted(changed_seen):
            print(f"   有意改动(不比内容): {name} —— {INTENTIONALLY_CHANGED[name]}")

        # 4. 注释块完整性(残余部分,即类型/常量/变量的文档注释)
        oc, nc = block_comments(ores), block_comments(nres)
        for c in sorted(oc - nc):
            if any(re.search(p, c) for p in ALLOWED_REMOVED_COMMENTS):
                print(f"   注释块已改写(允许): {c[:40]}…")
                continue
            print(f"   ✗ 注释块消失: {c[:70]}")
            failed = True

        # 5. 残余行守恒(类型、常量、变量、包注释等)
        def bag(lines):
            out = []
            for l in lines:
                n = norm(l)
                if not n or any(re.search(p, n) for p in ALLOWED_LINE_PATTERNS):
                    continue
                out.append(n)
            return out

        pool, missing = bag(nres), []
        for line in bag(ores):
            if line in pool:
                pool.remove(line)
            else:
                missing.append(line)
        if missing:
            print(f"   ✗ {len(missing)} 行残余内容丢失,例如:")
            for m in missing[:5]:
                print(f"       {m[:90]}")
            failed = True

    # 白名单里的函数若一个都没匹配上,说明条目已过期,提醒清理
    stale = set(ALLOWED_REMOVED_FUNCS) - seen_whitelist
    for name in sorted(stale):
        print(f"⚠ 白名单条目已过期(原文件里没有该函数): {name}")

    print()
    if failed:
        sys.exit("✗ 校验未通过:存在未登记的改动(补进白名单,或修正代码)")
    print("✓ 全部通过:除登记在案的改动外,内容与注释均保持原样")


if __name__ == "__main__":
    main()
