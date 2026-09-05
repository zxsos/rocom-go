// Package envfile 读写 systemd 的 EnvironmentFile 格式(KEY=VALUE 文本),供管理面板
// 修改 /etc/rocom.env 用。
//
// 为什么要有这个包:部署由 systemd 管理,启动参数存在 /etc/rocom.env 里
// (由 scripts/deploy.sh 生成,/opt/rocom/run.sh 把每项组装成 flag)。管理员在面板上改配置,
// 最终必须落到那个文件 —— 否则重启后配置就丢了。故它是配置的**唯一落盘位置**,
// 进程内存里的值只是「不必重启的加速缓存」。
//
// 两条硬约束:
//
//  1. **必须保留注释与未知键**。deploy.sh 生成的 env 文件里带着「改后执行 systemctl restart」
//     这类运维提示,注释掉了的键(如 ROCOM_SOCKS5_ADDR=)也是有意留的空位。用 map 读一遍再
//     整个重写会把这些全抹掉 —— 那份文件是给人在服务器上用 vi 看的,不是纯数据。
//     故这里按**行**处理:认得的键就地替换值,不认得的原样保留。
//  2. **必须原子写**。文件里存着 SMTP 授权码与 socks5 密码;写一半崩了会留下截断的文件,
//     而 systemd 的 EnvironmentFile= 遇到语法错误会让服务起不来 —— 那等于把面板改崩了。
//     故写临时文件 + rename,并保持 0600。
package envfile

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// entry 是文件里的一行:要么是键值对,要么是原样保留的文本(注释/空行/无法识别的行)。
type entry struct {
	key   string // 空表示非键值对
	value string
	raw   string // 非键值对时的原始行
}

// File 是一份已解析的 env 文件,保留原有行序与注释。
type File struct {
	path    string
	entries []entry
}

// Load 读取并解析 env 文件。文件不存在时返回错误(调用方据此判定「不被 systemd 管理」)。
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	f := &File{path: path}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 单行上限 1MB,防异常文件吃内存
	for sc.Scan() {
		f.entries = append(f.entries, parseLine(sc.Text()))
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return f, nil
}

// parseLine 解析一行。空行/注释/没有 '=' 的行原样保留。
func parseLine(line string) entry {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return entry{raw: line}
	}
	eq := strings.Index(trimmed, "=")
	if eq < 0 {
		return entry{raw: line} // 无 '=' ,不是我们能理解的键值对:原样留着
	}
	key := strings.TrimSpace(trimmed[:eq])
	if key == "" || !validKey(key) {
		return entry{raw: line}
	}
	return entry{key: key, value: unquote(strings.TrimSpace(trimmed[eq+1:]))}
}

// validKey 校验键名:shell 变量名(字母/数字/下划线,非数字开头)。
// 挡下含空格、引号、'=' 这类会破坏文件语法的名字 —— 键名来自管理面板的输入,不可信。
func validKey(k string) bool {
	if k == "" {
		return false
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_':
		case c >= '0' && c <= '9':
			if i == 0 {
				return false // 不能以数字开头
			}
		default:
			return false
		}
	}
	return true
}

// unquote 剥掉成对引号并解转义。systemd 支持 "..." 与 '...';只有一侧有引号、
// 或引号未闭合时不动(那是写坏了的值,保持原样交给 systemd 去报错,比我们悄悄改掉更安全)。
//
// 必须与 quote 严格互逆:值里含引号/反斜杠的是密码类配置,往返一次多出一个反斜杠
// 就等于把密码改掉了 —— 而它又刚好还能认证成功几小时,极难排查。
func unquote(v string) string {
	if len(v) < 2 {
		return v
	}
	var q byte
	switch v[0] {
	case '"':
		q = '"'
	case '\'':
		q = '\'' // 单引号内 systemd 不做转义,故下面只处理双引号
	default:
		return v
	}
	if v[len(v)-1] != q {
		return v // 未闭合:原样返回
	}
	inner := v[1 : len(v)-1]
	if q == '\'' {
		return inner
	}
	if !strings.Contains(inner, `\`) {
		return inner
	}
	var b strings.Builder
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		if c != '\\' || i+1 >= len(inner) {
			b.WriteByte(c)
			continue
		}
		i++
		switch inner[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case '\\':
			b.WriteByte('\\')
		case '"':
			b.WriteByte('"')
		case '$':
			b.WriteByte('$')
		default:
			// 未识别的转义序列:连同反斜杠一起保留(可能是别处写的值,别丢字符)
			b.WriteByte('\\')
			b.WriteByte(inner[i])
		}
	}
	return b.String()
}

// Get 返回某个键的当前值;不存在返回 ("", false)。
func (f *File) Get(key string) (string, bool) {
	for _, e := range f.entries {
		if e.key == key {
			return e.value, true
		}
	}
	return "", false
}

// Set 设置键值:键已存在则就地替换(注释与位置不变),不存在则追加到文件末尾。
// 值含换行时报错 —— 它在 EnvironmentFile 里无法无歧义地表示,宁可拒绝也不写坏文件。
func (f *File) Set(key, value string) error {
	if !validKey(key) {
		return fmt.Errorf("envfile: 非法键名 %q", key)
	}
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("envfile: 值不能含换行(键 %s)", key)
	}
	for i := range f.entries {
		if f.entries[i].key == key {
			f.entries[i].value = value
			return nil
		}
	}
	f.entries = append(f.entries, entry{key: key, value: value})
	return nil
}

// bytes 序列化整个文件。值含空格/引号/'$' 时加双引号并转义,避免 systemd 解析错位。
func (f *File) bytes() []byte {
	var b strings.Builder
	for _, e := range f.entries {
		if e.key == "" {
			b.WriteString(e.raw)
		} else {
			b.WriteString(e.key)
			b.WriteByte('=')
			b.WriteString(quote(e.value))
		}
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

// quote 决定是否给值加引号。为空则不加(写 KEY= 而非 KEY="",后者 systemd 会解成空串,
// 两者语义其实一致,但前者与 deploy.sh 生成的模板一致,diff 更干净)。
// 转义序列须与 unquote 严格互逆(见 unquote 注释)。
func quote(v string) string {
	if v == "" {
		return ""
	}
	if !strings.ContainsAny(v, " \t\"'\\$#") {
		return v
	}
	return `"` + strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"\t", `\t`,
		"$", `\$`,
	).Replace(v) + `"`
}

// testBeforeRename 是测试注入点:非 nil 时 Save 会在**写完临时文件之后、rename 之前**
// 调用它,返回非 nil 即中止(见 envfile_test.go 的 TestSaveFailureKeepsOriginal)。
//
// 存在的理由:「写失败不能破坏原文件」是这个文件最重要的性质 —— 原文件是服务唯一的
// 启动配置,写坏了服务就起不来,而管理员此时已经在面板上、连不上就改不回来。
// 单靠正常路径的测试无从验证:不注入故障,Save 永远成功。
// 与 state.go 里 smtpSender 的 sendFn/dialFn 同一套路。
var testBeforeRename func() error

// SetTestBeforeRename 设置/清除测试注入点,返回旧值。
//
// 存在理由同 testBeforeRename,但那个变量是包私有的,而「落盘失败不能改内存」
// 这条不变量的验证点在 internal/server —— 配置端点才是把两者串起来的地方。
// 生产代码永不调用(名字带 Test 是刻意的:grep 一眼就能看出它只该出现在 _test.go)。
func SetTestBeforeRename(fn func() error) func() error {
	old := testBeforeRename
	testBeforeRename = fn
	return old
}

// Save 原子写回文件:写同目录临时文件 → fsync → rename。
// 权限固定 0600 —— 文件里可能有 SMTP 授权码与代理密码,不能让同机其它用户读到。
// 原文件的权限会被沿用(若它是 0644,说明管理员有意放宽,不该由我们收紧后引发困惑)。
func (f *File) Save() error {
	mode := os.FileMode(0o600)
	if info, err := os.Stat(f.path); err == nil {
		mode = info.Mode().Perm()
	}
	data := f.bytes()

	dir := filepath.Dir(f.path)
	tmp, err := os.CreateTemp(dir, ".envfile-*.tmp")
	if err != nil {
		return fmt.Errorf("envfile: 创建临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	// 失败路径统一清理:下面任何一步出错都把临时文件删掉,不留垃圾
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("envfile: 写入临时文件失败: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return fmt.Errorf("envfile: 设置权限失败: %w", err)
	}
	// rename 前必须 Sync:否则掉电后 rename 已生效而数据还在页缓存里,得到空文件
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("envfile: 刷盘失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("envfile: 关闭临时文件失败: %w", err)
	}
	// 此刻临时文件已完整落盘、原文件还**一个字节都没动**:从这里到 rename 之间无论
	// 怎么失败,原文件都还是好的。这正是原子写的意义,由 TestSaveFailureKeepsOriginal 守住。
	if testBeforeRename != nil {
		if err := testBeforeRename(); err != nil {
			return err
		}
	}
	if err := os.Rename(tmpName, f.path); err != nil {
		return fmt.Errorf("envfile: 替换文件失败: %w", err)
	}
	return nil
}

// Writable 报告该文件是否可写(存在且当前进程有写权限)。
// 面板据此决定「可编辑」还是「只读 + 提示命令行改法」—— 手动跑的进程(如 -pcap 回放)
// 不该假装能改一个它其实改不动的文件。
func Writable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return false
		}
		return false
	}
	f.Close()
	return true
}
