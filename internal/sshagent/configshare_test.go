package sshagent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderUserSSHConfigDropsHostOnlyDirectives(t *testing.T) {
	dir := t.TempDir()
	src := "" +
		"Host github.com\n" +
		"  User git\n" +
		"  IdentityAgent \"~/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock\"\n" +
		"  IdentityFile ~/.ssh/id_ed25519\n" +
		"  IdentitiesOnly yes\n" +
		"  Port 22\n" +
		"\n" +
		"# IdentityFile kept in a comment\n"
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := renderUserSSHConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"1password", "IdentityFile ~/.ssh", "IdentitiesOnly"} {
		if strings.Contains(got, banned) {
			t.Errorf("rendered config still contains %q:\n%s", banned, got)
		}
	}
	for _, want := range []string{"Host github.com", "User git", "Port 22", "IdentityAgent " + sandboxSSHAgentSock, "# IdentityFile kept in a comment"} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered config missing %q:\n%s", want, got)
		}
	}
}

func TestRenderUserSSHConfigKeepsIdentityAgentNone(t *testing.T) {
	dir := t.TempDir()
	src := "Host *.sbx\n  IdentityAgent none\n  User git\n"
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := renderUserSSHConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "IdentityAgent none") {
		t.Fatalf("IdentityAgent none was rewritten:\n%s", got)
	}
	if strings.Contains(got, sandboxSSHAgentSock) {
		t.Fatalf("none was pointed at the forwarded socket:\n%s", got)
	}
}

func TestRenderUserSSHConfigInlinesInclude(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "config.d"), 0o700); err != nil {
		t.Fatal(err)
	}
	extra := "" +
		"Host git.example.com\n" +
		"  User git\n" +
		"  IDENTITYFILE /home/me/.ssh/id_rsa\n"
	if err := os.WriteFile(filepath.Join(dir, "config.d", "work.conf"), []byte(extra), 0o600); err != nil {
		t.Fatal(err)
	}
	main := "Include config.d/*.conf\nHost *\n  ForwardAgent no\n"
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := renderUserSSHConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "Include") || strings.Contains(strings.ToLower(got), "identityfile") {
		t.Fatalf("include or identityfile leaked:\n%s", got)
	}
	if !strings.Contains(got, "Host git.example.com") || !strings.Contains(got, "ForwardAgent no") {
		t.Fatalf("missing inlined or main content:\n%s", got)
	}
}

func TestRenderUserSSHConfigSkipsIncludeCycleAndPrivateKey(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "loop"), []byte("Include config\nHost b\n  User b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	key := "-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n"
	if err := os.WriteFile(filepath.Join(dir, "secret"), []byte(key), 0o600); err != nil {
		t.Fatal(err)
	}
	main := "" +
		"Include loop\n" +
		"Include secret\n" +
		"Include missing\n" +
		"Host only\n" +
		"  User me\n"
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := renderUserSSHConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "PRIVATE KEY") {
		t.Fatalf("private key was inlined:\n%s", got)
	}
	if !strings.Contains(got, "Host b") || !strings.Contains(got, "Host only") {
		t.Fatalf("expected both sides of the cycle and the main host:\n%s", got)
	}
}
