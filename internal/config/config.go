package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config は config.toml 全体の構造。
type Config struct {
	Container ContainerConfig `toml:"container"`
	Dotfiles  DotfilesConfig  `toml:"dotfiles"`
}

// ContainerConfig はコンテナのリソース設定。
type ContainerConfig struct {
	CPUs   int    `toml:"cpus"`
	Memory string `toml:"memory"`
}

// DotfilesConfig はコンテナ起動時に適用する dotfiles の設定。
type DotfilesConfig struct {
	Repository     string `toml:"repository"`
	TargetPath     string `toml:"target_path"`
	InstallCommand string `toml:"install_command"`
}

// Root はデータディレクトリ ~/.agentsb を返す（home / build / logs など）。
func Root() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".agentsb"), nil
}

// ConfigDir は設定ディレクトリを返す。
// $XDG_CONFIG_HOME/agentsb、未設定なら ~/.config/agentsb。
func ConfigDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "agentsb"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".config", "agentsb"), nil
}

// GlobalPath は設定ファイルパス（~/.config/agentsb/config.toml）を返す。
func GlobalPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// legacyGlobalPath は移行前の ~/.agentsb/config.toml。
func legacyGlobalPath() (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "config.toml"), nil
}

// loadedPath は直近の Load が実際に読んだパス（無い場合は推奨パス）。
var loadedPath string

// LoadedPath は直近の Load が参照した config.toml のパスを返す。
func LoadedPath() string {
	return loadedPath
}

// Load はデフォルト値の上に config.toml の内容を重ねて返す。
// 優先は ~/.config/agentsb/config.toml。無ければ旧パス ~/.agentsb/config.toml
// を読む（移行用）。どちらも無くてよい。
func Load() (Config, error) {
	cfg := Config{
		Container: ContainerConfig{CPUs: 4, Memory: "4g"},
	}
	path, err := GlobalPath()
	if err != nil {
		return cfg, err
	}
	loadedPath = path

	readPath := path
	if _, err := os.Stat(path); os.IsNotExist(err) {
		legacy, lerr := legacyGlobalPath()
		if lerr != nil {
			return cfg, lerr
		}
		if _, err := os.Stat(legacy); err == nil {
			readPath = legacy
			loadedPath = legacy
		} else {
			return cfg, nil
		}
	} else if err != nil {
		return cfg, err
	}

	if _, err := toml.DecodeFile(readPath, &cfg); err != nil {
		return cfg, fmt.Errorf("invalid config %s: %w", readPath, err)
	}
	// repository だけ書いた場合でも動くよう、install の既定名を補う。
	if cfg.Dotfiles.Repository != "" && cfg.Dotfiles.InstallCommand == "" {
		cfg.Dotfiles.InstallCommand = "install.sh"
	}
	return cfg, nil
}
