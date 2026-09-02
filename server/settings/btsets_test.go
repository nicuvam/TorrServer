package settings

import (
	"strings"
	"testing"
)

func TestBTSetsStringRedactsQBitPassword(t *testing.T) {
	sets := &BTSets{QBitSettings: QBitConfig{Password: "hunter2"}}
	out := sets.String()
	if strings.Contains(out, "hunter2") {
		t.Fatalf("password leaked into String(): %s", out)
	}
	if sets.QBitSettings.Password != "hunter2" {
		t.Fatal("String() must not mutate the settings")
	}
}
