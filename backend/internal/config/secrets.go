package config

import (
	"fmt"
	"os"
	"strings"
)

// secretFileSuffix 是"从文件读取敏感项"的环境变量后缀约定：X_FILE 的值是文件路径，
// 文件内容就是 X 的值。这是 Docker Secret、Kubernetes Secret 卷挂载以及
// Vault Agent 注入的通用形态，避免密码以明文出现在进程环境变量里。
//
// 具体变量名在 secretTargets 里逐条写出、不在读取处拼接，这样在仓库里搜
// DB_PASSWORD_FILE 能直接落到这张表和下面的读取逻辑上，而不是只搜到文档。
const secretFileSuffix = "_FILE"

// secretTarget 把一个敏感项的两种来源绑定到同一个配置字段：
//
//	env     值内联，例如 DB_PASSWORD=secret
//	fileEnv 路径内联，例如 DB_PASSWORD_FILE=/run/secrets/db_password
//
// 两者互斥，判定规则见 applySecretFiles。
type secretTarget struct {
	env     string
	fileEnv string
	dest    *string
}

// secretTargets 登记全部支持文件注入的敏感项。新增一项时 env 与 fileEnv 都要写；
// TestSecretFileEnvNamesFollowSuffix 会检查 fileEnv 严格等于 env+"_FILE"，
// 防止手写变量名与约定漂移。
func secretTargets(cfg *Config) []secretTarget {
	return []secretTarget{
		{env: "JWT_SECRET", fileEnv: "JWT_SECRET_FILE", dest: &cfg.JWT.Secret},
		{env: "DB_PASSWORD", fileEnv: "DB_PASSWORD_FILE", dest: &cfg.Database.Password},
		{env: "REDIS_PASSWORD", fileEnv: "REDIS_PASSWORD_FILE", dest: &cfg.Redis.Password},
		{env: "REDIS_SENTINEL_PASSWORD", fileEnv: "REDIS_SENTINEL_PASSWORD_FILE", dest: &cfg.Redis.SentinelPassword},
		{env: "BOOTSTRAP_ADMIN_PASSWORD", fileEnv: "BOOTSTRAP_ADMIN_PASSWORD_FILE", dest: &cfg.Bootstrap.AdminPassword},
	}
}

// applySecretFiles 把 X_FILE 指向的文件内容写入对应字段，位置在 env.Parse 之后、
// Validate 之前：文件内容与直接设置 X 完全等价，因此长度、占位符、生产环境策略
// 等校验照常生效。
//
// 规则是"宁可不启动，也不猜"：
//   - X 与 X_FILE 同时非空 → 报错，不定义优先级，避免"改了文件却被环境变量盖住"；
//   - X_FILE 已设置但为空串 → 报错，这类值通常来自流水线里缺失的变量；
//   - 文件不存在、不可读或内容为空 → 报错，绝不让密码静默退化成空值；
//   - 只去掉文件末尾的换行（编辑器和 Secret 挂载常带换行），其余字节原样保留。
func applySecretFiles(cfg *Config) error {
	var p problems
	for _, target := range secretTargets(cfg) {
		path, ok := os.LookupEnv(target.fileEnv)
		if !ok {
			continue
		}
		path = strings.TrimSpace(path)
		if path == "" {
			p.addf("%s is set but empty", target.fileEnv)
			continue
		}
		if value, ok := os.LookupEnv(target.env); ok && value != "" {
			p.addf("%s and %s are both set; provide the secret through only one of them (comment out the one you do not use, e.g. in .env)", target.env, target.fileEnv)
			continue
		}

		value, err := readSecretFile(path)
		if err != nil {
			p.addf("%s: %v", target.fileEnv, err)
			continue
		}
		// 写入配置树：后续 Validate 与消费方看到的都是普通字段值。
		*target.dest = value
	}
	return p.err()
}

// readSecretFile 读取并清理一个密钥文件。去掉末尾换行是唯一的内容加工：
// echo 出来的密码与 Secret 卷挂载都会带一个换行，保留它会让"看起来一样"的
// 两个密码认证失败。
func readSecretFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	value := strings.TrimRight(string(data), "\r\n")
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("file %q is empty", path)
	}
	return value, nil
}
