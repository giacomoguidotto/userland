package planner

import (
	"testing"

	"github.com/giacomoguidotto/userland/internal/plan"
)

func TestRollingPackagesDeclarePresenceWithoutUpgradeDrift(t *testing.T) {
	value := plan.New()
	encoded := []byte(`{"resources":[
		{"id":{"kind":"package","name":"brew:missing"},"current":"absent","desired":"latest","action":"create"},
		{"id":{"kind":"package","name":"brew:installed"},"current":"1","desired":"latest","action":"update"}
	]}`)
	if err := importResources(value, encoded); err != nil {
		t.Fatal(err)
	}
	items := value.Items()
	if len(items) != 1 || items[0].Action != "install" || items[0].Target != "missing" {
		t.Fatalf("rolling package updates leaked into convergence: %#v", items)
	}
}
