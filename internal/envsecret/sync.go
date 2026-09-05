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
)

const syncHashFile = "secrets.toml.sha256"
const legacySyncHashDir = "secrets-sync"

// Sync はシークレットを sbx global へ登録し、登録済みのシークレット一覧を返す。
// 取得元は config [secrets]（既定: secrets.toml、1password なら op read）。
// 内容が前回と同じなら set はスキップする。
// 変わっていれば既存の sbx シークレットを全部消してから入れ直す。
// global シークレットは sbx がサンドボックス作成時に取り込むため、
// 必ず sandbox.Create より前に呼ぶこと。
func Sync() ([]Secret, error) {
	secrets, label, raw, err := loadSource()
	if err != nil {
		return nil, err
	}
	sum := sha256Hex(raw)
	prev, err := loadSyncHash()
	if err != nil {
		return nil, err
	}
	if prev == sum {
		if len(secrets) == 0 {
			runlog.Info("envsecret: %s missing or empty, skipping", label)
			return nil, nil
		}
		runlog.Info("envsecret: secrets unchanged (%s), skip set", label)
		fmt.Fprintf(os.Stderr, "agentsb: secrets unchanged; reusing sbx global secrets\n")
		return secrets, nil
	}

	fmt.Fprintf(os.Stderr, "agentsb: secrets changed (%s); replacing sbx secrets\n", label)
	if _, _, err := removeAllSecrets(); err != nil {
		return nil, fmt.Errorf("clear existing secrets: %w", err)
	}
	if len(secrets) == 0 {
		if err := saveSyncHash(sum); err != nil {
			return nil, err
		}
		runlog.Info("envsecret: %s empty after replace", label)
		return nil, nil
	}
	fmt.Fprintf(os.Stderr, "agentsb: syncing %d secret(s) to sbx global from %s\n", len(secrets), label)
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
	return secrets, nil
}

// Hosts はカスタムシークレットの対象ドメイン（重複除去済み）を返す。
// プロキシは対象ホストへの通信が allow されていないと置換できないため、
// サンドボックス作成後に sandbox.AllowNetwork へ渡す。
func Hosts(secrets []Secret) []string {
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
	return hosts
}

func setBuiltin(service, value string) error {
	return runSbxInput([]string{"secret", "set", service, "-f"}, value)
}

func setCustom(s Secret) error {
	args := []string{"secret", "set-custom", "--env", s.Name, "--placeholder", placeholderFor(s.Name)}
	for _, h := range s.Domains {
		args = append(args, "--host", h)
	}
	return runSbxInput(args, s.Value)
}

// runSbxInput は値を stdin で渡す（引数やログに実値を出さない）。
func runSbxInput(args []string, value string) error {
	runlog.Info("sbx %s", strings.Join(args, " "))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sbx", args...)
	cmd.Stdin = strings.NewReader(value + "\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.ReplaceAll(strings.TrimSpace(string(out)), value, "***")
		return fmt.Errorf("%w: %s", err, detail)
	}
	return nil
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

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
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
