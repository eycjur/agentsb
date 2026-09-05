package home

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"agentsb/internal/config"
	"agentsb/internal/runlog"
	"agentsb/internal/sandbox"
)

// credentialsRel は OAuth トークンを保持する .credentials.json の home からの
// 相対パス。ExtractCredentials の書き戻し判定はこのファイルで行う。
var credentialsRel = filepath.Join(".claude", ".credentials.json")

// basePath は ~/.agentsb/home（認証情報を永続化するディレクトリ）を返す。
func basePath() (string, error) {
	root, err := config.Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "home"), nil
}

// CredentialFile は認証情報ファイル 1 つ分の、ホスト側パスとコンテナ側の
// 絶対パスの組。
type CredentialFile struct {
	HostPath      string
	ContainerPath string
}

// credentialRelPaths は Claude 認証の同期対象（home からの相対パス）。
// .credentials.json はセッション中にリフレッシュされる OAuth トークン、
// .claude.json はオンボーディング状態や設定で、両者はひと組として扱い、
// .credentials.json の expiresAt がホスト側より大きい（＝新しい）ときだけ
// まとめて書き戻す。Codex の auth.json はここには含めず、ホストの
// ~/.codex/auth.json を InjectCodexAuth で一方通行コピーする。
var credentialRelPaths = []string{credentialsRel, ".claude.json"}

// EnsureCredentialFiles はコピー先ディレクトリの存在を保証し、サンドボックス
// とのコピーに使う情報を返す。ホスト側ファイル自体は無ければ作らない — 存在
// しないなら InjectCredentials 側でコピーをスキップする（空ファイルで上書き
// しないため）。マウントではなく `sbx cp` を使うのは、サンドボックス内の他の
// 状態（イメージに焼き込んだものなど）をマウントで隠さないため。
func EnsureCredentialFiles() ([]CredentialFile, error) {
	base, err := basePath()
	if err != nil {
		return nil, err
	}
	files := make([]CredentialFile, len(credentialRelPaths))
	for i, rel := range credentialRelPaths {
		p := filepath.Join(base, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			return nil, fmt.Errorf("cannot prepare %s: %w", p, err)
		}
		files[i] = CredentialFile{
			HostPath:      p,
			ContainerPath: filepath.Join(sandbox.HomeDir, rel),
		}
	}
	return files, nil
}

// InjectCodexAuth はホストの ~/.codex/auth.json をサンドボックスへコピーする。
// 書き戻しはしない（ホスト側が正）。ファイルが無ければ何もしない。
func InjectCodexAuth(runName string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}
	hostPath := filepath.Join(homeDir, ".codex", "auth.json")
	if _, err := os.Stat(hostPath); os.IsNotExist(err) {
		runlog.Notice("codex auth: skip %s (host file not found)", hostPath)
		return nil
	} else if err != nil {
		return fmt.Errorf("cannot stat %s: %w", hostPath, err)
	}
	containerPath := filepath.Join(sandbox.HomeDir, ".codex", "auth.json")
	if err := sandbox.CopyToSandbox(runName, hostPath, containerPath); err != nil {
		return fmt.Errorf("cannot inject %s: %w", containerPath, err)
	}
	if err := sandbox.ChownAgent(runName, containerPath); err != nil {
		return fmt.Errorf("cannot fix ownership of %s: %w", containerPath, err)
	}
	runlog.Notice("codex auth: copied %s -> sandbox:%s", hostPath, containerPath)
	return nil
}

// InjectCredentials はサンドボックス作成直後に認証情報ファイルをコピーする。
// ホスト側にファイルが無ければ（未オンボーディングなど）そのファイルはスキッ
// プする — 空ファイルでサンドボックス側の状態を上書きしないため。コピー結果の
// 所有者は `sbx cp` の実装に依存するため、agent が確実に読み書きできるよう
// chown で agent に揃える。
func InjectCredentials(runName string, files []CredentialFile) error {
	for _, f := range files {
		_, err := os.Stat(f.HostPath)
		if os.IsNotExist(err) {
			runlog.Notice("claude credentials: skip %s (host file not found)", f.HostPath)
			continue
		} else if err != nil {
			return fmt.Errorf("cannot stat %s: %w", f.HostPath, err)
		}
		if err := sandbox.CopyToSandbox(runName, f.HostPath, f.ContainerPath); err != nil {
			return fmt.Errorf("cannot inject %s: %w", f.ContainerPath, err)
		}
		if err := sandbox.ChownAgent(runName, f.ContainerPath); err != nil {
			return fmt.Errorf("cannot fix ownership of %s: %w", f.ContainerPath, err)
		}
		runlog.Notice("claude credentials: copied %s -> sandbox:%s", f.HostPath, f.ContainerPath)
	}
	return nil
}

// ExtractCredentials はセッション終了後、コンテナ内の認証情報ファイルをホストへ
// 書き戻す。全ファイルをいったん一時ファイルに取り出し、.credentials.json の
// claudeAiOauth.expiresAt がホスト側より大きいときだけ、まとめてアトミックな
// rename でホストへ反映する。ひと組で判定するのは、並行セッションのうち古い
// トークンを持つ方が後から終了しても、リフレッシュ済みのトークンとそれに対応
// する .claude.json を巻き戻さないため。
func ExtractCredentials(runName string, files []CredentialFile) error {
	tmps := make(map[string]string, len(files)) // HostPath -> 一時ファイル
	defer func() {
		for _, tmp := range tmps {
			os.Remove(tmp)
		}
	}()
	for _, f := range files {
		exists, err := sandbox.PathExists(runName, f.ContainerPath)
		if err != nil {
			return fmt.Errorf("cannot check %s: %w", f.ContainerPath, err)
		}
		if !exists {
			runlog.Notice("claude credentials: skip sandbox:%s (not found in sandbox)", f.ContainerPath)
			continue
		}
		tmp, err := extractToTemp(runName, f)
		if err != nil {
			return err
		}
		tmps[f.HostPath] = tmp
	}

	tokenFile, ok := findCredentialsFile(files)
	if !ok {
		return fmt.Errorf("credential files do not include %s", credentialsRel)
	}
	tokenTmp, ok := tmps[tokenFile.HostPath]
	if !ok {
		runlog.Notice("claude credentials: skip write-back (no %s in sandbox)", credentialsRel)
		return nil
	}
	newer, reason, err := tokenIsNewerThanHost(tokenTmp, tokenFile.HostPath)
	if err != nil {
		return err
	}
	if !newer {
		runlog.Notice("claude credentials: skip write-back (%s)", reason)
		return nil
	}

	for _, f := range files {
		tmp, ok := tmps[f.HostPath]
		if !ok {
			continue
		}
		if err := os.Rename(tmp, f.HostPath); err != nil {
			return err
		}
		delete(tmps, f.HostPath)
		runlog.Notice("claude credentials: copied sandbox:%s -> %s", f.ContainerPath, f.HostPath)
	}
	return nil
}

// extractToTemp はコンテナ側ファイルをホスト側と同じディレクトリの一時ファイル
// に取り出し、そのパスを返す。同じディレクトリに置くのは rename をアトミックに
// するため。
func extractToTemp(runName string, f CredentialFile) (string, error) {
	tmpFile, err := os.CreateTemp(filepath.Dir(f.HostPath), ".agentsb-tmp-*")
	if err != nil {
		return "", err
	}
	tmp := tmpFile.Name()
	tmpFile.Close()
	if err := sandbox.CopyFromSandbox(runName, f.ContainerPath, tmp); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("cannot extract %s: %w", f.ContainerPath, err)
	}
	if err := os.Chmod(tmp, 0600); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return tmp, nil
}

// findCredentialsFile は files から .credentials.json に対応する要素を返す。
func findCredentialsFile(files []CredentialFile) (CredentialFile, bool) {
	target := filepath.Join(sandbox.HomeDir, credentialsRel)
	for _, f := range files {
		if f.ContainerPath == target {
			return f, true
		}
	}
	return CredentialFile{}, false
}

// tokenIsNewerThanHost はコンテナから取り出した .credentials.json（containerPath）
// の claudeAiOauth.expiresAt がホスト側より大きいかを返す。書き戻さない場合は
// その理由も返す。
//   - コンテナ側から expiresAt が読めない（ログアウト状態など）→ 書き戻さない。
//     ホスト側の有効なトークンを壊さないため。
//   - ホスト側ファイルが無い、または expiresAt が読めない → 書き戻す。初回
//     ログインや API キー運用からの切り替えを拾うため。
//   - 同値なら変更なしとして書き戻さない。
func tokenIsNewerThanHost(containerPath, hostPath string) (bool, string, error) {
	containerData, err := os.ReadFile(containerPath)
	if err != nil {
		return false, "", err
	}
	hostData, err := os.ReadFile(hostPath)
	if err != nil && !os.IsNotExist(err) {
		return false, "", fmt.Errorf("cannot read %s: %w", hostPath, err)
	}
	newer, reason := tokenIsNewer(containerData, hostData)
	return newer, reason, nil
}

// tokenIsNewer は tokenIsNewerThanHost の純粋な比較部分。hostData が nil なら
// ホスト側ファイル無しとして扱う。
func tokenIsNewer(containerData, hostData []byte) (bool, string) {
	containerExp, ok := oauthExpiresAt(containerData)
	if !ok {
		return false, "sandbox copy has no claudeAiOauth.expiresAt"
	}
	if hostData == nil {
		return true, ""
	}
	hostExp, ok := oauthExpiresAt(hostData)
	if !ok {
		return true, ""
	}
	if containerExp > hostExp {
		return true, ""
	}
	return false, fmt.Sprintf("sandbox expiresAt %d is not newer than host %d", containerExp, hostExp)
}

// oauthExpiresAt は .credentials.json の claudeAiOauth.expiresAt（UNIX ミリ秒）
// を返す。JSON として読めない、またはキーが無い場合は ok=false。
func oauthExpiresAt(data []byte) (int64, bool) {
	var v struct {
		ClaudeAiOauth struct {
			ExpiresAt *int64 `json:"expiresAt"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(data, &v); err != nil || v.ClaudeAiOauth.ExpiresAt == nil {
		return 0, false
	}
	return *v.ClaudeAiOauth.ExpiresAt, true
}
