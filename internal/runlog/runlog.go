// Package runlog は agentsb の動作検証用ログを ~/.agentsb/logs/ に残す。
// 対話セッションの stdout を汚さないよう既定ではファイルのみへ書き、
// -v のときだけ stderr にも同じ行を出す。
package runlog

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"agentsb/internal/config"
)

const (
	fileName   = "agentsb.log"
	maxBytes   = 2 << 20 // 2 MiB 超えたら agentsb.log.1 へローテート
	timeLayout = "2006-01-02T15:04:05.000Z07:00"
)

var (
	mu      sync.Mutex
	file    *os.File
	verbose bool
	path    string
)

// SetVerbose は stderr へのミラー出力を有効化する。Open の前後どちらでもよい。
func SetVerbose(v bool) {
	mu.Lock()
	defer mu.Unlock()
	verbose = v
}

// Path は現在のログファイルパスを返す（未 Open なら想定パス）。
func Path() string {
	mu.Lock()
	defer mu.Unlock()
	if path != "" {
		return path
	}
	root, err := config.Root()
	if err != nil {
		return ""
	}
	return filepath.Join(root, "logs", fileName)
}

// Open はログファイルを追記オープンする。失敗してもコマンド自体は止めず、
// 以後の Info/Warn は可能な範囲で stderr に落とす。
func Open() {
	mu.Lock()
	defer mu.Unlock()
	root, err := config.Root()
	if err != nil {
		fmt.Fprintf(os.Stderr, "agentsb: warning: cannot resolve log dir: %v\n", err)
		return
	}
	dir := filepath.Join(root, "logs")
	if err := os.MkdirAll(dir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "agentsb: warning: cannot create log dir %s: %v\n", dir, err)
		return
	}
	p := filepath.Join(dir, fileName)
	if err := rotateIfLarge(p); err != nil {
		fmt.Fprintf(os.Stderr, "agentsb: warning: log rotate: %v\n", err)
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agentsb: warning: cannot open log %s: %v\n", p, err)
		return
	}
	file = f
	path = p
	writeLocked("info", "log opened path=%s verbose=%v", p, verbose)
}

// Close はログファイルを閉じる。
func Close() {
	mu.Lock()
	defer mu.Unlock()
	if file != nil {
		_ = file.Close()
		file = nil
	}
}

// Info は通常の経過ログを書く。
func Info(format string, args ...any) {
	mu.Lock()
	defer mu.Unlock()
	writeLocked("info", format, args...)
}

// Warn は警告をログに書く。ユーザー向け表示は呼び出し側の stderr に任せる。
func Warn(format string, args ...any) {
	mu.Lock()
	defer mu.Unlock()
	writeLocked("warn", format, args...)
}

func writeLocked(level, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("%s %s %s\n", time.Now().Format(timeLayout), level, msg)
	if file != nil {
		_, _ = file.WriteString(line)
	}
	if verbose {
		fmt.Fprint(os.Stderr, "agentsb: ", msg, "\n")
	}
}

func rotateIfLarge(p string) error {
	st, err := os.Stat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if st.Size() < maxBytes {
		return nil
	}
	bak := p + ".1"
	_ = os.Remove(bak)
	return os.Rename(p, bak)
}
