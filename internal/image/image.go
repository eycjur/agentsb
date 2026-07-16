package image

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"agentsb/internal/config"
	"agentsb/internal/container"
	"agentsb/internal/runlog"
)

const imageBase = "agentsb-base"

// containerfile はバイナリに埋め込まれたサンドボックスのイメージ定義。
// これが唯一の正で、ユーザーが編集する想定はない。変更はリポジトリの
// internal/image/Containerfile を編集して agentsb を入れ直す。
//
// claude を npm で /usr/local/bin に入れているのは、run ごとの分離のために
// /home/agent をマウントで差し替えてもバイナリが隠れないようにするため。
//
//go:embed Containerfile
var containerfile string

// Tag は埋め込み Containerfile と UID/GID に対応するイメージタグを返す。
// タグには定義内容のハッシュが含まれるため、既存コンテナのイメージ参照と
// 比較すれば定義の変更を検知できる。
func Tag(uid, gid int) string {
	sum := sha256.Sum256([]byte(containerfile))
	return fmt.Sprintf("%s:%d-%d-%x", imageBase, uid, gid, sum[:6])
}

// EnsureBuilt は埋め込み Containerfile に対応するイメージタグを返し、
// 存在しなければビルドする。タグには定義内容のハッシュを埋め込んであるため、
// agentsb の更新で Containerfile が変わると自動的にリビルドされる。
// force=true なら無条件でリビルドする（ハッシュでは検知できない上流ベース
// イメージの更新を取り込む）。
func EnsureBuilt(uid, gid int, force bool) (string, error) {
	tag := Tag(uid, gid)

	if !force && container.ImageExists(tag) {
		runlog.Info("image %s already present", tag)
		return tag, nil
	}
	cf, err := writeBuildContext()
	if err != nil {
		return "", err
	}
	runlog.Info("building image %s force=%v containerfile=%s", tag, force, cf)
	fmt.Fprintf(os.Stderr, "agentsb: building %s (this may take a few minutes)…\n", tag)
	if err := container.BuildImage(cf, filepath.Dir(cf), tag, uid, gid); err != nil {
		return "", fmt.Errorf("image build failed: %w", err)
	}
	// prune は明示的な `agentsb build` のときだけ行う。自動ビルド後
	// （例: `agentsb run` 経由）に prune すると、並行する run が解決済みで
	// まだコンテナを作成していないイメージを消してしまう恐れがある —
	// InUseImages に見えるのは、すでに存在するコンテナが使っているイメージだけ。
	if force {
		pruneSuperseded(tag)
	}
	return tag, nil
}

// writeBuildContext は埋め込み Containerfile を ~/.agentsb/build/Containerfile
// へ書き出し、そのパスを返す。専用ディレクトリを使うのは、home/ と home-runs/
// （認証情報）を決してビルドコンテキストに入れないため。
func writeBuildContext() (string, error) {
	root, err := config.Root()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "build")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "Containerfile")
	if err := os.WriteFile(path, []byte(containerfile), 0644); err != nil {
		return "", fmt.Errorf("cannot write %s: %w", path, err)
	}
	return path, nil
}

// pruneSuperseded は current 以外の agentsb-base イメージのうち、
// 存在するコンテナ（停止中を含む）が使っていないものを削除する。
func pruneSuperseded(current string) {
	inUse, err := container.InUseImages()
	if err != nil {
		// 使用中イメージの一覧が取れないと、どれを消して安全か判断できない
		// — run を壊すリスクを取るより prune 自体をスキップする。
		fmt.Fprintf(os.Stderr, "agentsb: warning: could not list running containers, skipping prune: %v\n", err)
		return
	}
	inUseSet := make(map[string]bool, len(inUse))
	for _, img := range inUse {
		inUseSet[img] = true
	}
	tags, err := container.ListImages(imageBase)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agentsb: warning: could not list images to prune: %v\n", err)
		return
	}
	for _, tag := range tags {
		if tag == current || inUseSet[tag] {
			continue
		}
		if err := container.DeleteImage(tag); err != nil {
			fmt.Fprintf(os.Stderr, "agentsb: warning: could not prune %s: %v\n", tag, err)
		} else {
			fmt.Fprintf(os.Stderr, "agentsb: pruned superseded image %s\n", tag)
		}
	}
}
