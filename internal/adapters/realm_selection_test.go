package adapters

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/giacomoguidotto/userland/internal/platform"
)

func TestRealmSelectionRecordsAnExplicitChoiceOfNone(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	state := filepath.Join(base, "state")
	if err := os.MkdirAll(filepath.Join(root, "cfg"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	declarations := "name,repository,configuration_path,default_path,branch,mode\n" +
		"one,git@example.test:private/one.git,~/one,~/one,main,optional\n" +
		"two,git@example.test:private/two.git,~/two,~/two,main,optional\n"
	if err := os.WriteFile(filepath.Join(root, "cfg", "realms.csv"), []byte(declarations), 0o600); err != nil {
		t.Fatal(err)
	}
	env := platform.NewEnvironment([]string{
		"USERLAND_ROOT=" + root,
		"USERLAND_HOME=" + filepath.Join(base, "home"),
		"USERLAND_STATE_DIR=" + state,
	})
	var output bytes.Buffer
	invocation := &Context{
		Context: context.Background(), Env: env, Stdin: strings.NewReader("n\nn\n"), Output: &output, Terminal: true,
	}
	if code := realmSelection(invocation, Apply); code != 0 {
		t.Fatalf("selection returned %d: %#v", code, invocation.Events)
	}
	if got, err := os.ReadFile(filepath.Join(state, "realms.csv")); err != nil || string(got) != "name,path\n" {
		t.Fatalf("empty selection was not recorded: %q %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(state, "realm-selection")); err != nil || string(got) != "complete\n" {
		t.Fatalf("selection completion was not recorded: %q %v", got, err)
	}
	second := &Context{Context: context.Background(), Env: env}
	if code := realmSelection(second, Plan); code != 0 || len(second.Events) != 0 {
		t.Fatalf("recorded selection was prompted again: %d %#v", code, second.Events)
	}
}
