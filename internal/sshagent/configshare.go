package sshagent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"

	"agentsb/internal/runlog"
	"agentsb/internal/sandbox"
)

const sshConfigIncludeDepth = 8

// sandboxSSHDir はサンドボックス内の agent ユーザーの ~/.ssh。
var sandboxSSHDir = filepath.Join(sandbox.HomeDir, ".ssh")

// sandboxSSHAgentSock は sbx がサンドボックス内に出す転送先ソケット。
// ホストの IdentityAgent（1Password のパスなど）はそのまま置くと
// ~ が /home/agent に展開され、存在しないソケットを見にいく。
const sandboxSSHAgentSock = "/run/ssh-agent.sock"

// dropSSHKeywords はサンドボックスへ置かない指定。
// IdentityFile はホストの鍵パスを指す。秘密鍵はコピーしない。
// IdentitiesOnly は対になる IdentityFile を外したあとも残すと、agent の鍵を
// 使わなくなる。
var dropSSHKeywords = map[string]struct{}{
	"identityfile":   {},
	"identitiesonly": {},
}

// InjectConfig はホストの ~/.ssh/config をサンドボックスの ~/.ssh/config へコピーする。
// ホスト側が正で、毎回上書きする。書き戻しはしない。ファイルが無ければ何もしない。
// Include は展開して 1 ファイルにまとめる。秘密鍵はコピーしない。
func InjectConfig(name string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}
	sshDir := filepath.Join(home, ".ssh")
	hostPath := filepath.Join(sshDir, "config")
	if _, err := os.Stat(hostPath); os.IsNotExist(err) {
		runlog.Notice("ssh config: skip %s (host file not found)", hostPath)
		return nil
	} else if err != nil {
		return fmt.Errorf("cannot stat %s: %w", hostPath, err)
	}

	body, err := renderUserSSHConfig(sshDir)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp("", "agentsb-ssh-config-*")
	if err != nil {
		return fmt.Errorf("cannot create temp ssh config: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(body); err != nil {
		tmp.Close()
		return fmt.Errorf("cannot write temp ssh config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cannot close temp ssh config: %w", err)
	}

	containerPath := filepath.Join(sandboxSSHDir, "config")
	if err := execInSandbox(name, "sudo", "mkdir", "-p", sandboxSSHDir); err != nil {
		return fmt.Errorf("cannot create %s: %w", sandboxSSHDir, err)
	}
	if err := execInSandbox(name, "sudo", "chmod", "700", sandboxSSHDir); err != nil {
		return fmt.Errorf("cannot chmod %s: %w", sandboxSSHDir, err)
	}
	if err := execInSandbox(name, "sudo", "chown", "agent:agent", sandboxSSHDir); err != nil {
		return fmt.Errorf("cannot chown %s: %w", sandboxSSHDir, err)
	}
	if err := sandbox.CopyToSandbox(name, tmpPath, containerPath); err != nil {
		return fmt.Errorf("cannot inject %s: %w", containerPath, err)
	}
	if err := sandbox.ChownAgent(name, containerPath); err != nil {
		return fmt.Errorf("cannot fix ownership of %s: %w", containerPath, err)
	}
	if err := execInSandbox(name, "sudo", "chmod", "600", containerPath); err != nil {
		return fmt.Errorf("cannot chmod %s: %w", containerPath, err)
	}
	runlog.Notice("ssh config: copied %s -> sandbox:%s", hostPath, containerPath)
	return nil
}

// renderUserSSHConfig は sshDir/config を、サンドボックスへ置ける本文にする。
func renderUserSSHConfig(sshDir string) (string, error) {
	return renderSSHFile(filepath.Join(sshDir, "config"), sshDir, 0, map[string]struct{}{})
}

func renderSSHFile(path, sshDir string, depth int, seen map[string]struct{}) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("ssh config %s: %w", path, err)
	}
	if _, ok := seen[abs]; ok {
		runlog.Warn("ssh config: skip include cycle %s", abs)
		return "", nil
	}
	if depth > sshConfigIncludeDepth {
		runlog.Warn("ssh config: skip include %s (depth > %d)", abs, sshConfigIncludeDepth)
		return "", nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("cannot read ssh config %s: %w", abs, err)
	}
	if strings.Contains(string(data), "PRIVATE KEY") {
		runlog.Warn("ssh config: skip %s (looks like a private key)", abs)
		return "", nil
	}
	seen[abs] = struct{}{}

	var b strings.Builder
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		line = strings.TrimRight(line, "\r")
		keyword, args, ok := parseSSHLine(line)
		if ok {
			if strings.EqualFold(keyword, "identityagent") {
				if line, keep := rewriteIdentityAgent(line, args); keep {
					b.WriteString(line)
					b.WriteByte('\n')
				}
				continue
			}
			if _, drop := dropSSHKeywords[strings.ToLower(keyword)]; drop {
				continue
			}
			if strings.EqualFold(keyword, "include") {
				for _, arg := range args {
					included, err := renderInclude(arg, sshDir, depth+1, seen)
					if err != nil {
						return "", err
					}
					b.WriteString(included)
				}
				continue
			}
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String(), nil
}

// rewriteIdentityAgent は IdentityAgent none を残し、それ以外を転送ソケットへ
// 書き換える。値の無い行は捨てる。
func rewriteIdentityAgent(line string, args []string) (string, bool) {
	if len(args) == 0 {
		return "", false
	}
	if strings.EqualFold(strings.Trim(args[0], `"`), "none") {
		return line, true
	}
	indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	return indent + "IdentityAgent " + sandboxSSHAgentSock, true
}

func renderInclude(pattern, sshDir string, depth int, seen map[string]struct{}) (string, error) {
	paths, err := resolveInclude(pattern, sshDir)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, p := range paths {
		info, err := os.Stat(p)
		if os.IsNotExist(err) {
			runlog.Warn("ssh config: skip missing include %s", p)
			continue
		}
		if err != nil {
			return "", fmt.Errorf("cannot stat include %s: %w", p, err)
		}
		if info.IsDir() {
			runlog.Warn("ssh config: skip directory include %s", p)
			continue
		}
		body, err := renderSSHFile(p, sshDir, depth, seen)
		if err != nil {
			return "", err
		}
		b.WriteString(body)
		if body != "" && !strings.HasSuffix(body, "\n") {
			b.WriteByte('\n')
		}
	}
	return b.String(), nil
}

// resolveInclude は Include のパスをホスト上のファイル列にする。
// 相対パスはユーザー設定と同じく ~/.ssh 基準。グロブに一致が無いときは空。
func resolveInclude(pattern, sshDir string) ([]string, error) {
	expanded, err := expandHome(pattern)
	if err != nil {
		return nil, fmt.Errorf("ssh config include %s: %w", pattern, err)
	}
	if !filepath.IsAbs(expanded) {
		expanded = filepath.Join(sshDir, expanded)
	}
	if strings.ContainsAny(expanded, "*?[") {
		matches, err := filepath.Glob(expanded)
		if err != nil {
			return nil, fmt.Errorf("ssh config include %s: %w", pattern, err)
		}
		return matches, nil
	}
	return []string{expanded}, nil
}

func expandHome(p string) (string, error) {
	if p == "" || p[0] != '~' {
		return p, nil
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if p == "~" {
			return home, nil
		}
		return filepath.Join(home, p[2:]), nil
	}
	rest := p[1:]
	name := rest
	suffix := ""
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		name = rest[:i]
		suffix = rest[i:]
	}
	u, err := user.Lookup(name)
	if err != nil {
		return "", err
	}
	return u.HomeDir + suffix, nil
}

func parseSSHLine(line string) (keyword string, args []string, ok bool) {
	parts := splitSSHArgs(strings.TrimSpace(line))
	if len(parts) == 0 || strings.HasPrefix(parts[0], "#") {
		return "", nil, false
	}
	return parts[0], parts[1:], true
}

func splitSSHArgs(s string) []string {
	var args []string
	i := 0
	for i < len(s) {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i >= len(s) {
			break
		}
		if s[i] == '"' {
			i++
			var b strings.Builder
			for i < len(s) && s[i] != '"' {
				if s[i] == '\\' && i+1 < len(s) {
					i++
				}
				b.WriteByte(s[i])
				i++
			}
			if i < len(s) && s[i] == '"' {
				i++
			}
			args = append(args, b.String())
			continue
		}
		j := i
		for j < len(s) && s[j] != ' ' && s[j] != '\t' {
			j++
		}
		args = append(args, s[i:j])
		i = j
	}
	return args
}

func execInSandbox(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sbx", append([]string{"exec", name}, args...)...)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("timed out after %s", probeTimeout)
	}
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail != "" {
			return fmt.Errorf("%w: %s", err, detail)
		}
		return err
	}
	return nil
}
