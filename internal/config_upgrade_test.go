package internal

import "testing"

func TestUpgradeStoredValuesSwapsRetiredMALClient(t *testing.T) {
	onDisk := map[string]string{"MyAnimeListClientID": retiredMyAnimeListClientID}
	if !upgradeStoredValues(onDisk) {
		t.Fatal("retired client not reported as changed")
	}
	if got, want := onDisk["MyAnimeListClientID"], defaultConfigMap()["MyAnimeListClientID"]; got != want {
		t.Fatalf("client id = %q, want %q", got, want)
	}
}

func TestUpgradeStoredValuesKeepsOwnMALClient(t *testing.T) {
	onDisk := map[string]string{"MyAnimeListClientID": "my-own-client"}
	if upgradeStoredValues(onDisk) {
		t.Fatal("a user's own client id was rewritten")
	}
	if onDisk["MyAnimeListClientID"] != "my-own-client" {
		t.Fatalf("client id = %q", onDisk["MyAnimeListClientID"])
	}
}
