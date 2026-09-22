package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"cyannvr/server/auth"
	"cyannvr/server/config"
	"cyannvr/server/models"
	"cyannvr/server/store"
)

// 命令行工具：忘记密码时的兜底手段。
// 与 Web 界面共用同一个二进制，因此容器 / fpk / 裸机三种部署方式都能用：
//
//   容器: docker exec -it cyannvr /app/cyannvr reset-password
//   fpk : /var/apps/CyanNVR/target/cyannvr reset-password
//   裸机: ./cyannvr reset-password
//
// 数据目录取自 NVR_DATA（容器与 fpk 均已设置），未设置时回退到 ./data。

const cliMinPassword = 8

func runCLI(args []string) {
	switch args[0] {
	case "reset-password", "reset":
		os.Exit(cliResetPassword(args[1:]))
	case "list-users", "users":
		os.Exit(cliListUsers(args[1:]))
	case "help", "-h", "--help":
		printCLIUsage(os.Stdout)
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "未知命令: %s\n\n", args[0])
		printCLIUsage(os.Stderr)
		os.Exit(1)
	}
}

func printCLIUsage(w *os.File) {
	fmt.Fprint(w, `CyanNVR 命令行工具

用法:
  cyannvr                      启动服务
  cyannvr reset-password [选项]  重置用户密码（忘记密码时使用）
  cyannvr list-users           列出所有用户
  cyannvr help                 显示本帮助

reset-password 选项:
  --user NAME        要重置的用户名；省略时自动选择唯一的管理员
  --password NEW     新密码；省略则交互式输入（不回显）
  --data DIR         覆盖数据目录（默认取环境变量 NVR_DATA，否则 ./data）

示例:
  cyannvr reset-password                          # 重置唯一管理员的密码
  cyannvr reset-password --user admin             # 指定用户，交互式输入新密码
  cyannvr reset-password --user admin --password 'NewPass123'

注意: 密码至少 8 个字符。命令行传入 --password 会留在 shell 历史中，
      交互式输入更安全。
`)
}

func openStoreForCLI(dataDir string) (*store.Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}
	dbPath := filepath.Join(dataDir, "nvr.db")
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("找不到数据库 %s（请确认数据目录是否正确，可用 --data 指定）", dbPath)
	}
	return store.Open(dbPath)
}

func cliListUsers(args []string) int {
	fs := flag.NewFlagSet("list-users", flag.ContinueOnError)
	dataDir := fs.String("data", "", "覆盖数据目录")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	dir := resolveDataDir(*dataDir)
	st, err := openStoreForCLI(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		return 1
	}
	defer st.Close()

	users, err := st.ListUsers()
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取用户失败: %v\n", err)
		return 1
	}
	fmt.Printf("数据目录: %s\n\n", dir)
	fmt.Printf("%-24s %-12s %s\n", "用户名", "角色", "创建时间")
	for _, u := range users {
		fmt.Printf("%-24s %-12s %s\n", u.Username, u.Role, u.CreatedAt.Format("2006-01-02 15:04:05"))
	}
	return 0
}

func cliResetPassword(args []string) int {
	fs := flag.NewFlagSet("reset-password", flag.ContinueOnError)
	user := fs.String("user", "", "要重置的用户名")
	password := fs.String("password", "", "新密码（省略则交互式输入）")
	dataDir := fs.String("data", "", "覆盖数据目录")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	dir := resolveDataDir(*dataDir)
	st, err := openStoreForCLI(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		return 1
	}
	defer st.Close()

	// 1. 选定用户
	target, err := pickUser(st, *user)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		return 1
	}

	// 2. 取得新密码
	pw := *password
	if pw == "" {
		pw, err = promptPasswordTwice()
		if err != nil {
			fmt.Fprintf(os.Stderr, "错误: %v\n", err)
			return 1
		}
	}
	if len(pw) < cliMinPassword {
		fmt.Fprintf(os.Stderr, "错误: 密码至少需要 %d 个字符\n", cliMinPassword)
		return 1
	}

	// 3. 写入
	// JWT 密钥只用于签发 token，重置密码不涉及签发，这里传空串即可。
	am := auth.NewManager("")
	hash, err := am.HashPassword(pw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "生成密码哈希失败: %v\n", err)
		return 1
	}
	if err := st.UpdateUserPassword(target.ID, hash); err != nil {
		fmt.Fprintf(os.Stderr, "更新密码失败: %v\n", err)
		return 1
	}

	fmt.Printf("✓ 已重置用户 %q 的密码\n", target.Username)
	fmt.Printf("  数据目录: %s\n", dir)
	fmt.Println("  现在可以用新密码登录；若服务正在运行，无需重启即生效。")

	// 顺带清理可能残留的重置码文件（走 CLI 就不需要它了）
	codeFile := filepath.Join(dir, "reset-code.txt")
	if _, err := os.Stat(codeFile); err == nil {
		_ = os.Remove(codeFile)
		fmt.Println("  已清理未使用的重置码文件 reset-code.txt")
	}
	return 0
}

func resolveDataDir(override string) string {
	if override != "" {
		return override
	}
	// 与服务端保持同一套解析逻辑：读 NVR_DATA，缺省 ./data
	return config.Load().DataDir
}

// pickUser 选择目标用户：显式指定优先，否则要求系统里只有一个管理员。
func pickUser(st *store.Store, name string) (*models.User, error) {
	if name != "" {
		u, err := st.GetUserByName(name)
		if err != nil {
			return nil, fmt.Errorf("查询用户失败: %w", err)
		}
		if u == nil {
			return nil, fmt.Errorf("用户不存在: %s（可用 cyannvr list-users 查看）", name)
		}
		return u, nil
	}

	users, err := st.ListUsers()
	if err != nil {
		return nil, fmt.Errorf("查询用户失败: %w", err)
	}
	if len(users) == 0 {
		return nil, fmt.Errorf("系统中还没有任何用户，请先启动服务完成初始化")
	}
	var admins []models.User
	for _, u := range users {
		if u.Role == models.RoleAdmin {
			admins = append(admins, u)
		}
	}
	switch len(admins) {
	case 0:
		return nil, fmt.Errorf("没有管理员账号，请用 --user 指定要重置的用户")
	case 1:
		return &admins[0], nil
	default:
		names := make([]string, 0, len(admins))
		for _, a := range admins {
			names = append(names, a.Username)
		}
		return nil, fmt.Errorf("存在多个管理员（%s），请用 --user 指定", strings.Join(names, "、"))
	}
}

// promptPasswordTwice 交互式读取两次密码并比对；终端下关闭回显。
func promptPasswordTwice() (string, error) {
	first, err := readPassword("请输入新密码: ")
	if err != nil {
		return "", err
	}
	if first == "" {
		return "", fmt.Errorf("密码不能为空")
	}
	second, err := readPassword("请再次输入确认: ")
	if err != nil {
		return "", err
	}
	if first != second {
		return "", fmt.Errorf("两次输入的密码不一致")
	}
	return first, nil
}

// readPassword 读取一行输入。若 stdin 是终端则先关闭回显，避免密码显示在屏幕上。
func readPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)

	if isTerminal() {
		if c := exec.Command("stty", "-echo"); c != nil {
			c.Stdin = os.Stdin
			_ = c.Run()
		}
		defer func() {
			c := exec.Command("stty", "echo")
			c.Stdin = os.Stdin
			_ = c.Run()
			fmt.Fprintln(os.Stderr)
		}()
	}

	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func isTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
