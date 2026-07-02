# Claude Sandbox (GPU)

GPU 環境向けに Docker で Claude Code を動かす開発環境です。  
`ubuntu:26.04` ベースのカスタムイメージを CI でビルドし、Docker Hub から pull して Linux 上で使います。

Apple container は GPU パススルーに非対応なため、本ブランチでは標準 Docker + [NVIDIA Container Toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html) を使います。

## 前提

- Linux (GPU サーバー想定)
- Docker Engine
- NVIDIA ドライバ + nvidia-container-toolkit (`make install` で動作確認)

## セットアップ

```bash
make install
```

Docker daemon の起動と、コンテナから GPU が見えるかを確認します。

## 使い方

```bash
make run
```

初回はイメージを pull してコンテナを作成し、zsh（login shell）に入ります。2回目以降は起動してから接続します。

コンテナ内で Claude Code を yolo モードで起動する例:

```bash
claude-yolo
# または
claude --dangerously-skip-permissions
```

### セキュリティ上の注意

yolo モード (`--dangerously-skip-permissions`) はツール実行の確認をスキップします。本リポジトリでは次の対策を入れています。

| レイヤ | 内容 |
|--------|------|
| コンテナ | 非 root ユーザー `agent`、`--cap-drop=ALL`、`--security-opt=no-new-privileges`、メモリ上限 |
| マウント | カレントディレクトリのみ (`/home/agent/workspace`)。`~/.ssh` や `~/.aws` はマウントしない |
| Claude Code | `.claude/settings.json` で危険コマンド・機密ファイル読み取りを deny、sandbox 有効 |
| GPU | `--gpus all` 固定 |

信頼できないコードや機密ファイルを含むリポジトリでは yolo モードを使わないでください。

### Web サーバー

アプリは `0.0.0.0` で listen してください。ホストからはポートフォワード経由でアクセスします。

```bash
# コンテナ内
python3 -m http.server 8000 --bind 0.0.0.0

# ホスト側（デフォルト PORT=8000）
make open

# ポートを変える場合
make open PORT=5173
```

| コマンド | 説明 |
|----------|------|
| `make run` | コンテナを作成/起動して zsh に入る |
| `make open` | `localhost:PORT` をブラウザで開く（`PORT` 既定値: 8000） |
| `make stop` | コンテナを停止する |
| `make rm` | コンテナを削除する |
| `make help` | コマンド一覧を表示する |

### 環境変数

| 変数 | 既定値 | 説明 |
|------|--------|------|
| `MEMORY` | `8g` | コンテナのメモリ上限 |
| `PORT` | `8000` | ホストへ公開するポート |
| `PLATFORM` | `linux/amd64` | `make build` 時のターゲットプラットフォーム |

## イメージのビルド

CI（`ubuntu-latest`）で Docker Hub へ push:

```bash
make build
```

> [!WARNING]
> Makefile の `DOCKER_HUB_USERNAME` は `eycjur` 固定です。自分のイメージを使う場合は書き換えてください。
