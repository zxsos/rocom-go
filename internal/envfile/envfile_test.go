package envfile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 本文件覆盖 envfile 的两条硬约束(见包注释):
//   1. 保留注释与未知键 —— 那份文件是给人在服务器上用 vi 看的
//   2. 原子写 —— 写崩了会让 systemd 起不来,等于把面板改崩

// sampleEnv 是 deploy.sh 生成的 /etc/rocom.env 的简化版,保留它的注释与空行结构。
const sampleEnv = `# rocom-capture 运行参数(改后执行: systemctl restart rocom)
# 抓包网卡(默认 eth0)
ROCOM_IFACE=eth0
# Web 监听地址(默认 :4939)
ROCOM_ADDR=:4939

# SOCKS5 代理(云端部署时用;留空=不启用)
ROCOM_SOCKS5_ADDR=
ROCOM_SOCKS5_USER=
ROCOM_SOCKS5_PASS=
`

func writeSample(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "rocom.env")
	if err := os.WriteFile(p, []byte(sampleEnv), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadGet(t *testing.T) {
	f, err := Load(writeSample(t))
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := f.Get("ROCOM_IFACE"); !ok || v != "eth0" {
		t.Errorf("ROCOM_IFACE = %q,%v; 期望 eth0,true", v, ok)
	}
	// 模板里留空的键:值应为空字符串且「存在」(区别于「没有这个键」)
	if v, ok := f.Get("ROCOM_SOCKS5_ADDR"); !ok || v != "" {
		t.Errorf("ROCOM_SOCKS5_ADDR = %q,%v; 期望 \"\",true", v, ok)
	}
	if _, ok := f.Get("ROCOM_NOPE"); ok {
		t.Error("不存在的键应返回 ok=false")
	}
}

func TestSetExistingKeepsComments(t *testing.T) {
	p := writeSample(t)
	f, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Set("ROCOM_SOCKS5_ADDR", ":1080"); err != nil {
		t.Fatal(err)
	}
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"# rocom-capture 运行参数(改后执行: systemctl restart rocom)",
		"# 抓包网卡(默认 eth0)",
		"ROCOM_IFACE=eth0",
		"# SOCKS5 代理(云端部署时用;留空=不启用)",
		"ROCOM_SOCKS5_ADDR=:1080",
		"ROCOM_SOCKS5_USER=",
	}
	for _, w := range want {
		if !strings.Contains(string(got), w) {
			t.Errorf("改后文件缺少 %q\n--- 实际 ---\n%s", w, got)
		}
	}
	// 注释一行都不能少
	if n := strings.Count(string(got), "#"); n < 3 {
		t.Errorf("注释被吃掉: 只剩 %d 个 '#',原文件有 3 行注释", n)
	}
	// 就地替换:行数不该变多(若是「追加新行 + 留着旧的」就会出现两行 ADDR)
	if n := strings.Count(string(got), "ROCOM_SOCKS5_ADDR="); n != 1 {
		t.Errorf("ROCOM_SOCKS5_ADDR 出现 %d 次,期望 1(应就地替换)", n)
	}
}

func TestSetNewKeyAppends(t *testing.T) {
	p := writeSample(t)
	f, _ := Load(p)
	if err := f.Set("ROCOM_SMTP_USER", "a@qq.com"); err != nil {
		t.Fatal(err)
	}
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if !strings.Contains(string(got), "ROCOM_SMTP_USER=a@qq.com") {
		t.Errorf("新键未追加:\n%s", got)
	}
	// 原有内容必须还在
	if !strings.Contains(string(got), "ROCOM_IFACE=eth0") {
		t.Error("追加新键时丢掉了原有内容")
	}
}

func TestQuoting(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"plain", "KEY=plain"},           // 无需引号
		{"", "KEY="},                     // 空值不加引号,与模板一致
		{"two words", `KEY="two words"`}, // 含空格
		{`has"quote`, `"has\"quote"`},    // 含引号需转义
		// 含 $ 必须转义:systemd 在双引号内同样做变量展开,不转义则 cost$5 会被展开掉
		{`cost$5`, `"cost\$5"`},
		{`back\slash`, `"back\\slash"`}, // 反斜杠
	}
	for _, c := range cases {
		f := &File{}
		if err := f.Set("KEY", c.in); err != nil {
			t.Fatal(err)
		}
		got := strings.TrimSpace(string(f.bytes()))
		if !strings.Contains(got, c.want) {
			t.Errorf("Set(%q) 序列化为 %q,期望含 %q", c.in, got, c.want)
		}
		// 往返:写出去再读回来应还原原值(引号不能把内容吃掉)
		p := filepath.Join(t.TempDir(), "e")
		if err := os.WriteFile(p, f.bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		r, err := Load(p)
		if err != nil {
			t.Fatal(err)
		}
		if v, _ := r.Get("KEY"); v != c.in {
			t.Errorf("往返后 = %q,期望 %q", v, c.in)
		}
	}
}

func TestRoundTripKeepsUnknownLines(t *testing.T) {
	raw := "# 顶部注释\nEXISTING=1\n\n   # 缩进注释\nBARE_NO_EQUALS\nBAD KEY=oops\n"
	p := filepath.Join(t.TempDir(), "e")
	if err := os.WriteFile(p, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	f, _ := Load(p)
	f.Set("EXISTING", "2")
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	for _, want := range []string{"# 顶部注释", "   # 缩进注释", "BARE_NO_EQUALS", "BAD KEY=oops", "EXISTING=2"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("往返后丢失 %q:\n%s", want, got)
		}
	}
}

func TestSetRejectsBadKey(t *testing.T) {
	f := &File{}
	for _, k := range []string{"", "HAS SPACE", "HAS=EQ", "1STARTS_DIGIT", `HAS"QUOTE`} {
		if err := f.Set(k, "v"); err == nil {
			t.Errorf("Set(%q) 应报错(会破坏文件语法)", k)
		}
	}
}

// TestSetRejectsNewline 守一条容易漏的值校验:换行会让 env 文件语法崩掉
// (一行变两行,后半截变成 systemd 看不懂的东西,服务起不来)。宁可拒绝写入,
// 也不能把 /etc/rocom.env 写成废品 —— 那等于管理员把自己锁在系统外面。
func TestSetRejectsNewline(t *testing.T) {
	f := &File{}
	for _, v := range []string{"a\nb", "a\r\nb", "\n", "a\r"} {
		if err := f.Set("KEY", v); err == nil {
			t.Errorf("Set 值 %q 应报错,实际通过(会写出坏文件)", v)
		}
	}
	// 普通值仍要能通过:别把校验写得太宽
	if err := f.Set("KEY", "normal pass word $pecial"); err != nil {
		t.Errorf("含空格与 $ 的普通密码被误拒: %v", err)
	}
}

func TestSaveAtomicKeepsMode(t *testing.T) {
	p := writeSample(t)
	if err := os.Chmod(p, 0o640); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	f, _ := Load(p)
	f.Set("ROCOM_IFACE", "br-lan")
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Errorf("保存后权限 = %v,期望沿用 0640", info.Mode().Perm())
	}
	// inode 必须变:rename 过去的是另一个文件。若哪天改成原地覆盖写
	// (os.WriteFile 走 O_TRUNC,inode 不变),这里会失败 —— 那正是要防的回退:
	// 原地写期间进程一旦中断,文件就是截断的残骸,systemd 读不懂,服务起不来。
	if os.SameFile(before, info) {
		t.Error("保存后 inode 未变:说明是原地覆盖写而非原子替换(中断会留下截断文件)")
	}
	// 临时文件必须清理干净,不能留在目录里
	des, _ := os.ReadDir(filepath.Dir(p))
	for _, d := range des {
		if strings.HasPrefix(d.Name(), ".envfile-") {
			t.Errorf("残留临时文件: %s", d.Name())
		}
	}
}

func TestSaveNewFileDefaultsTo0600(t *testing.T) {
	// 文件里会存 SMTP 授权码与代理密码:新建时默认必须私有。
	p := filepath.Join(t.TempDir(), "new.env")
	f := &File{path: p}
	f.Set("ROCOM_SMTP_PASS", "secret")
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(p)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("新建文件权限 = %v,期望 0600(含密钥)", info.Mode().Perm())
	}
}

// TestSaveFailureKeepsOriginal 守整个包最重要的一条性质:**写失败不能破坏原文件**。
//
// 原文件是服务唯一的启动配置来源。若 Save 是直接覆盖写,写到一半失败就会留下截断的
// 文件 —— systemd 读不懂 → 服务起不来 → 管理员刚在面板上把配置改崩了,而面板随服务
// 一起没了,人不在机器旁边就改不回来。
//
// 原子写(临时文件 + rename)在正常路径下看不出差别,只有注入故障才能验证,
// 故用 testBeforeRename 在 rename 前一刻制造失败。
func TestSaveFailureKeepsOriginal(t *testing.T) {
	p := writeSample(t)
	f, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Set("ROCOM_IFACE", "br-lan"); err != nil {
		t.Fatal(err)
	}

	testBeforeRename = func() error { return errors.New("注入失败") }
	defer func() { testBeforeRename = nil }()

	if err := f.Save(); err == nil {
		t.Fatal("Save 在注入失败时应返回错误")
	}

	// 原文件必须逐字节不变:IFACE 仍是 eth0,注释也还在
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != sampleEnv {
		t.Errorf("保存失败后原文件被改动了:\n--- 实际 ---\n%s\n--- 期望 ---\n%s", got, sampleEnv)
	}
	// 也不能留下临时文件
	des, _ := os.ReadDir(filepath.Dir(p))
	for _, d := range des {
		if strings.HasPrefix(d.Name(), ".envfile-") {
			t.Errorf("失败后残留临时文件: %s", d.Name())
		}
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.env")); err == nil {
		t.Error("不存在的文件应报错(调用方据此判定非 systemd 部署)")
	}
}

func TestWritable(t *testing.T) {
	p := writeSample(t)
	if !Writable(p) {
		t.Error("刚创建的 0600 文件应可写")
	}
	if Writable(filepath.Join(t.TempDir(), "nope.env")) {
		t.Error("不存在的文件不应报可写")
	}
	if Writable(t.TempDir()) {
		t.Error("目录不应报可写")
	}
}
