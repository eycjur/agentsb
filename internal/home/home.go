package home

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"agentsb/internal/config"
)

// basePath は ~/.agentsb/home（永続化されるベース home ディレクトリ）を返す。
// run ごとにここから fork され、認証情報はここへ書き戻される。
func basePath() (string, error) {
	root, err := config.Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "home"), nil
}

// runsPath は run ごとの fork された home を置く ~/.agentsb/home-runs を返す。
func runsPath() (string, error) {
	root, err := config.Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "home-runs"), nil
}

// Path は runName に対応するサンドボックス用 home のパスを返す。
func Path(runName string) (string, error) {
	runs, err := runsPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(runs, runName), nil
}

// Ensure はサンドボックス用の home を返し、無ければベース home から fork して
// 作成する。コピーはコンテナ内の /home/agent にマウントされ、各サンドボックスは
// 同じ認証状態から始まりつつ、互いに独立した home を持つ。`rm` で削除されるまで
// 同じ home を使い続ける。
func Ensure(runName string) (string, error) {
	base, err := basePath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(base, 0755); err != nil {
		return "", fmt.Errorf("cannot create base home %s: %w", base, err)
	}
	runHome, err := Path(runName)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(runHome); err == nil {
		return runHome, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(runHome), 0755); err != nil {
		return "", err
	}
	// コピーは cp に委譲する: symlink・パーミッション・特殊ファイルの扱いを
	// 自前で実装しない。-R は symlink をリンクのまま複製する。
	if out, err := exec.Command("cp", "-Rp", base, runHome).CombinedOutput(); err != nil {
		return "", fmt.Errorf("cannot copy home: %v: %s", err, out)
	}
	return runHome, nil
}

// credentialFiles はセッション終了後にベース home へ書き戻すパス（home からの相対パス）。
// .claude/.credentials.json はセッション中にリフレッシュされる OAuth トークン、
// .claude.json はオンボーディングや設定の状態 — これが無いと毎回初期セットアップを
// 聞かれてしまう。それ以外のデータは各サンドボックスの home に留まり、`rm` で破棄される。
var credentialFiles = []string{
	filepath.Join(".claude", ".credentials.json"),
	".claude.json",
}

// SyncCredentials は credentialFiles をサンドボックス用 home からベース home へ
// コピーする。各コピーはユニークな一時ファイル + アトミックな rename を経由する
// ため、並行するセッションが同時に終了しても書きかけのファイルは生じない —
// OAuth トークンは後勝ち（latest-wins)で問題ない。
func SyncCredentials(runHome string) error {
	base, err := basePath()
	if err != nil {
		return err
	}
	var firstErr error
	for _, rel := range credentialFiles {
		src := filepath.Join(runHome, rel)
		if _, err := os.Stat(src); err != nil {
			if !os.IsNotExist(err) && firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := copyFileAtomic(src, filepath.Join(base, rel)); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Cleanup はサンドボックスの削除（rm）時に home ディレクトリを削除する。
func Cleanup(runHome string) {
	if err := os.RemoveAll(runHome); err != nil {
		fmt.Fprintf(os.Stderr, "agentsb: warning: could not clean up %s: %v\n", runHome, err)
	}
}

// copyFileAtomic は src をユニークな一時ファイルへコピーし、rename で dst に
// 置き換える。並行する run が同時に書き戻しても、dst に書きかけの内容が
// 見えることはない。認証情報用なのでパーミッションは 0600 固定。
func copyFileAtomic(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.CreateTemp(filepath.Dir(dst), ".agentsb-tmp-*")
	if err != nil {
		return err
	}
	tmp := out.Name()
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Chmod(0600); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}
