package envsecret

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"agentsb/internal/config"
	"agentsb/internal/runlog"
	"agentsb/internal/sandbox"
)

const syncHashFile = "secrets.toml.sha256"

// Sync は secrets.toml を sbx global へ登録する。
// 内容が前回と同じなら set をスキップする（network allow のみ）。
// 戻り値はカスタムシークレットの KEY=placeholder（sbx exec -e 用）。
func Sync(sandboxName string) ([]string, error) {
	secrets, err := Load()
	if err != nil {
		return nil, err
	}
	if len(secrets) == 0 {
		path, _ := Path()
		runlog.Info("envsecret: %s missing or empty, skipping", path)
		return nil, nil
	}

	path, err := Path()
	if err != nil {
		return nil, err
	}
	env := execEnv(secrets)

	var hosts []string
	seen := map[string]struct{}{}
	for _, s := range secrets {
		if _, ok := builtinByEnv[s.Name]; ok {
			continue
		}
		for _, d := range s.Domains {
			if _, ok := seen[d]; ok {
				continue
			}
			seen[d] = struct{}{}
			hosts = append(hosts, d)
		}
	}
	if err := sandbox.AllowNetwork(sandboxName, hosts); err != nil {
		return nil, fmt.Errorf("policy allow: %w", err)
	}

	sum, err := fileSHA256(path)
	if err != nil {
		return nil, err
	}
	prev, err := loadSyncHash()
	if err != nil {
		return nil, err
	}
	if prev == sum {
		runlog.Info("envsecret: secrets.toml unchanged, skip set")
		fmt.Fprintf(os.Stderr, "agentsb: secrets.toml unchanged; reusing sbx global secrets\n")
		return env, nil
	}

	fmt.Fprintf(os.Stderr, "agentsb: syncing %d secret(s) to sbx global from %s\n", len(secrets), path)
	for _, s := range secrets {
		if svc, ok := builtinByEnv[s.Name]; ok {
			if err := setBuiltin(svc, s.Value); err != nil {
				return nil, fmt.Errorf("secret set %s: %w", svc, err)
			}
			continue
		}
		if err := setCustom(s); err != nil {
			return nil, fmt.Errorf("secret set-custom %s: %w", s.Name, err)
		}
	}
	if err := saveSyncHash(sum); err != nil {
		return nil, err
	}
	return env, nil
}

func setBuiltin(service, value string) error {
	return runSbx(
		[]string{"secret", "set", "-g", service, "--token", value, "--force"},
		[]string{"secret", "set", "-g", service, "--token", "***", "--force"},
	)
}

func setCustom(s Secret) error {
	ph := placeholderFor(s.Name)
	args := []string{"secret", "set-custom", "-g", "--env", s.Name, "--value", s.Value, "--placeholder", ph}
	logged := []string{"secret", "set-custom", "-g", "--env", s.Name, "--value", "***", "--placeholder", ph}
	for _, h := range s.Domains {
		args = append(args, "--host", h)
		logged = append(logged, "--host", h)
	}
	return runSbx(args, logged)
}

func runSbx(args, logArgs []string) error {
	runlog.Info("sbx %s", strings.Join(logArgs, " "))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sbx", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err == nil {
		return nil
	}
	detail := strings.TrimSpace(stderr.String())
	if detail == "" {
		detail = strings.TrimSpace(string(out))
	}
	if detail != "" {
		return fmt.Errorf("%w: %s", err, detail)
	}
	return err
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func syncHashPath() (string, error) {
	root, err := config.Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, syncHashFile), nil
}

func loadSyncHash() (string, error) {
	path, err := syncHashPath()
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func saveSyncHash(hash string) error {
	path, err := syncHashPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(hash+"\n"), 0o600)
}
