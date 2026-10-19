package internal

import (
	"strconv"
	"testing"
	"time"
)

var benchStatuses = []string{"CURRENT", "COMPLETED", "PAUSED", "PLANNING", "DROPPED"}

// syntheticList spreads n entries round-robin over the five statuses.
func syntheticList(n int) AnimeList {
	var l AnimeList
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		name := "Show " + strconv.Itoa(i)
		e := Entry{
			Media:     Media{ID: i + 1, MalID: 5000 + i, Title: AnimeTitle{English: name, Romaji: name}},
			Status:    benchStatuses[i%len(benchStatuses)],
			Progress:  i % 13,
			Score:     float64(i % 10),
			UpdatedAt: base.Add(time.Duration(i) * time.Minute),
		}
		switch e.Status {
		case "CURRENT":
			l.Watching = append(l.Watching, e)
		case "COMPLETED":
			l.Completed = append(l.Completed, e)
		case "PAUSED":
			l.Paused = append(l.Paused, e)
		case "PLANNING":
			l.Planning = append(l.Planning, e)
		default:
			l.Dropped = append(l.Dropped, e)
		}
	}
	return l
}

func BenchmarkListMergeDualPlan(b *testing.B) {
	al, mal := syntheticList(1000), syntheticList(1000)
	for i := range mal.Watching {
		mal.Watching[i].Progress++
		mal.Watching[i].UpdatedAt = mal.Watching[i].UpdatedAt.Add(2 * time.Hour)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buildDualRemoteSyncPlan(al, mal)
	}
}

func BenchmarkListMergeCombine(b *testing.B) {
	al, mal := syntheticList(1000), syntheticList(1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mergeAnimeLists(al, mal)
	}
}

func BenchmarkListMergeTitleDistance(b *testing.B) {
	for i := 0; i < b.N; i++ {
		levenshtein("Kusuriya no Hitorigoto Season 2", "The Apothecary Diaries Season 2")
	}
}
