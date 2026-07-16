package home

import (
	"fmt"
	"os"
	"path/filepath"

	"agentsb/internal/config"
	"agentsb/internal/container"
)

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

// credentialRelPaths は home からの相対パス。.claude/.credentials.json は
// セッション中にリフレッシュされる OAuth トークン。
// .claude.json（オンボーディングや設定の状態）はサンドボックスごとに
// 独立させたいため、あえて同期しない。
var credentialRelPaths = []string{
	filepath.Join(".claude", ".credentials.json"),
}

// EnsureCredentialFiles はコピー先ディレクトリの存在を保証し、コンテナとの
// コピーに使う情報を返す。ホスト側ファイル自体は無ければ作らない — 存在しな
// いなら InjectCredentials 側でコピーをスキップする（空ファイルで上書きしな
// いため）。bind mount ではなく `container cp` を使うのは、コンテナ内の他の
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
		files[i] = CredentialFile{HostPath: p, ContainerPath: filepath.Join(container.HomeDir, rel)}
	}
	return files, nil
}

// InjectCredentials はコンテナ起動直後に認証情報ファイルをコンテナへコピーする。
// ホスト側にファイルが無ければ（未オンボーディングなど）そのファイルはスキッ
// プする — 空ファイルでコンテナ側の状態を上書きしないため。`container cp` は
// 稼働中のコンテナにしか使えないため、呼び出し側は `container start` の後に
// これを呼ぶこと。
func InjectCredentials(runName string, files []CredentialFile) error {
	for _, f := range files {
		if _, err := os.Stat(f.HostPath); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return fmt.Errorf("cannot stat %s: %w", f.HostPath, err)
		}
		if err := container.CopyToContainer(runName, f.HostPath, f.ContainerPath); err != nil {
			return fmt.Errorf("cannot inject %s: %w", f.ContainerPath, err)
		}
	}
	return nil
}

// ExtractCredentials はセッション終了後、コンテナ内の認証情報ファイルをホストへ
// 書き戻す。一時ファイル + アトミックな rename を経由するため、並行するセッ
// ションが同時に終了しても書きかけのファイルは生じない — OAuth トークンは
// 後勝ち（latest-wins）で問題ない。
func ExtractCredentials(runName string, files []CredentialFile) error {
	var firstErr error
	for _, f := range files {
		if err := extractOne(runName, f); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func extractOne(runName string, f CredentialFile) error {
	exists, err := container.Exists(runName, f.ContainerPath)
	if err != nil {
		return fmt.Errorf("cannot check %s: %w", f.ContainerPath, err)
	}
	if !exists {
		return nil
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(f.HostPath), ".agentsb-tmp-*")
	if err != nil {
		return err
	}
	tmp := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmp)

	if err := container.CopyFromContainer(runName, f.ContainerPath, tmp); err != nil {
		return fmt.Errorf("cannot extract %s: %w", f.ContainerPath, err)
	}
	if err := os.Chmod(tmp, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, f.HostPath)
}
