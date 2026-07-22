// Package envsecret は ~/.config/agentsb/secrets.toml を sbx global へプロキシ注入する。
// 組み込みは secret set -g、それ以外は set-custom -g。内容が同じなら set をスキップする。
package envsecret

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/BurntSushi/toml"

	"agentsb/internal/config"
)

const fileName = "secrets.toml"

// builtinByEnv は sbx 組み込みサービス名。set-custom ではなく secret set -g で登録する。
var builtinByEnv = map[string]string{
	"OPENAI_API_KEY":     "openai",
	"ANTHROPIC_API_KEY":  "anthropic",
	"GOOGLE_API_KEY":     "google",
	"GEMINI_API_KEY":     "google",
	"GROQ_API_KEY":       "groq",
	"MISTRAL_API_KEY":    "mistral",
	"OPENROUTER_API_KEY": "openrouter",
	"XAI_API_KEY":        "xai",
	"GITHUB_TOKEN":       "github",
	"GH_TOKEN":           "github",
}

type file struct {
	Secret []Secret `toml:"secret"`
}

// Secret はプロキシ置換する 1 シークレット。
type Secret struct {
	Name    string   `toml:"name"`
	Value   string   `toml:"value"`
	Domains []string `toml:"domains"` // カスタム必須。組み込みは不要
}

// Path は ~/.config/agentsb/secrets.toml を返す。
func Path() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fileName), nil
}

// Load は secrets.toml を読む。無ければ (nil, nil)。
func Load() ([]Secret, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var f file
	if err := toml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", path, err)
	}
	for i := range f.Secret {
		s := &f.Secret[i]
		if s.Name == "" {
			return nil, fmt.Errorf("%s: secret[%d]: name is required", path, i)
		}
		if s.Value == "" {
			return nil, fmt.Errorf("%s: secret[%d] (%s): value is required", path, i, s.Name)
		}
		_, builtin := builtinByEnv[s.Name]
		if !builtin && len(s.Domains) == 0 {
			return nil, fmt.Errorf("%s: secret[%d] (%s): domains is required", path, i, s.Name)
		}
		for j, d := range s.Domains {
			d = strings.TrimSpace(d)
			if d == "" {
				return nil, fmt.Errorf("%s: secret[%d] (%s): empty domain", path, i, s.Name)
			}
			s.Domains[j] = d
		}
	}
	return f.Secret, nil
}

// placeholderFor は env 名から決定的なプレースホルダを返す（DEEPL_API_KEY → sbx-cs-DEEPLAPIKEY）。
func placeholderFor(env string) string {
	var b strings.Builder
	b.WriteString("sbx-cs-")
	for _, r := range env {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// execEnv はカスタムシークレット用の KEY=placeholder（sbx exec -e 向け）。
func execEnv(secrets []Secret) []string {
	var out []string
	for _, s := range secrets {
		if _, ok := builtinByEnv[s.Name]; ok {
			continue
		}
		out = append(out, s.Name+"="+placeholderFor(s.Name))
	}
	return out
}
