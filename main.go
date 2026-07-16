// agentsb は apple/container 上で AI コーディングエージェントを
// 使い捨てサンドボックスとして起動する CLI。
// このファイルにはコマンド定義をまとめてある。複雑なコマンドの実装は
// run.go に、実処理は internal/ 配下にある。
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"agentsb/internal/container"
	"agentsb/internal/home"
	"agentsb/internal/image"
	"agentsb/internal/runlog"

	"github.com/spf13/cobra"
)

func main() {
	os.Exit(execute())
}

// execute は CLI を実行し、プロセスの終了ステータスを返す。
func execute() int {
	rootCmd.PersistentFlags().BoolVarP(&verboseFlag, "verbose", "v", false, "mirror diagnostic logs to stderr")
	rootCmd.AddCommand(runCmd, buildCmd, lsCmd, stopCmd, rmCmd, openCmd)
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, _ []string) {
		runlog.SetVerbose(verboseFlag)
		runlog.Open()
		runlog.Info("command %s", strings.Join(os.Args[1:], " "))
	}
	defer runlog.Close()
	if err := rootCmd.Execute(); err != nil {
		runlog.Warn("command failed: %v", err)
		fmt.Fprintln(os.Stderr, "agentsb:", err)
		return 1
	}
	return exitCode
}

// exitCode は、コマンドが Go のエラーなしに完了したときに伝播させる
// プロセス終了ステータス（例: コンテナ内エージェントの exit code）。
var exitCode int

// verboseFlag は -v / --verbose。
var verboseFlag bool

// rootCmd は agentsb のルートコマンド。
var rootCmd = &cobra.Command{
	Use:           "agentsb",
	Short:         "Run AI coding agents in apple/container sandboxes",
	SilenceUsage:  true,
	SilenceErrors: true,
}

// runCmd は agentsb run コマンド。エージェント用サンドボックスを起動する。
var runCmd = newRunCmd()

var (
	runCPUs   int
	runMemory string
)

// newRunCmd は run コマンドをフラグ込みで組み立てる。フラグ変数の初期化式で
// runCmd を参照すると初期化循環（フラグ → runCmd → runRun → フラグ）に
// なるため、コンストラクタで登録する。
func newRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "run [flags]",
		Aliases: []string{"exec"},
		Short:   "Enter the sandbox for the current directory (builds, creates and starts it as needed)",
		Args:    cobra.NoArgs,
		RunE:    runRun,
	}
	cmd.Flags().IntVar(&runCPUs, "cpus", 0, "CPU count (overrides config)")
	cmd.Flags().StringVar(&runMemory, "memory", "", `memory limit, e.g. "8g" (overrides config)`)
	return cmd
}

// buildCmd は agentsb build コマンド。イメージを強制リビルドし、
// 古いイメージを prune する。
var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Force rebuild the sandbox image (picks up base image or tool updates)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := container.EnsureRunning(); err != nil {
			return err
		}
		uid, gid := container.HostIDs()
		tag, err := image.EnsureBuilt(uid, gid, true)
		if err != nil {
			return err
		}
		fmt.Printf("built %s\n", tag)
		return nil
	},
}

// lsCmd は agentsb ls コマンド。agentsb のサンドボックスを一覧する。
var lsCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list", "ps"},
	Short:   "List agentsb sandboxes (including stopped)",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := container.EnsureRunning(); err != nil {
			return err
		}
		containers, err := container.ListAgentsb()
		if err != nil {
			return err
		}
		if len(containers) == 0 {
			fmt.Println("no agentsb sandboxes")
			return nil
		}
		for _, c := range containers {
			fmt.Printf("%-40s  %s\n", c.Name, c.State)
		}
		return nil
	},
}

// targetName は引数で指定された名前を返し、省略時はカレントディレクトリの
// サンドボックス名を返す。
func targetName(args []string) (string, error) {
	if len(args) == 1 {
		return args[0], nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot get working directory: %w", err)
	}
	return container.RunName(cwd), nil
}

// stopCmd は agentsb stop コマンド。コンテナを停止する。
// コンテナと home は残るため、次の run で同じ状態から再開できる。
var stopCmd = &cobra.Command{
	Use:   "stop [name]",
	Short: "Stop a running sandbox (state is kept; the next run resumes it; defaults to the current directory's)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := container.EnsureRunning(); err != nil {
			return err
		}
		name, err := targetName(args)
		if err != nil {
			return err
		}
		if err := container.Stop(name); err != nil {
			return fmt.Errorf("stop %s: %w", name, err)
		}
		fmt.Printf("stopped %s\n", name)
		return nil
	},
}

// openCmd は agentsb open コマンド。カレントディレクトリのサンドボックスの
// IP を調べ、コンテナ内で動くサーバーをホストのブラウザで開く。
// apple/container はコンテナごとに macOS ホストから直接届く IP を割り当てる
// ため、ポート公開の設定なしで http://<IP>:<port>/ にアクセスできる。
var openCmd = &cobra.Command{
	Use:   "open [port]",
	Short: "Open the sandbox's server in the browser (defaults to port 8000)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := container.EnsureRunning(); err != nil {
			return err
		}
		port := 8000
		if len(args) == 1 {
			p, err := strconv.Atoi(args[0])
			if err != nil || p < 1 || p > 65535 {
				return fmt.Errorf("invalid port %q", args[0])
			}
			port = p
		}
		name, err := targetName(nil)
		if err != nil {
			return err
		}
		info, err := container.Get(name)
		if err != nil {
			return err
		}
		if info == nil {
			return fmt.Errorf("no sandbox for this directory — start one with `agentsb run`")
		}
		if info.State != container.StateRunning {
			return fmt.Errorf("sandbox %s is not running — start it with `agentsb run`", name)
		}
		if info.IP == "" {
			return fmt.Errorf("could not determine the IP of %s — check `container ls`", name)
		}
		url := fmt.Sprintf("http://%s:%d/", info.IP, port)
		fmt.Println(url)
		if err := exec.Command("open", url).Run(); err != nil {
			return fmt.Errorf("open %s: %w", url, err)
		}
		return nil
	},
}

// rmCmd は agentsb rm コマンド。サンドボックスのコンテナと home を削除する。
// 名前を省略するとカレントディレクトリのサンドボックスを対象にする。
var rmCmd = &cobra.Command{
	Use:     "rm [name]",
	Aliases: []string{"delete", "remove"},
	Short:   "Remove a sandbox and its home (defaults to the current directory's)",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := container.EnsureRunning(); err != nil {
			return err
		}
		name, err := targetName(args)
		if err != nil {
			return err
		}
		info, err := container.Get(name)
		if err != nil {
			return err
		}
		runHome, err := home.Path(name)
		if err != nil {
			return err
		}
		if info == nil {
			if _, err := os.Stat(runHome); os.IsNotExist(err) {
				return fmt.Errorf("no sandbox named %s", name)
			}
		} else {
			if info.State == container.StateRunning {
				if err := container.Stop(name); err != nil {
					return fmt.Errorf("stop %s: %w", name, err)
				}
			}
			if err := container.Delete(name); err != nil {
				return err
			}
		}
		home.Cleanup(runHome)
		fmt.Printf("removed %s\n", name)
		return nil
	},
}
