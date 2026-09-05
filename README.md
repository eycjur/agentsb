# agentsb

Claude Code や Codex などを、ディレクトリ単位の microVM サンドボックスで動かす CLI です。
実行環境は [Docker Sandboxes](https://docs.docker.com/ai/sandboxes/)（`sbx`）です。

ディレクトリごとに 1 つのサンドボックスを持ちます。Claude の認証情報（`~/.claude/.credentials.json`、`~/.claude.json`）は、サンドボックス作成時にホストの `~/.agentsb/home` からコピーし（`sbx cp`）、セッション終了時に書き戻します。書き戻しは 2 ファイルをひと組として扱い、`.credentials.json`（OAuth トークン）の `claudeAiOauth.expiresAt` がホスト側より大きい場合だけ、`.claude.json`（オンボーディング状態や設定）と合わせて上書きします（並行セッションの古いトークンでリフレッシュ済みのものを巻き戻さないため）。Codex の `~/.codex/auth.json` はホスト側を正とし、`agentsb run` のたびにコンテナへコピーするだけで、書き戻しはしません。サンドボックスは `agentsb rm` で削除するまで維持されます。

## 前提

- [Docker Sandboxes](https://docs.docker.com/ai/sandboxes/get-started/)（`sbx` CLI、0.39 以降。`secret` サブコマンドの引数形式に依存）: `brew install docker/tap/sbx`
- `docker` CLI（テンプレートイメージのビルド時のみ使用）
- ビルドに Go 1.22 以降

## インストール

### バイナリ

[Releases](https://github.com/eycjur/agentsb/releases) から、環境に合うバイナリ（darwin / linux × amd64 / arm64）をダウンロードして `PATH` の通った場所へ置いてください。

### 開発時

```bash
sudo make install
```

`PREFIX`（既定: `/usr/local/bin`）へ直接 `go build -o` します。再実行で上書き更新されます。

## 使い方

```bash
agentsb run   # サンドボックスの zsh（login shell）に入る
```

`agentsb run` は状態を意識せずに使えます: テンプレート(Docker image相当)が無ければビルド、サンドボックス(Docker Container相当)が無ければ作成して、セッション（zsh）に入ります。作成済みなら新しいセッションを開くだけなので、同じディレクトリで複数の端末から同時に入れます。

実行したディレクトリはサンドボックス内の同じパスにマウントされ、そこが作業ディレクトリになります。エージェントはその中から起動してください。

```bash
# サンドボックス内
claude --dangerously-skip-permissions
codex --dangerously-bypass-approvals-and-sandbox
```

CLI ツール（`claude` / `codex` / `hunkdiff`）は `/usr/local/share/npm-global` に入ります。サンドボックス内での更新は `npm install -g @openai/codex` のように **sudo なし**で行ってください（`sudo npm` は別の prefix を触ります）。テンプレートへ恒久反映する場合は Containerfile を編集して `sudo make install` → `agentsb build` → 対象ディレクトリで `agentsb rm` → `agentsb run` です。

| コマンド | 説明 |
|----------|------|
| `agentsb ls` | サンドボックスの一覧（停止中も含む。NAME は完全名から `agentsb-` を除いたもの、SBX NAME は sbx 上の完全名） |
| `agentsb build` | テンプレートを強制リビルドして sbx へロードし直す（ベースイメージやツールの更新を取り込む。古いテンプレートは prune） |
| `agentsb run` | サンドボックスに入る（必要に応じてテンプレートのビルド → サンドボックスの作成を自動で行う） |
| `agentsb stop [name]` | サンドボックスを停止（状態は保持され、次の `run` で再開。名前省略時はカレントディレクトリのもの） |
| `agentsb rm [name]` | サンドボックスを削除（名前省略時はカレントディレクトリのもの。認証情報は他サンドボックスとも共有しているため削除しない） |
| `agentsb open [port]` | サンドボックスのポートをホストへ公開し（`sbx ports --publish`）、ブラウザで `http://localhost:<port>/` を開く（ポート省略時は 8000） |
| `agentsb secret clear` | sbx に登録済みのシークレットをすべて削除する（同期ハッシュもクリア。次回 `run` で再登録） |

`agentsb build` はテンプレートだけを対象にした操作で、既存サンドボックスの状態には影響しません。`agentsb prune` は管理下の全サンドボックスを状態に関わらず削除し、テンプレートと認証情報（sbx secrets 含む）も含めて全消去します。

`[name]` を取るコマンド（`stop` / `rm` / `open`）では `agentsb-` プレフィックスを省略できます（例: `agentsb stop myapp` は `agentsb stop agentsb-myapp` と同じ）。

## 設定

任意。無ければデフォルトで動作します（`$XDG_CONFIG_HOME` があればそちら優先）。

| パス | 役割 |
|------|------|
| `~/.config/agentsb/config.toml` | グローバル設定（dotfiles・SSH agent・シークレット取得元など） |
| `~/.config/agentsb/secrets.toml` | プロキシ注入するシークレット（`[[secret]]`。1Password 利用時は不要） |

`config.toml` の例:

```toml
[dotfiles]
repository      = "https://github.com/yourname/dotfiles.git"
target_path     = "~/dotfiles"
install_command = "install.sh"

# ホスト SSH agent をサンドボックスで使う（git push 等）
# OpenSSH の IdentityAgent と同じソケット（1Password など）
[ssh]
identity_agent = "~/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock"

# 省略時は ~/.config/agentsb/secrets.toml
# [secrets]
# source = "1password"
# ref    = "op://Personal/agentsb-secrets/notesPlain"
```

`[dotfiles]` を設定すると、リポジトリを clone し、`target_path` 内で `bash <install_command>` を実行してからシェルを起動します。実行されるのは `target_path` に dotfiles がまだ無いときだけ（実質サンドボックスの新規作成時）で、`agentsb stop` からの復帰でも稼働中のサンドボックスへの再入室でも何もしません。dotfiles の更新を反映したいときは、サンドボックス内で手動 pull するか、`agentsb rm` してから再度 `agentsb run` してください。

### SSH agent 転送

`[ssh].identity_agent` を設定すると、`agentsb run` 時に次を行います。

1. そのパスを `SSH_AUTH_SOCK` に載せる（`~/.ssh/config` の `IdentityAgent` と同じ発想）
2. ホストでソケットが使え、agent に鍵があることを `ssh-add -l` で確認（無ければエラー）
3. GitHub SSH 向けに解決した `IP:22` をサンドボックスの network policy へ allow
4. サンドボックス内で `ssh-add -l` をプローブ（失敗時は警告のみ。セッションは続行）

未設定なら SSH 関連の処理はしません。転送そのものは [Docker Sandboxes](https://docs.docker.com/ai/sandboxes/security/credentials/#ssh-agent) が行います。秘密鍵はサンドボックスへコピーしません。

なお、本設定と同時に、ホスト側でも `SSH_AUTH_SOCK` の設定が必要です。

```bash:~/.zshrc
export SSH_AUTH_SOCK="$HOME/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock"
```

### シークレット（プロキシ注入）

`agentsb run` はサンドボックスを作る前にシークレットを sbx の **global** スコープへ登録し、プロキシ注入します。実値は stdin で sbx に渡し、コマンド引数やログには出しません。コンテナには代替値だけを渡します。内容が前回と同じなら登録をスキップし（`~/.agentsb/secrets.toml.sha256`）、変わっていれば既存の sbx シークレットを全部消してから入れ直します。

sbx のプロキシは global シークレットをサンドボックス作成時に取り込むため、設定の追加・変更・削除を既存のサンドボックスに反映するには `agentsb rm` → `agentsb run` で作り直す必要があります（sbx の仕様。作り直すと apt install などサンドボックス内の変更は消えます）。

既定は `~/.config/agentsb/secrets.toml`。`config.toml` で指定すると 1Password（Secure Note）から読み込むこともできます。

シークレット本体（ローカルファイル / 1Password 共通）の形:

```toml
[[secret]]
name = "OPENAI_API_KEY"
value = "sk-..."

[[secret]]
name = "DEEPL_API_KEY"
value = "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx:fx"
domains = ["api.deepl.com", "api-free.deepl.com"]
```

[組み込みサービス](https://docs.docker.com/ai/sandboxes/security/credentials/#built-in-services)（OpenAI 等）は `domains` 不要で `secret set`、それ以外は `domains` 付きで `set-custom` します。コンテナ内が `proxy-managed` / `sbx-cs-…` のままなのは正常です。プロジェクトの `.env` には関与しません。

## 内部仕様

### テンプレートのビルドとロード

sbx はホストの Docker のイメージストアを共有しないため、テンプレートは次の 3 段階でローカル完結でロードします（レジストリへの push は不要）。

1. `docker build` — 埋め込み Containerfile（`docker/sandbox-templates:shell` ベース）からイメージをビルド
2. `docker image save` — tar へ書き出し
3. `sbx template load` — サンドボックスランタイムへロード

テンプレートタグには Containerfile のハッシュが含まれ、これが自動リビルド判定に使われます。

### ディレクトリ構成

agentsb が管理するデータ（初回の `agentsb run` で自動生成。手動編集は不要）:

| パス | 役割 |
|------|------|
| `~/.agentsb/build/` | テンプレートビルド用の作業ディレクトリ。ビルド時に Containerfile と tar がここへ書き出される |
| `~/.agentsb/home/` | Claude 認証情報（`.claude/.credentials.json`、`.claude.json`）を永続化し、サンドボックス作成時・セッション終了時に `sbx cp` でやり取りする（Codex の auth はホストの `~/.codex/auth.json` から都度コピー） |
| `~/.agentsb/logs/agentsb.log` | 動作検証用ログ（設定の有無、sbx CLI 呼び出し、dotfiles の有効/無効など） |
| `~/.agentsb/secrets.toml.sha256` | シークレット同期スキップ判定用のハッシュ |

ログは常に追記され、2 MiB 超で `agentsb.log.1` へローテートします。ターミナルにも同じ行を出したいときは `-v` / `--verbose` を付けてください。dotfiles の clone/install の途中経過はサンドボックス内の stderr（セッション画面）にも出ます。

Claude は初回だけサンドボックス内でログインしてください（セッション終了時に `~/.agentsb/home` へ書き戻されます）。Codex はホストでログイン済みの `~/.codex/auth.json` を使います。

### herdr 連携

[herdr](https://herdr.dev/) の pane 内で実行すると、pane の表示名（例: `claude (agentsb)`）を自動で herdr に報告します。

エージェントの状態（working/blocked/idle）と完了の検出は herdr 自身に任せます。herdr はホストのプロセスツリーからエージェントを識別して画面内容から状態を検出するため、agentsb はセッション（`sbx exec`）プロセスの argv[0] をエージェント名に書き換えて、サンドボックス内のエージェントをホスト側から識別できるようにしています。agentsb は Claude Code 前提で常に `claude` を設定するため、Codex CLI を使った場合は herdr 側の状態表示が不正確になります（対応は別途検討）。herdr 外での実行には影響しません。

## Acknowledgements

The integration with herdr was inspired by [pall8t](https://github.com/TakiTake/pall8t).
