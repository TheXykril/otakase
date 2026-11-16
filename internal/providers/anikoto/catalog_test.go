package anikoto

import (
	"strings"
	"testing"
)

const catalogItem = `<div class="item "> <div class="inner"> <div class="ani poster tip" data-tip="7457"> <a href="https://anikototv.bz/watch/%s/ep-1"> <img src="x.jpg" alt="x" /> <div class="meta"> <div class="inner"> <div class="right">TV</div> </div> </div> </a> </div> <div class="info"> <div class="b1"> <a class="name d-title" href="https://anikototv.bz/watch/%s/ep-1"%s>%s</a> </div> </div> </div> </div>`

func catalogPage(items ...[3]string) []byte {
	var b strings.Builder
	for _, it := range items {
		jp := ""
		if it[1] != "" {
			jp = ` data-jp="` + it[1] + `"`
		}
		item := strings.Replace(catalogItem, "%s", it[0], 2)
		item = strings.Replace(item, "%s", jp, 1)
		item = strings.Replace(item, "%s", it[2], 1)
		b.WriteString(item)
	}
	// A sidebar poster is not a catalog entry.
	b.WriteString(`<div class="poster"><a href="/watch/other/ep-1"></a></div>`)
	return []byte(b.String())
}

func TestParseCatalogReadsBothNames(t *testing.T) {
	got := parseCatalog(catalogPage(
		[3]string{"solo-leveling-3eukp", "Ore dake Level Up na Ken", "Solo Leveling"},
		[3]string{"kill-blue-1", "", " Kusunoki&#039;s Garden of Gods "},
	))
	if len(got) != 2 {
		t.Fatalf("entries = %+v", got)
	}
	if got[0].Title != "Solo Leveling" || got[0].AltTitle != "Ore dake Level Up na Ken" {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].Title != "Kusunoki's Garden of Gods" || got[1].AltTitle != "" {
		t.Errorf("second = %+v", got[1])
	}
}

func TestCatalogRefusesUnknownOrder(t *testing.T) {
	if _, err := (&client{}).catalog("anything", "", 1); err == nil {
		t.Fatal("unknown order accepted")
	}
}
