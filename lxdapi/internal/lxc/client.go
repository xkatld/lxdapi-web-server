package lxc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"lxdapi/internal/core"
	"lxdapi/pkg/logger"
	"os/exec"
	"strings"
	"time"
)

type Client struct {
	socket  string
	timeout time.Duration
}

func NewClient() *Client {
	cfg := core.GlobalConfig.LXC
	return &Client{
		socket:  cfg.Socket,
		timeout: time.Duration(cfg.Timeout) * time.Second,
	}
}

func (c *Client) exec(ctx context.Context, args ...string) (string, error) {
	return c.execWithStdin(ctx, "", args...)
}

func (c *Client) execWithStdin(ctx context.Context, stdin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "lxc", args...)
	var stdout, stderr bytes.Buffer
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	logger.Info("执行LXC命令: lxc %s", strings.Join(args, " "))

	err := cmd.Run()
	if err != nil {
		errMsg := stderr.String()
		if errMsg == "" {
			errMsg = err.Error()
		}
		logger.Error("LXC命令执行失败: %s", errMsg)
		return "", fmt.Errorf("%s", errMsg)
	}

	return stdout.String(), nil
}

func (c *Client) execJSON(ctx context.Context, result interface{}, args ...string) error {
	output, err := c.exec(ctx, append(args, "--format=json")...)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(output), result)
}

