package container

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"agentsb/internal/runlog"
)

const (
	// HomeDir はコンテナ側の home ディレクトリのパス。
	HomeDir = "/home/agent"
	// Workdir はコンテナ側のワークスペースのパス。
	Workdir = HomeDir + "/workspace"

	containerUser = "agent"
	// apple/container は Apple Silicon 専用のため arm64 に固定する。
	platform = "linux/arm64"
)

// EnsureRunning は `container` CLI の存在を確認し、システムサービスが
// 停止していれば起動する。
func EnsureRunning() error {
	if _, err := exec.LookPath("container"); err != nil {
		return fmt.Errorf(
			"the `container` CLI is not available — install apple/container: " +
				"https://github.com/apple/container/releases",
		)
	}
	if exec.Command("container", "system", "status").Run() == nil {
		runlog.Info("container system already running")
		return nil
	}
	runlog.Info("starting container system service")
	fmt.Fprintln(os.Stderr, "agentsb: starting the container system service…")
	cmd := exec.Command("container", "system", "start")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		runlog.Warn("container system start failed: %v", err)
		return err
	}
	return nil
}

// runCLI は `container` サブコマンドを実行し、失敗時は stderr をエラーに含める。
// 成功時の stdout を返す。呼び出し内容は runlog に残す。
func runCLI(args ...string) ([]byte, error) {
	runlog.Info("container %s", strings.Join(args, " "))
	cmd := exec.Command("container", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(string(out))
		}
		if detail != "" {
			runlog.Warn("container %s failed: %v: %s", strings.Join(args, " "), err, detail)
			return out, fmt.Errorf("%w: %s", err, detail)
		}
		runlog.Warn("container %s failed: %v", strings.Join(args, " "), err)
		return out, err
	}
	return out, nil
}

// Mount はホスト側パスとコンテナ側パスの組。
type Mount struct {
	Host string
	Dest string
}

// CreateSpec は `container create` に渡すサンドボックス作成パラメータ一式。
type CreateSpec struct {
	Name   string
	Image  string
	Mounts []Mount
	CPUs   int
	Memory string
	UID    int
	GID    int
}

// Create はサンドボックスのコンテナを作成する。本体プロセスは sleep で待機させ、
// エージェントのセッションは exec（ExecArgs）で入る。
func Create(spec CreateSpec) error {
	args := []string{"create", "--init", "--name", spec.Name, "--platform", platform}
	for _, m := range spec.Mounts {
		args = append(args, "-v", fmt.Sprintf("%s:%s", m.Host, m.Dest))
	}
	args = append(args,
		"-w", Workdir,
		"--user", containerUser,
		"--uid", fmt.Sprintf("%d", spec.UID),
		"--gid", fmt.Sprintf("%d", spec.GID),
		"--cpus", fmt.Sprintf("%d", spec.CPUs),
		"--memory", spec.Memory,
		spec.Image,
		"sleep", "infinity",
	)
	if _, err := runCLI(args...); err != nil {
		return fmt.Errorf("container create: %w", err)
	}
	runlog.Info("created container %s image=%s", spec.Name, spec.Image)
	return nil
}

// ExecArgs は起動済みコンテナでセッションを開始する `container exec` の引数列を
// 返す。実行ユーザーと作業ディレクトリはイメージの USER/WORKDIR
// （agent / ~/workspace）がそのまま使われる。
func ExecArgs(name string, tty bool, command []string) []string {
	args := []string{"exec", "-i"}
	if tty {
		args = append(args, "-t")
	}
	args = append(args, name)
	return append(args, command...)
}

// HostIDs はホストの UID/GID を返す。イメージのユーザー作成と
// コンテナの実行ユーザーに使い、マウントしたファイルの権限を一致させる。
func HostIDs() (int, int) {
	return os.Getuid(), os.Getgid()
}

// RunName はカレントディレクトリのパスから決定的なコンテナ名を生成する。
// 同じディレクトリでは常に同じ名前になるため、同時に起動できる run は
// ディレクトリごとに 1 つ。ディレクトリ名が同じでもパスが異なれば衝突しないよう、
// フルパスの短いハッシュを付ける。
func RunName(cwd string) string {
	sum := sha256.Sum256([]byte(cwd))
	return fmt.Sprintf("agentsb-%s-%x", pathKey(cwd), sum[:3])
}

// pathKey はディレクトリ名をコンテナ名に使える文字（英小文字・数字・ハイフン）
// に正規化する。
func pathKey(path string) string {
	base := filepath.Base(path)
	var b strings.Builder
	for _, r := range strings.ToLower(base) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// ImageExists は指定タグのイメージがローカルに存在するかを返す。
func ImageExists(tag string) bool {
	ok := exec.Command("container", "image", "inspect", tag).Run() == nil
	runlog.Info("image inspect %s exists=%v", tag, ok)
	return ok
}

// BuildImage は Containerfile からイメージをビルドする。ビルドログは stderr へ流す。
// apple/container 1.0 ではビルドはトップレベルの `container build`（`image build` ではない）。
func BuildImage(containerfile, contextDir, tag string, uid, gid int) error {
	args := []string{
		"build",
		"-f", containerfile,
		"--platform", platform,
		"--build-arg", fmt.Sprintf("UID=%d", uid),
		"--build-arg", fmt.Sprintf("GID=%d", gid),
		"-t", tag,
		contextDir,
	}
	runlog.Info("container %s", strings.Join(args, " "))
	cmd := exec.Command("container", args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		runlog.Warn("container build failed: %v", err)
		return err
	}
	runlog.Info("built image %s", tag)
	return nil
}

// DeleteImage は指定タグのイメージを削除する。
func DeleteImage(tag string) error {
	_, err := runCLI("image", "delete", tag)
	return err
}

// ContainerInfo はコンテナ一覧の 1 エントリ（agentsb が使う項目のみ）。
type ContainerInfo struct {
	Name  string
	State string
	Image string
	IP    string
}

// networkAddr は list JSON のネットワークアドレス（CIDR 付き）を表す。
type networkAddr struct {
	Address     string `json:"address"`
	IPv4Address string `json:"ipv4Address"`
}

// listEntry は `container list --format json` が出力する JSON の構造を写したもの。
// apple/container は ManagedContainer をエンコードするため、名前は
// configuration.id、イメージは configuration.image.reference、状態は
// status.state にネストされている（status は文字列ではなくオブジェクト）。
// 稼働中の IP は status.networks[].ipv4Address（1.0.0）。
type listEntry struct {
	Status struct {
		State    string        `json:"state"`
		Networks []networkAddr `json:"networks"`
	} `json:"status"`
	Configuration struct {
		ID    string `json:"id"`
		Image struct {
			Reference string `json:"reference"`
		} `json:"image"`
	} `json:"configuration"`
}

// ip は status.networks の先頭エントリから CIDR サフィックスを除いた IP を返す。
// 未接続（停止中など）の場合は空文字列。
func (e listEntry) ip() string {
	if len(e.Status.Networks) == 0 {
		return ""
	}
	n := e.Status.Networks[0]
	addr := n.IPv4Address
	if addr == "" {
		addr = n.Address
	}
	return strings.SplitN(addr, "/", 2)[0]
}

// listAll は停止中を含む全コンテナを返す。
func listAll() ([]ContainerInfo, error) {
	out, err := runCLI("list", "--all", "--format", "json")
	if err != nil {
		return nil, fmt.Errorf("container list: %w", err)
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		return nil, nil
	}
	var entries []listEntry
	if err := json.Unmarshal(out, &entries); err != nil {
		runlog.Warn("parse container list failed: %v", err)
		return nil, fmt.Errorf("parse container list: %w", err)
	}
	containers := make([]ContainerInfo, 0, len(entries))
	for _, e := range entries {
		containers = append(containers, ContainerInfo{
			Name:  e.Configuration.ID,
			State: e.Status.State,
			Image: e.Configuration.Image.Reference,
			IP:    e.ip(),
		})
	}
	runlog.Info("container list: %d entries", len(containers))
	return containers, nil
}

// ListAgentsb は agentsb が起動したコンテナ（agentsb- プレフィックス）だけを返す。
func ListAgentsb() ([]ContainerInfo, error) {
	all, err := listAll()
	if err != nil {
		return nil, err
	}
	var result []ContainerInfo
	for _, c := range all {
		if strings.HasPrefix(c.Name, "agentsb-") {
			result = append(result, c)
		}
	}
	return result, nil
}

// StateRunning は稼働中コンテナの状態文字列。
const StateRunning = "running"

// Get は指定した名前のコンテナ情報を返す。存在しなければ nil を返す。
func Get(name string) (*ContainerInfo, error) {
	all, err := listAll()
	if err != nil {
		return nil, err
	}
	for _, c := range all {
		if c.Name == name {
			return &c, nil
		}
	}
	return nil, nil
}

// Start は作成済み・停止中のコンテナを起動する。
func Start(name string) error {
	if _, err := runCLI("start", name); err != nil {
		return fmt.Errorf("container start: %w", err)
	}
	return nil
}

// Delete は停止済みのコンテナを削除する。
func Delete(name string) error {
	if _, err := runCLI("delete", name); err != nil {
		return fmt.Errorf("container delete: %w", err)
	}
	return nil
}

// Stop は指定した名前のコンテナを停止する。
func Stop(name string) error {
	_, err := runCLI("stop", name)
	return err
}

// InUseImages は存在するコンテナ（停止中を含む）が使っているイメージ参照の
// 一覧を返す。prune の安全判定に使う。
func InUseImages() ([]string, error) {
	containers, err := listAll()
	if err != nil {
		return nil, err
	}
	var images []string
	for _, c := range containers {
		if c.Image != "" {
			images = append(images, c.Image)
		}
	}
	return images, nil
}

// imageListEntry は `container image list --format json` の 1 エントリ。
// apple/container は ImageResource をエンコードするため、name:tag 形式の
// 参照は configuration.name にネストされている。
type imageListEntry struct {
	Configuration struct {
		Name string `json:"name"`
	} `json:"configuration"`
}

// ListImages は basePrefix に一致するローカルイメージのタグ一覧を返す。
func ListImages(basePrefix string) ([]string, error) {
	out, err := runCLI("image", "list", "--format", "json")
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		return nil, nil
	}
	var entries []imageListEntry
	if err := json.Unmarshal(out, &entries); err != nil {
		return nil, err
	}
	var result []string
	for _, e := range entries {
		if name := e.Configuration.Name; strings.HasPrefix(name, basePrefix+":") {
			result = append(result, name)
		}
	}
	return result, nil
}
