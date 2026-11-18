//go:build linux

package deploy

import (
	"encoding/json"
	"testing"
)

func TestOrdinaryRecipeRestartMatchesDockerPolicy(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  fenceRestart
		valid bool
	}{
		{"", fenceRestart{Name: "no"}, true},
		{"no", fenceRestart{Name: "no"}, true},
		{"unless-stopped", fenceRestart{Name: "unless-stopped"}, true},
		{"always", fenceRestart{Name: "always"}, true},
		{"on-failure:3", fenceRestart{Name: "on-failure", MaximumRetryCount: 3}, true},
		{"always:3", fenceRestart{}, false},
		{"on-failure:-1", fenceRestart{}, false},
		{"on-failure:03", fenceRestart{}, false},
	} {
		got, valid := ordinaryRecipeRestart(tc.value)
		if valid != tc.valid || got != tc.want {
			t.Fatalf("restart %q: got %+v valid=%v", tc.value, got, valid)
		}
	}
}

func TestOrdinaryDatabaseRestartMustMatchRecordedRecipe(t *testing.T) {
	intent := ordinaryUpgradeIntent{Request: ordinaryUpgradeRequest{DatabaseContainerID: "db-id"}, Fence: fenceJournal{Containers: []fenceContainer{{ID: "db-id", Service: "db", Restart: fenceRestart{Name: "unless-stopped"}}}}}
	if !ordinaryDatabaseRecipeMatches(intent, map[string]ordinaryRuntimeRecipeService{"db": {Restart: "unless-stopped"}}) {
		t.Fatal("matching recorded database policy rejected")
	}
	for _, value := range []string{"always", "no", "on-failure:3", ""} {
		if ordinaryDatabaseRecipeMatches(intent, map[string]ordinaryRuntimeRecipeService{"db": {Restart: value}}) {
			t.Fatalf("database policy drift %q accepted", value)
		}
	}
	if ordinaryDatabaseRecipeMatches(intent, nil) {
		t.Fatal("missing database recipe accepted")
	}
}

func TestOrdinaryComposeProxySelection(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"COMPOSE_FILE=compose.yaml:compose.proxy-network.yaml\n", "compose.proxy-network.yaml"},
		{"COMPOSE_FILE=compose.yaml:compose.proxy-host.yaml\n", "compose.proxy-host.yaml"},
		{"", ""},
		{"COMPOSE_FILE=compose.yaml\n", ""},
		{"COMPOSE_FILE=compose.yaml:compose.proxy-host.yaml:compose.proxy-network.yaml\n", ""},
		{"COMPOSE_FILE=./compose.yaml:compose.proxy-host.yaml\n", ""},
		{"COMPOSE_FILE=compose.yaml:compose.proxy-host.yaml\nCOMPOSE_FILE=compose.yaml:compose.proxy-network.yaml\n", ""},
	} {
		got, err := ordinaryComposeProxySelection([]byte(tc.input))
		if got != tc.want || (err == nil) != (tc.want != "") {
			t.Fatalf("selection %q: %q %v", tc.input, got, err)
		}
	}
}

func TestOrdinaryCenterNetworksMatchMergedProxy(t *testing.T) {
	var cfg struct {
		Services map[string]ordinaryRuntimeRecipeService `json:"services"`
		Networks map[string]struct {
			Name string `json:"name"`
		} `json:"networks"`
	}
	if err := json.Unmarshal([]byte(`{"services":{"houfeng":{"networks":{"default":null,"proxy":{"aliases":["houfeng"]}}}},"networks":{"default":{"name":"demo_default"},"proxy":{"name":"npm"}}}`), &cfg); err != nil {
		t.Fatal(err)
	}
	actual := map[string]fenceNetwork{"demo_default": {}, "npm": {Aliases: []string{"houfeng", "container"}}}
	if !ordinaryCenterNetworksMatch(actual, cfg.Services["houfeng"], cfg.Networks) {
		t.Fatal("merged proxy network rejected")
	}
	delete(actual, "npm")
	if ordinaryCenterNetworksMatch(actual, cfg.Services["houfeng"], cfg.Networks) {
		t.Fatal("missing proxy network accepted")
	}
	actual["npm"] = fenceNetwork{Aliases: []string{"wrong"}}
	if ordinaryCenterNetworksMatch(actual, cfg.Services["houfeng"], cfg.Networks) {
		t.Fatal("missing proxy alias accepted")
	}
}
