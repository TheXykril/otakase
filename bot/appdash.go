package main

import (
	"fmt"
	"html/template"
	"sort"
	"time"
)

// The dashboard's "Linked apps" page: how many apps are linked, which
// versions and systems they run, who watches what right now and the most
// watch time this week.

func init() {
	addDashSection("Linked apps", (*bot).dashApps)
}

type appCounts struct {
	Apps, Members, Active int
	Versions, Systems     []showCount // reusing name+count rows
	TopWeek               []struct {
		User    string
		Seconds int64
	}
}

// appCountsAt counts the linked apps; active ones were seen in the last week.
func (st *store) appCountsAt(now time.Time) appCounts {
	var c appCounts
	versions, systems, members := map[string]int{}, map[string]int{}, map[string]bool{}
	week := isoWeekKey(now)
	st.view(func(d *storeData) {
		for _, l := range d.AppLinks {
			c.Apps++
			members[l.User] = true
			if now.Sub(l.LastSeen) < 7*24*time.Hour {
				c.Active++
			}
			v := l.Version
			if v == "" {
				v = "not seen yet"
			}
			versions[v]++
			systems[osLabel(l.OS)]++
		}
		for u, s := range d.AppStats {
			if n := s.Weeks[week]; n > 0 {
				c.TopWeek = append(c.TopWeek, struct {
					User    string
					Seconds int64
				}{u, n})
			}
		}
	})
	c.Members = len(members)
	c.Versions, c.Systems = sortedCounts(versions), sortedCounts(systems)
	sort.Slice(c.TopWeek, func(i, j int) bool {
		if c.TopWeek[i].Seconds != c.TopWeek[j].Seconds {
			return c.TopWeek[i].Seconds > c.TopWeek[j].Seconds
		}
		return c.TopWeek[i].User < c.TopWeek[j].User
	})
	if len(c.TopWeek) > 10 {
		c.TopWeek = c.TopWeek[:10]
	}
	return c
}

func sortedCounts(m map[string]int) []showCount {
	out := make([]showCount, 0, len(m))
	for k, n := range m {
		out = append(out, showCount{k, n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Title < out[j].Title
	})
	return out
}

func (b *bot) dashApps() template.HTML {
	now := appNow()
	c := b.store.appCountsAt(now)
	watching := watchingList(now)
	rows := func(list []showCount) [][]string {
		var out [][]string
		for _, s := range list {
			out = append(out, []string{s.Title, fmt.Sprint(s.Count)})
		}
		return out
	}
	var nowRows [][]string
	for _, w := range watching {
		ep := ""
		if w.Show.Episode > 0 {
			ep = fmt.Sprint(w.Show.Episode)
			if w.Show.Episodes > 0 {
				ep += fmt.Sprintf(" / %d", w.Show.Episodes)
			}
		}
		title := w.Show.Title
		if title == "" {
			title = fmt.Sprintf("AniList %d", w.Show.AniListID)
		}
		nowRows = append(nowRows, []string{b.dashName(w.User), title, ep, dashAgo(now.Sub(w.Since))})
	}
	var top [][]string
	for _, t := range c.TopWeek {
		top = append(top, []string{b.dashName(t.User), watchDuration(t.Seconds)})
	}
	return dashJoin(
		dashStats(
			dashStat{"Linked apps", fmt.Sprint(c.Apps)},
			dashStat{"Members", fmt.Sprint(c.Members)},
			dashStat{"Used this week", fmt.Sprint(c.Active)},
			dashStat{"Watching now", fmt.Sprint(len(watching))},
		),
		dashHeading("Watching now"),
		dashTable([]string{"Member", "Show", "Episode", "For"}, nowRows, "Nobody is sharing what they watch right now."),
		dashHeading("Versions"),
		dashTable([]string{"Version", "Apps"}, rows(c.Versions), "No app linked yet."),
		dashHeading("Systems"),
		dashTable([]string{"System", "Apps"}, rows(c.Systems), "No app linked yet."),
		dashHeading("Most watched this week"),
		dashTable([]string{"Member", "Time"}, top, "No watch time this week yet."),
		dashNote("Only members who turned on Share what I watch show under Watching now."),
	)
}
