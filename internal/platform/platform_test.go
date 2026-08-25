package platform

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestMiseInvocationUsesOnlyPersonalConfiguration(t *testing.T) {
	root := t.TempDir()
	env := NewEnvironment([]string{
		"USERLAND_ROOT=" + root,
		"USERLAND_MISE=/opt/userland/bin/mise",
		"MISE_OVERRIDE_CONFIG_FILENAMES=parent.toml",
		"PATH=/usr/bin:/bin",
	})

	invocation := env.MiseInvocation("bootstrap", "plan", "--json")

	if invocation.Name != "/opt/userland/bin/mise" {
		t.Fatalf("name = %q", invocation.Name)
	}
	wantArgs := []string{"-C", filepath.Join(root, "cfg"), "bootstrap", "plan", "--json"}
	if !reflect.DeepEqual(invocation.Args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", invocation.Args, wantArgs)
	}
	values := NewEnvironment(invocation.Environ)
	if got := values.Get("MISE_OVERRIDE_CONFIG_FILENAMES"); got != "mise.toml" {
		t.Fatalf("MISE_OVERRIDE_CONFIG_FILENAMES = %q, want mise.toml", got)
	}
}
