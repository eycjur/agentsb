// Package dotfiles は、コンテナ起動時に dotfiles リポジトリを clone/更新して
// インストールスクリプトを実行してから、本来のコマンドへ exec で引き継ぐ
// 起動コマンドを組み立てる。イメージビルド時ではなく起動時に行うのは、
// /home/agent が run ごとにマウントで差し替えられ、ビルド時に home へ
// 入れたものは隠れてしまうため。
package dotfiles

// Command は dotfiles のセットアップ後に command を exec する bash コマンド
// ラインを返す。設定値はスクリプト文字列に埋め込まず bash の位置引数
// （$1〜$3）として渡す。bash がコードとして解釈するのはスクリプト本文だけ
// なので、値に何が入っていてもエスケープ不要。~ の展開のみスクリプト内で行う。
func Command(repo, targetPath, installCmd string, command []string) []string {
	if targetPath == "" {
		targetPath = "~/dotfiles"
	}
	return append([]string{"bash", "-c", script, "bash", repo, targetPath, installCmd}, command...)
}

// script は $1=repository $2=target_path $3=install_command、$4 以降を
// 本来のコマンドとして受け取る。各段階を stderr に出し、検証しやすくする。
// clone/install の失敗は警告のみで、コマンドはそのまま起動する。
const script = `
repo=$1; target=$2; install=$3; shift 3

log() { echo "agentsb: dotfiles: $*" >&2; }

# 先頭の ~ を $HOME に展開する（クォート内の ~ はシェルが展開しないため）
case "$target" in
  "~")   dir=$HOME ;;
  "~/"*) dir=$HOME${target#"~"} ;;
  *)     dir=$target ;;
esac

log "repository=$repo"
log "target=$dir"
log "install_command=$install"

if [ -d "$dir/.git" ]; then
  log "pulling existing clone"
  if git -C "$dir" pull --ff-only --quiet; then
    log "pull ok"
  else
    log "pull failed (offline?), using cached version"
  fi
else
  log "cloning into $dir"
  if git clone --depth=1 --quiet "$repo" "$dir"; then
    log "clone ok"
  else
    log "clone failed, skipping install"
    log "exec: $*"
    exec "$@"
  fi
fi

if [ -z "$install" ]; then
  log "install_command empty, skipping install"
elif [ -f "$dir/$install" ]; then
  log "running: bash $install"
  if (cd "$dir" && bash "$install"); then
    log "install ok"
  else
    log "install failed, continuing"
  fi
else
  log "install script not found: $dir/$install"
fi

log "exec: $*"
exec "$@"
`
