# Claude Sandbox (OpenShell MicroVM + GPU)

Linux 上で [NVIDIA OpenShell](https://docs.nvidia.com/openshell/) の **MicroVM ドライバー** を使い、GPU パススルー付きの VM 境界内で Claude Code を動かす開発環境です。

従来の Docker + NVIDIA Container Toolkit 構成から、OpenShell の VM 隔離（KVM / QEMU + VFIO GPU パススルー）に移行しています。

## 前提

- Linux（GPU サーバー想定）
- [NVIDIA OpenShell](https://docs.nvidia.com/openshell/latest/about/installation)（CLI + gateway + `openshell-driver-vm`）
- Docker Engine（`--from ./Dockerfile` のイメージビルド用。VM ドライバーがローカルイメージ解決に利用）
- KVM（`/dev/kvm`）
- NVIDIA ドライバ（ホスト）
- GPU パススルー: **IOMMU 有効** + VFIO 対応（実験的機能）

### OpenShell のインストール

```bash
make install
```

`curl ... | sh` だけだと **compute driver 未設定** のまま gateway が起動せず、末尾で次のエラーになります。

```
configuration error: no compute driver configured and auto-detection found no suitable driver
```

これは想定内です。`make install` が `gateway.toml`（`compute_drivers = ["vm"]`）を配置して gateway を再起動します。

手動で直す場合:

```bash
make gateway-config
```

GPU パススルーでは gateway が VFIO で GPU を bind/unbind するため、環境によっては **root 権限** が必要です。うまくいかない場合は [VM driver README](https://github.com/NVIDIA/OpenShell/blob/main/crates/openshell-driver-vm/README.md) を参照してください。

## セットアップ

```bash
make install
```

KVM / IOMMU / OpenShell gateway / Docker の状態を確認します。VM ドライバーは自動検出されないため、`make install` 内で `gateway.toml` を必ず配置します。

## 使い方

```bash
make run
```

初回は `./Dockerfile` からイメージをビルドし、GPU 付き MicroVM サンドボックスを作成して zsh に接続します。カレントディレクトリは `/sandbox` にアップロードされます。2 回目以降は既存サンドボックスに接続します。

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
| MicroVM | VM 境界による隔離（コンテナ名前空間より強い分離） |
| OpenShell | ネットワークポリシー、プロキシ経由 egress、サンドボックス supervisor |
| ワークスペース | 作成時にカレントディレクトリのみ `/sandbox` へ upload（`~/.ssh` 等は含めない） |
| Claude Code | `.claude/settings.json` で危険コマンド・機密ファイル読み取りを deny、sandbox 有効 |
| GPU | `--gpu` 固定（VM ドライバーは最大 1 GPU） |

信頼できないコードや機密ファイルを含むリポジトリでは yolo モードを使わないでください。

### Web サーバー

アプリは `0.0.0.0` で listen してください。ホストからはポートフォワード経由でアクセスします（`make run` 時に `--forward` 済み）。

```bash
# サンドボックス内
python3 -m http.server 8000 --bind 0.0.0.0

# ホスト側（デフォルト PORT=8000）
make open
```

| コマンド | 説明 |
|----------|------|
| `make run` | GPU 付き MicroVM サンドボックスを作成/接続 |
| `make connect` | 既存サンドボックスに接続 |
| `make upload` | カレントディレクトリを `/sandbox` に再同期 |
| `make open` | `localhost:PORT` をブラウザで開く |
| `make rm` | サンドボックスを削除 |
| `make gateway-config` | MicroVM 用 `gateway.toml` を配置 |
| `make help` | コマンド一覧 |

### 環境変数

| 変数 | 既定値 | 説明 |
|------|--------|------|
| `SANDBOX_NAME` | ディレクトリ名 | サンドボックス名 |
| `MEMORY` | `8Gi` | サンドボックスのメモリ上限 |
| `PORT` | `8000` | ホストへフォワードするポート |
| `GPU` | `1` | 要求 GPU 数（VM ドライバーは 1 のみ） |
| `GPU_DEVICE` | 未設定 | PCI BDF で GPU を指定（例: `0000:2d:00.0`） |
| `IMAGE` | `<name>:latest` | `make build` のイメージタグ |

### GPU デバイスを指定する

複数 GPU がある場合:

```bash
make run GPU_DEVICE=0000:2d:00.0
```

## イメージ

`Dockerfile` は `nvidia/cuda:12.6.3-base-ubuntu24.04` ベースです。OpenShell の VM ゲスト init が VFIO 経由で GPU を初期化し、`nvidia-smi` で検証します。

レジストリに push してリモート gateway から使う場合:

```bash
make build IMAGE=your-registry/claude-sandbox:latest
docker push your-registry/claude-sandbox:latest

openshell sandbox create --from your-registry/claude-sandbox:latest --gpu -- claude
```

## 参考リンク

- [OpenShell Installation](https://docs.nvidia.com/openshell/latest/about/installation)
- [Sandbox Compute Drivers (MicroVM)](https://docs.nvidia.com/openshell/latest/reference/sandbox-compute-drivers)
- [Manage Sandboxes](https://docs.nvidia.com/openshell/latest/sandboxes/manage-sandboxes)
- [Gateway Configuration](https://docs.nvidia.com/openshell/latest/reference/gateway-config)
