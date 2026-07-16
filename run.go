package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"agentsb/internal/config"
	"agentsb/internal/container"
	"agentsb/internal/dotfiles"
	"agentsb/internal/herdr"
	"agentsb/internal/home"
	"agentsb/internal/image"
	"agentsb/internal/runlog"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// runRun はサンドボックスの状態を問わず「入れる状態」まで進めてセッションを開く:
// イメージが無ければビルド、コンテナが無ければ作成、停止中なら起動、
// 起動済みなら exec するだけ。セッション終了後に認証情報をベース home へ同期する。
func runRun(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if runCPUs > 0 {
		cfg.Container.CPUs = runCPUs
	}
	if runMemory != "" {
		cfg.Container.Memory = runMemory
	}
	logConfig(cfg)
	if err := container.EnsureRunning(); err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cannot get working directory: %w", err)
	}

	// herdr の pane 内なら表示名を報告し、状態検出用に argv[0] へ埋める
	// エージェント名を決めておく（詳細は internal/herdr のパッケージコメント）。
	herdrEnv := herdr.Detect()
	var herdrAgent string
	if herdrEnv != nil {
		herdrAgent = herdrEnv.Agent
		if herdrAgent == "" {
			herdrAgent = "zsh"
		}
		herdrEnv.Announce(herdrAgent)
	}

	uid, gid := container.HostIDs()
	runName := container.RunName(cwd)
	runlog.Info("run cwd=%s name=%s uid=%d gid=%d", cwd, runName, uid, gid)

	info, err := container.Get(runName)
	if err != nil {
		return err
	}
	if info == nil {
		runlog.Info("sandbox %s does not exist yet", runName)
	} else {
		runlog.Info("sandbox %s state=%s image=%s ip=%s", info.Name, info.State, info.Image, info.IP)
	}

	// agentsb の更新でイメージ定義が変わっていたら、コンテナだけ作り直す。
	// home は維持されるため、消えるのはコンテナ層の変更（apt install など）だけ。
	if info != nil && !strings.HasSuffix(info.Image, image.Tag(uid, gid)) {
		runlog.Info("image definition changed; recreating sandbox %s (was %s, want tag ending %s)",
			runName, info.Image, image.Tag(uid, gid))
		fmt.Fprintf(os.Stderr, "agentsb: image definition changed, recreating sandbox %s…\n", runName)
		if info.State == container.StateRunning {
			if err := container.Stop(runName); err != nil {
				return fmt.Errorf("stop %s: %w", runName, err)
			}
		}
		if err := container.Delete(runName); err != nil {
			return err
		}
		info = nil
	}

	if info == nil {
		imageTag, err := image.EnsureBuilt(uid, gid, false)
		if err != nil {
			return err
		}
		runHome, err := home.Ensure(runName)
		if err != nil {
			return fmt.Errorf("cannot prepare home: %w", err)
		}
		runlog.Info("prepared home %s", runHome)
		// workspace のマウントは home のマウントの内側にネストする。ランタイムが
		// 指定順にマウントしても workspace が隠れないよう home を先に並べ、home に
		// マウントポイントが無ければ作成しておく。
		workspaceRel := strings.TrimPrefix(container.Workdir, container.HomeDir+"/")
		if err := os.MkdirAll(filepath.Join(runHome, workspaceRel), 0755); err != nil {
			return fmt.Errorf("cannot create workspace mountpoint: %w", err)
		}
		spec := container.CreateSpec{
			Name:  runName,
			Image: imageTag,
			Mounts: []container.Mount{
				{Host: runHome, Dest: container.HomeDir},
				{Host: cwd, Dest: container.Workdir},
			},
			CPUs:   cfg.Container.CPUs,
			Memory: cfg.Container.Memory,
			UID:    uid,
			GID:    gid,
		}
		if err := container.Create(spec); err != nil {
			return err
		}
		info = &container.ContainerInfo{Name: runName}
	} else if runCPUs > 0 || runMemory != "" {
		fmt.Fprintln(os.Stderr, "agentsb: warning: --cpus/--memory apply only when the sandbox is created — `agentsb rm` first to apply them")
	}

	if info.State != container.StateRunning {
		runlog.Info("starting sandbox %s", runName)
		if err := container.Start(runName); err != nil {
			return err
		}
	}

	// セッションはログインシェル固定。エージェントはシェル内から手動で起動する。
	// [dotfiles] が設定されていれば、clone/インストールを済ませてからシェルへ
	// exec する起動スクリプトで包む（詳細は internal/dotfiles）。
	command := []string{"zsh", "-l"}
	if cfg.Dotfiles.Repository != "" {
		command = dotfiles.Command(
			cfg.Dotfiles.Repository,
			cfg.Dotfiles.TargetPath,
			cfg.Dotfiles.InstallCommand,
			command,
		)
		runlog.Info("session will bootstrap dotfiles then exec %v", []string{"zsh", "-l"})
	} else {
		runlog.Info("session command: %v (dotfiles disabled)", command)
	}

	runlog.Info("exec session argv0=%q command=%v", herdrAgent, command)
	code, err := execSession(runName, herdrAgent, command)
	runlog.Info("session finished exit=%d err=%v", code, err)

	// セッションの終わり方によらず、認証情報の同期は必ず行う。コンテナと home は
	// `rm` まで維持される。完了の herdr への報告は不要: exec プロセスの終了とともに
	// argv[0] がプロセスツリーから消え、herdr が自前で検出する。
	if runHome, homeErr := home.Path(runName); homeErr == nil {
		if syncErr := home.SyncCredentials(runHome); syncErr != nil {
			runlog.Warn("could not sync credentials: %v", syncErr)
			fmt.Fprintf(os.Stderr, "agentsb: warning: could not sync credentials: %v\n", syncErr)
		} else {
			runlog.Info("synced credentials from %s", runHome)
		}
	}

	if err != nil {
		return err
	}
	exitCode = code
	return nil
}

// logConfig は読み込んだ設定の要点をログに残す（dotfiles 未設定の取りこぼし防止）。
func logConfig(cfg config.Config) {
	preferred, _ := config.GlobalPath()
	cfgPath := config.LoadedPath()
	if _, err := os.Stat(cfgPath); err != nil {
		runlog.Info("config file missing path=%s (using defaults)", preferred)
	} else {
		runlog.Info("config file loaded path=%s", cfgPath)
		if preferred != "" && cfgPath != preferred {
			runlog.Info("config: using legacy path; move to %s when convenient", preferred)
		}
	}
	runlog.Info("config container cpus=%d memory=%s", cfg.Container.CPUs, cfg.Container.Memory)
	if cfg.Dotfiles.Repository == "" {
		runlog.Info("config dotfiles=disabled (set [dotfiles].repository in %s)", preferred)
		return
	}
	target := cfg.Dotfiles.TargetPath
	if target == "" {
		target = "~/dotfiles"
	}
	runlog.Info("config dotfiles repository=%s target=%s install=%s",
		cfg.Dotfiles.Repository, target, cfg.Dotfiles.InstallCommand)
}

// execSession は稼働中のサンドボックスで `container exec` を前面実行し、
// セッションの終了コードを返す。
// argv0 が空でなければ、exec プロセスの argv[0] をその名前に書き換える:
// herdr はホストのプロセスツリーからエージェントを識別して画面内容の状態検出を
// 行うため、コンテナ内で動くエージェントの名前をホスト側プロセスに映しておく。
func execSession(name, argv0 string, command []string) (int, error) {
	args := container.ExecArgs(name, term.IsTerminal(int(os.Stdin.Fd())), command)
	cmd := exec.Command("container", args...)
	if argv0 != "" {
		cmd.Args[0] = argv0
		// 将来 herdr が env ヒントを拾えるようにしておく。
		cmd.Env = append(os.Environ(), "HERDR_AGENT="+argv0)
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return 1, fmt.Errorf("cannot start session: %w", err)
	}

	// シグナルを自分で受けて子に転送する: agentsb 自身が即死すると、
	// この後の認証情報の同期が走らなくなるため。
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)
	go func() {
		for sig := range sigCh {
			cmd.Process.Signal(sig)
		}
	}()

	err := cmd.Wait()
	signal.Stop(sigCh)
	close(sigCh)

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				return 128 + int(status.Signal()), nil
			}
			return exitErr.ExitCode(), nil
		}
		return 1, err
	}
	return 0, nil
}
