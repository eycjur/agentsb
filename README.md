# agentsb

Claude Code、Codex、Cursor Agent、Gemini CLI を、ディレクトリ単位の microVM サンドボックスで動かす CLI です。
実行環境には [Docker Sandboxes](https://docs.docker.com/ai/sandboxes/)（`sbx`）を使います。

- 作業ディレクトリをサンドボックス内の同じパスにマウント
- ディレクトリごとに環境を保持し、複数の端末から利用可能
- `agentsb rm` で削除するまで、インストールしたツールや設定を保持

## インストール

### 前提

| 必要なもの | 用途 |
|---|---|
| [Docker Sandboxes](https://docs.docker.com/ai/sandboxes/get-started/) 0.39 以降 | サンドボックスの実行。`brew install docker/tap/sbx` で導入 |
| `docker` CLI | テンプレートイメージのビルド |
| Go 1.22 以降 | agentsb をソースからビルドする場合のみ |

sbx は `secret` サブコマンドの引数形式に依存するため、0.39 以降が必要です。

### バイナリから

[Releases](https://github.com/eycjur/agentsb/releases) から環境に合うバイナリ（darwin / linux × amd64 / arm64）をダウンロードし、`PATH` の通った場所へ置いてください。

### ソースから

```bash
sudo make install
```

`PREFIX`（既定: `/usr/local/bin`）へビルドして配置します。再実行すると上書き更新されます。

## 使い方

作業ディレクトリで実行します。テンプレートのビルドとサンドボックスの作成は、必要な場合だけ自動で行われます。

```bash
agentsb run   # サンドボックスの zsh（ログインシェル）に入る
```

サンドボックス内で、使いたいエージェントを起動してください。

```bash
claude --dangerously-skip-permissions
codex --dangerously-bypass-approvals-and-sandbox
agent --yolo   # または cursor-agent --yolo
gemini --yolo
```

### 認証

| ツール | 認証方法 | ホストとの同期 |
|---|---|---|
| Claude Code | 初回はサンドボックス内でログイン | 作成時にコピーし、終了時に新しい認証情報を書き戻す |
| Codex | ホストでログイン | `~/.codex/auth.json` を毎回コピー。書き戻しなし |
| Cursor Agent | サンドボックス内でログイン、または `CURSOR_API_KEY` をプロキシ注入 | コピー・書き戻しなし |
| Gemini CLI | サンドボックス内で認証 | コピー・書き戻しなし |

Cursor Agent はサンドボックス内でファイルストア（`AGENT_CLI_CREDENTIAL_STORE=file`）を使います。
Cursor／Gemini の認証情報はサンドボックス内に残りますが、削除・再作成すると再認証が必要です。
API キーの注入方法は [シークレット](#シークレットプロキシ注入) を参照してください。

Claude の同期対象は `~/.agentsb/home` 配下の `.claude/.credentials.json` と `.claude.json` です。
OAuth トークンの `claudeAiOauth.expiresAt` がホスト側より新しい場合だけ、両方を書き戻します。並行セッションの古いトークンによる上書きを防ぐためです。

### コマンド一覧

| コマンド | 動作 |
|---|---|
| `agentsb run` | サンドボックスに入る。停止中なら再開 |
| `agentsb ls` | 停止中を含むサンドボックスの一覧 |
| `agentsb stop [name]` | 状態を保持して停止 |
| `agentsb rm [name]` | サンドボックスを削除。ホストに保存した認証情報は保持 |
| `agentsb open [port]` | ポートを公開し、ブラウザで `http://localhost:<port>/` を開く。既定は 8000 |
| `agentsb build` | テンプレートを強制リビルドしてロード。古いテンプレートは削除 |
| `agentsb secret clear` | sbx の登録済みシークレットと同期ハッシュを削除。次回 `run` で再登録 |
| `agentsb prune` | 管理下の全サンドボックス・テンプレート・認証情報・sbx secrets を全消去 |

- `stop` / `rm` は名前省略時、カレントディレクトリのサンドボックスが対象です。
- `stop` / `rm` で名前を指定する場合、`agentsb-` は省略できます。
- `ls` の `NAME` は省略名、`SBX NAME` は sbx 上の完全名です。
- `build` だけでは既存サンドボックスは更新されません。

### ツールの更新

| ツール | インストール先 | 更新・再インストール |
|---|---|---|
| Claude Code | `~/.local` 配下 | `curl -fsSL https://claude.ai/install.sh \| bash` |
| Cursor Agent | `~/.local` 配下 | `curl -fsSL https://cursor.com/install \| bash` |
| Codex / Gemini CLI / hunkdiff | `/usr/local/share/npm-global` | `npm install -g <パッケージ名>` |

npm のパッケージ名は `@openai/codex`、`@google/gemini-cli`、`hunkdiff` です。
**npm の更新に sudo は不要です。** `sudo npm` は別のインストール先を使います。

テンプレートへ恒久反映する場合:

1. `internal/image/Containerfile` を編集
2. `sudo make install` で agentsb を更新
3. `agentsb build` でテンプレートを再ビルド
4. 対象ディレクトリで `agentsb rm` → `agentsb run` を実行

再作成すると、サンドボックス内で追加したツールや設定は消えます。マウントした作業ディレクトリは残ります。

## 設定

設定ファイルは任意です。`$XDG_CONFIG_HOME` があれば `~/.config` より優先します。

| パス | 役割 |
|---|---|
| `~/.config/agentsb/config.toml` | dotfiles・SSH agent・シークレット取得元 |
| `~/.config/agentsb/secrets.toml` | プロキシ注入するシークレット。1Password 利用時は不要 |

`config.toml` の例:

```toml
[dotfiles]
repository      = "https://github.com/yourname/dotfiles.git"
target_path     = "~/dotfiles"
install_command = "install.sh"

# ホストの SSH agent を利用（例: 1Password）
[ssh]
identity_agent = "~/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock"
share_config   = true

# シークレットを 1Password から読む場合
# [secrets]
# source = "1password"
# ref    = "op://Personal/agentsb-secrets/notesPlain"
```

### dotfiles

`[dotfiles]` を設定すると、シェル起動前にリポジトリを clone し、`target_path` 内で `bash <install_command>` を実行します。

| 状態 | 動作 |
|---|---|
| `target_path` に dotfiles がない | clone とインストールを実行 |
| dotfiles がある（再入室・停止からの復帰など） | 何もしない |
| 更新を反映したい | サンドボックス内で手動 pull、または削除・再作成 |

### SSH agent 転送

`[ssh].identity_agent` にソケットを指定し、ホスト側の `SSH_AUTH_SOCK` にも同じパスを設定してください。

```bash
# ~/.zshrc（1Password の例）
export SSH_AUTH_SOCK="$HOME/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock"
```

`agentsb run` 時の処理:

1. 指定したパスを `SSH_AUTH_SOCK` に設定
2. ホストで `ssh-add -l` を実行し、ソケットと鍵を確認。使えなければエラー
3. GitHub SSH の接続先 `IP:22` をネットワークポリシーで許可
4. サンドボックス内でも `ssh-add -l` を実行。失敗時は警告して続行

未設定なら agent 転送はしません。転送は [Docker Sandboxes](https://docs.docker.com/ai/sandboxes/security/credentials/#ssh-agent) が行い、秘密鍵はコピーしません。

sbx 0.51 以降の shell は v3 kit で、`ssh-agent` を要求したサンドボックスにだけソケットを渡します。`identity_agent` があるとき、`agentsb` は作成時にその要求を足します。この変更より前に作ったサンドボックスには渡らないので、`agentsb rm` してから `agentsb run` し直してください。

### SSH config

`[ssh].share_config` を `true` にしたときだけ、ホストの `~/.ssh/config` を `agentsb run` のたびにサンドボックスの `~/.ssh/config` へコピーします。未設定または `false` ならコピーしません。ホスト側が正なので、サンドボックス内の変更は次の run で上書きされます。書き戻しはしません。

コピー時に `IdentityAgent` のパスは、サンドボックス内の転送ソケット `/run/ssh-agent.sock` に書き換えます。ホストの `~/Library/...` のまま置くと、`~` が `/home/agent` に展開されて届きません。`IdentityAgent none` はそのまま残します。次の行は除きます。

- `IdentityFile`
- `IdentitiesOnly`

`Include` は読み込んで 1 ファイルにまとめます。秘密鍵のファイルはコピーしません。ネットワーク許可はこれまでどおり GitHub の 22 番だけなので、config に他のホストがあっても、そこへの接続はポリシーで止まります。

### シークレット（プロキシ注入）

実値をコンテナに置かず、プロキシでリクエストへ注入します。取得元はローカルの `secrets.toml`、または 1Password の Secure Note です。

```toml
# secrets.toml / 1Password 共通の記述例（値はサンプル）
[[secret]]
name = "OPENAI_API_KEY"
value = "sk-..."

[[secret]]
name = "DEEPL_API_KEY"
value = "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx:fx"
domains = ["api.deepl.com", "api-free.deepl.com"]

[[secret]]
name = "CURSOR_API_KEY"
value = "cursor_..."
domains = ["api2.cursor.sh"]
```

| 項目 | 動作 |
|---|---|
| 登録先 | サンドボックス作成前に sbx の global スコープへ登録 |
| 実値の受け渡し | stdin を使用。コマンド引数・ログには出さない |
| コンテナ内の値 | `proxy-managed` / `sbx-cs-…` などの代替値。正常な状態 |
| 内容が前回と同じ | 登録をスキップ |
| 内容が変わった | 既存の sbx シークレットをすべて削除して再登録 |
| `domains` | [組み込みサービス](https://docs.docker.com/ai/sandboxes/security/credentials/#built-in-services)は不要（`secret set`）。それ以外は必須（`set-custom`） |

**追加・変更・削除を反映するには、`agentsb rm` → `agentsb run` が必要です。**
sbx はサンドボックス作成時に global シークレットを取り込むためです。プロジェクトの `.env` は変更しません。

## 内部仕様

### テンプレート

ホストの Docker と sbx はイメージストアが別のため、次の順でロードします。レジストリへの push は不要です。

1. `docker build` — 埋め込み Containerfile（`docker/sandbox-templates:shell-docker` ベース）からビルド
2. `docker image save` — tar へ書き出し
3. `sbx template load` — サンドボックスランタイムへロード

テンプレートタグには Containerfile のハッシュを含め、自動リビルドの判定に使います。

### 保存先とログ

以下は agentsb が自動生成します。手動編集は不要です。

| パス | 内容 |
|---|---|
| `~/.agentsb/build/` | ビルド用の Containerfile と tar |
| `~/.agentsb/home/` | Claude の認証情報 |
| `~/.agentsb/logs/agentsb.log` | 設定や sbx 呼び出しなどの動作ログ |
| `~/.agentsb/secrets.toml.sha256` | シークレットの変更検出用ハッシュ |

- ログは追記式で、2 MiB を超えると `agentsb.log.1` へローテートします。
- `-v` / `--verbose` を付けると、同じログをターミナルにも表示します。
- dotfiles の導入経過はセッション画面の stderr にも表示します。

### herdr 連携

[herdr](https://herdr.dev/) の pane 内では、表示名（例: `claude (agentsb)`）を自動で通知します。状態や完了の検出は herdr が行います。

agentsb は `sbx exec` プロセスの `argv[0]` を `claude` に設定し、ホスト側での識別を助けます。
この名前は固定のため、Codex／Cursor Agent／Gemini CLI 使用時の状態表示は不正確になります。herdr 外での実行には影響しません。

## Acknowledgements

The integration with herdr was inspired by [pall8t](https://github.com/TakiTake/pall8t).
