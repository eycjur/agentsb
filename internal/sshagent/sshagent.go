// Package sshagent はホスト SSH agent の転送準備を行う。
// 転送そのものは sbx がホストの SSH_AUTH_SOCK を見て行う。
// agentsb はホスト側の前提確認（IdentityAgent → SSH_AUTH_SOCK）、
// GitHub SSH 向け network allow、サンドボックス内プローブまでを担当する。
package sshagent

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"agentsb/internal/config"
	"agentsb/internal/runlog"
	"agentsb/internal/sandbox"
)

const probeTimeout = 30 * time.Second

// gitSSHHosts は Git over SSH で使うホスト。非 HTTP のため hostname:22 は
// 使えず、解決した IP:22 を policy allow する。
var gitSSHHosts = []string{"github.com", "ssh.github.com"}

// EnsureHost は [ssh].identity_agent を SSH_AUTH_SOCK に載せ、ソケット到達と
// ssh-add -l で identity があることを確認する。満たさなければエラー。
func EnsureHost(cfg config.SSHConfig) error {
	sock, err := expandPath(cfg.IdentityAgent)
	if err != nil {
		return err
	}
	if err := os.Setenv("SSH_AUTH_SOCK", sock); err != nil {
		return fmt.Errorf("set SSH_AUTH_SOCK: %w", err)
	}
	runlog.Info("sshagent: set SSH_AUTH_SOCK from [ssh].identity_agent=%s", sock)

	if _, err := os.Stat(sock); err != nil {
		return fmt.Errorf("SSH_AUTH_SOCK=%s is not reachable: %w", sock, err)
	}

	out, err := exec.Command("ssh-add", "-l").CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("ssh-add -l failed: %s — unlock 1Password / ensure the agent has keys", detail)
	}
	if strings.Contains(string(out), "The agent has no identities") {
		return fmt.Errorf("SSH agent has no identities — unlock 1Password or enable SSH keys in the agent")
	}
	runlog.Info("sshagent: host identities ok\n%s", strings.TrimSpace(string(out)))
	return nil
}

// PrepareSandbox は GitHub SSH 向けに IP:22 を allow し、サンドボックス内で
// ssh-add -l をプローブする。プローブ失敗は警告のみ（セッションは続行）。
func PrepareSandbox(name string) error {
	hosts, err := resolveSSHAllowHosts(gitSSHHosts)
	if err != nil {
		return err
	}
	if len(hosts) > 0 {
		runlog.Info("sshagent: allow network %s", strings.Join(hosts, ","))
		if err := sandbox.AllowNetwork(name, hosts); err != nil {
			return fmt.Errorf("policy allow for git SSH: %w", err)
		}
	}

	if err := probeSandboxAgent(name); err != nil {
		runlog.Warn("sshagent: sandbox probe failed: %v", err)
		fmt.Fprintf(os.Stderr, "agentsb: warning: SSH agent forwarding probe failed in sandbox: %v\n", err)
		fmt.Fprintf(os.Stderr, "agentsb: warning: custom templates may not forward SSH_AUTH_SOCK (https://github.com/docker/sbx-releases/issues/247); use GH_TOKEN or check host agent\n")
		return nil
	}
	runlog.Info("sshagent: sandbox ssh-add -l ok")
	fmt.Fprintf(os.Stderr, "agentsb: SSH agent forwarding looks available in the sandbox\n")
	return nil
}

func resolveSSHAllowHosts(names []string) ([]string, error) {
	seen := map[string]struct{}{}
	var hosts []string
	for _, name := range names {
		ips, err := net.LookupIP(name)
		if err != nil {
			runlog.Warn("sshagent: lookup %s: %v", name, err)
			continue
		}
		for _, ip := range ips {
			entry := ip.String() + ":22"
			if _, ok := seen[entry]; ok {
				continue
			}
			seen[entry] = struct{}{}
			hosts = append(hosts, entry)
		}
	}
	if len(hosts) == 0 {
		return nil, fmt.Errorf("could not resolve any of %v for SSH policy allow", names)
	}
	return hosts, nil
}

func probeSandboxAgent(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sbx", "exec", name, "ssh-add", "-l")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	combined := strings.TrimSpace(string(out) + "\n" + stderr.String())
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("timed out after %s", probeTimeout)
	}
	if err != nil {
		if combined != "" {
			return fmt.Errorf("%w: %s", err, combined)
		}
		return err
	}
	if strings.Contains(combined, "The agent has no identities") {
		return fmt.Errorf("agent reachable but has no identities")
	}
	if strings.Contains(combined, "Error connecting to agent") ||
		strings.Contains(combined, "Could not open a connection to your authentication agent") {
		return fmt.Errorf("%s", combined)
	}
	return nil
}

func expandPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("[ssh].identity_agent: empty path")
	}
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand %s: %w", p, err)
		}
		p = filepath.Join(home, p[2:])
	} else if p == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand ~: %w", err)
		}
		p = home
	}
	return filepath.Clean(p), nil
}
