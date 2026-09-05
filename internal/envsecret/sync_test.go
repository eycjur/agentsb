package envsecret

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fake sbx で、実値が stdin で渡ること、sbx v0.39 の引数形式で呼ぶことを確認する。
func TestSyncPassesValueViaStdin(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	log := filepath.Join(dir, "calls")
	t.Setenv("SBX_TEST_LOG", log)
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$SBX_TEST_LOG"
case "$1 $2" in
 'secret ls')
  printf 'SCOPE      TYPE     NAME     SECRET\n(global)   service  github   ***\n'
  ;;
 'secret set'|'secret set-custom')
  IFS= read -r value
  if [ "$value" != 'fixture-value' ]; then echo 'missing stdin'; exit 2; fi
  ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "sbx"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "agentsb")
	if err := os.MkdirAll(cfg, 0700); err != nil {
		t.Fatal(err)
	}
	data := "[[secret]]\nname = 'DEEPL_API_KEY'\nvalue = 'fixture-value'\ndomains = ['api.deepl.com']\n"
	if err := os.WriteFile(filepath.Join(cfg, "secrets.toml"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	secrets, err := Sync()
	if err != nil {
		t.Fatal(err)
	}
	env := ExecEnv(secrets)
	if !strings.Contains(strings.Join(env, " "), "DEEPL_API_KEY=sbx-cs-") {
		t.Fatalf("unexpected env: %v", env)
	}
	if hosts := Hosts(secrets); len(hosts) != 1 || hosts[0] != "api.deepl.com" {
		t.Fatalf("unexpected hosts: %v", hosts)
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	out := string(calls)
	for _, forbidden := range []string{"fixture-value", " -g "} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("unexpected command: %s", out)
		}
	}
	for _, want := range []string{
		"secret rm -f github",
		"secret set-custom --env DEEPL_API_KEY --placeholder sbx-cs-DEEPLAPIKEY --host api.deepl.com",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
