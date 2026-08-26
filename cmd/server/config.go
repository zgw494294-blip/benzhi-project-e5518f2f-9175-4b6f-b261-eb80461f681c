package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type config struct {
	Address     string
	JournalPath string
	Selfcheck   bool
}

func parseConfig(args []string, portEnv string) (config, error) {
	defaultAddr := defaultAddress
	if strings.TrimSpace(portEnv) != "" {
		port, err := strconv.Atoi(portEnv)
		if err != nil || port < 1 || port > 65535 {
			return config{}, fmt.Errorf("PORT 必须是 1 到 65535 的端口号")
		}
		defaultAddr = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	}
	flags := flag.NewFlagSet("oral-history-clearance", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	addr := flags.String("addr", defaultAddr, "回环监听地址")
	journalPath := flags.String("journal", filepath.Join("data", "oral-history-clearance.jsonl"), "事件日志路径")
	selfcheck := flags.Bool("selfcheck", false, "运行完整回环自检后退出")
	if err := flags.Parse(args); err != nil {
		return config{}, err
	}
	if flags.NArg() != 0 {
		return config{}, fmt.Errorf("存在未识别参数: %s", strings.Join(flags.Args(), " "))
	}
	validated, err := validateAddress(*addr)
	if err != nil {
		return config{}, err
	}
	if strings.TrimSpace(*journalPath) == "" {
		return config{}, fmt.Errorf("journal 路径不能为空")
	}
	return config{Address: validated, JournalPath: *journalPath, Selfcheck: *selfcheck}, nil
}

func validateAddress(addr string) (string, error) {
	host, portText, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return "", fmt.Errorf("addr 必须采用 host:port 格式: %w", err)
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return "", fmt.Errorf("addr 必须绑定回环地址，拒绝 %q", host)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("addr 端口必须介于 1 和 65535")
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}
