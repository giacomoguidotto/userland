package adapters

import "testing"

func TestParsePMSetCustom(t *testing.T) {
	values, err := parsePMSetCustom([]byte(`Battery Power:
 displaysleep 20
 sleep 0
AC Power:
 displaysleep 0
 sleep 0
`))
	if err != nil {
		t.Fatal(err)
	}
	for _, setting := range desiredPowerSettings {
		if got := values[setting.profile][setting.key]; got != setting.want {
			t.Fatalf("%s %s = %d, want %d", setting.profile, setting.key, got, setting.want)
		}
	}
}

func TestParsePMSetCustomRejectsMissingSetting(t *testing.T) {
	if _, err := parsePMSetCustom([]byte("Battery Power:\n displaysleep 20\n")); err == nil {
		t.Fatal("missing power setting was accepted")
	}
}
