# my-docker-sandbox

[Docker Sandbox](https://docs.docker.com/ai/sandboxes/) 上で Claude Code を動かすための開発環境です。  
`docker/sandbox-templates:claude-code` をベースに zsh と dotfiles を追加したカスタムテンプレートをビルドし、`sbx` CLI でコンテナを起動します。

## セットアップ

### 1. Sandbox CLI のインストール

```bash
make install
```

### 2. テンプレートイメージのビルド

> [!WARNING]
> Makefile に `DOCKER_HUB_USERNAME` をハードコードしています。自分の環境で使う場合は、Makefile 先頭を自分の Docker Hub ユーザー名に書き換えてください。

```bash
make build
```

Docker Hub（`<DOCKER_HUB_USERNAME>/claude-sandbox`）へ push されます。

## 使い方

### サンドボックスの起動

```bash
make up
```

コンテナが未存在の場合は作成し、zsh で接続します。停止中の場合も同じコマンドで再接続できます。  
接続後、コンテナ内で Claude Code を実行します。

```bash
claude --dangerously-skip-permissions
```

### その他のコマンド

| コマンド | 説明 |
|----------|------|
| `make stop` | コンテナを停止する |
| `make down` | コンテナを削除する |

### ポートの公開

コンテナ内で起動したサーバーなどをホストからアクセスできるようにするには、`sbx ports` でポートを公開します。

```bash
make port PORT=<ポート番号>
```
