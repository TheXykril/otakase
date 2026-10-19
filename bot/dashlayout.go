package main

import (
	_ "embed"
	"fmt"
	"html/template"
	"net/http"
	"strings"
)

// The dashboard's look and menu, matching the games Activity: flat dark
// cards, Inter served by the bot, thin line icons and faint お任せ lines
// behind everything. Pages sit in five groups. Wide screens get a side
// menu with every page; phones get the groups as a bar at the bottom and
// the current group's pages as tabs at the top.

//go:embed assets/dash.js
var dashJS []byte

// dashGroup is one menu group.
type dashGroup struct{ Key, Name, JP, Icon string }

var dashGroups = []dashGroup{
	{"status", "Status", "ステータス", "pulse"},
	{"posts", "Posts", "ポスト", "send"},
	{"members", "Members", "メンバー", "users"},
	{"events", "Events", "イベント", "calendar"},
	{"safety", "Safety", "セーフティ", "shield"},
}

// dashPageInfo places a page in a group, with its icon and the Japanese
// word on its page tag.
type dashPageInfo struct{ Group, Icon, JP string }

var dashPages = map[string]dashPageInfo{
	"overview":      {"status", "gauge", "がいよう"},
	"settings":      {"status", "sliders", "せってい"},
	"bot":           {"status", "cpu", "ボット"},
	"log":           {"status", "file", "ログ"},
	"post":          {"posts", "send", "とうこう"},
	"texts":         {"posts", "message", "テキスト"},
	"airing":        {"posts", "tv", "ほうそう"},
	"support":       {"members", "help", "サポート"},
	"suggestions":   {"members", "bulb", "ていあん"},
	"follows":       {"members", "bell", "フォロー"},
	"anilist":       {"members", "list", "アニリスト"},
	"linked-apps":   {"members", "link", "アプリ"},
	"games":         {"events", "dice", "ゲーム"},
	"live-games":    {"events", "play", "ライブ"},
	"riddles":       {"events", "riddle", "なぞなぞ"},
	"watch-parties": {"events", "party", "みんなで"},
	"rooms":         {"events", "mic", "ボイス"},
	"safety":        {"safety", "shield", "あんぜん"},
	"moderation":    {"safety", "flag", "けいこく"},
}

// dashInfo is a page's place in the menu; pages nobody placed go under
// Status with a plain icon.
func dashInfo(slug string) dashPageInfo {
	if p, ok := dashPages[slug]; ok {
		return p
	}
	return dashPageInfo{"status", "dot", "ページ"}
}

// Chars is the page tag's word one character at a time, so each sits
// centred in the stamp whatever font draws it.
func (p dashPageInfo) Chars() []string { return strings.Split(p.JP, "") }

// Line icons, 24×24, drawn with the current colour.
var dashIcons = map[string]string{
	"pulse":    `<path d="M3 12h4l3-8 4 16 3-8h4"/>`,
	"send":     `<path d="M21 3L10 14M21 3l-7 18-4-7-7-4z"/>`,
	"users":    `<circle cx="9" cy="8" r="3.5"/><path d="M2.5 20a6.5 6.5 0 0 1 13 0M16 4.5a3.5 3.5 0 0 1 0 7M18 14a6 6 0 0 1 3.5 6"/>`,
	"calendar": `<rect x="3" y="4" width="18" height="17" rx="2"/><path d="M3 9h18M8 2v4M16 2v4"/>`,
	"shield":   `<path d="M12 3l8 3v6c0 4.5-3.4 8-8 9-4.6-1-8-4.5-8-9V6z"/>`,
	"gauge":    `<path d="M4 18a9 9 0 1 1 16 0"/><path d="M12 14l4-5"/><circle cx="12" cy="14" r="1.2"/>`,
	"sliders":  `<path d="M4 6h10M18 6h2M4 12h4M12 12h8M4 18h12M20 18h0"/><circle cx="16" cy="6" r="2"/><circle cx="10" cy="12" r="2"/><circle cx="18" cy="18" r="2"/>`,
	"cpu":      `<rect x="6" y="6" width="12" height="12" rx="2"/><path d="M10 10h4v4h-4zM9 2v4M15 2v4M9 18v4M15 18v4M2 9h4M2 15h4M18 9h4M18 15h4"/>`,
	"file":     `<path d="M14 3H6a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V8z"/><path d="M14 3v5h5M8 13h8M8 17h6"/>`,
	"message":  `<path d="M4 4h16v12H9l-5 4z"/><path d="M8 9h8M8 12h5"/>`,
	"tv":       `<rect x="3" y="6" width="18" height="13" rx="2"/><path d="M8 2l4 4 4-4"/>`,
	"help":     `<circle cx="12" cy="12" r="9"/><path d="M9.5 9.5a2.5 2.5 0 1 1 3.5 2.3c-.6.3-1 .8-1 1.5v.7M12 17h.01"/>`,
	"bulb":     `<path d="M9 18h6M10 21h4M12 3a6 6 0 0 0-3.5 10.9c.6.5 1 1.2 1 2.1h5c0-.9.4-1.6 1-2.1A6 6 0 0 0 12 3z"/>`,
	"bell":     `<path d="M6 16V11a6 6 0 0 1 12 0v5l2 2H4z"/><path d="M10 21h4"/>`,
	"list":     `<path d="M9 6h12M9 12h12M9 18h12M4 6h.01M4 12h.01M4 18h.01"/>`,
	"link":     `<path d="M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1"/><path d="M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1-1"/>`,
	"dice":     `<rect x="4" y="4" width="16" height="16" rx="3"/><path d="M8.5 8.5h.01M15.5 8.5h.01M12 12h.01M8.5 15.5h.01M15.5 15.5h.01"/>`,
	"party":    `<circle cx="7" cy="8" r="3"/><circle cx="17" cy="8" r="3"/><path d="M2 20a5 5 0 0 1 10 0M12 20a5 5 0 0 1 10 0"/>`,
	"mic":      `<rect x="9" y="3" width="6" height="11" rx="3"/><path d="M5 11a7 7 0 0 0 14 0M12 18v3"/>`,
	"flag":     `<path d="M5 21V4M5 4h11l-2 4 2 4H5"/>`,
	"dot":      `<circle cx="12" cy="12" r="3"/>`,
	"play":     `<rect x="3" y="5" width="18" height="14" rx="3"/><path d="M10 9.5v5l4.5-2.5z"/>`,
	"riddle":   `<path d="M21 12a8 8 0 0 1-11.6 7.1L4 20l1-4.6A8 8 0 1 1 21 12z"/><path d="M10 10a2 2 0 1 1 2.8 1.8c-.5.3-.8.7-.8 1.2M12 16h.01"/>`,
	"chev":     `<path d="M6 9l6 6 6-6"/>`,
}

func dashIcon(name string) template.HTML {
	return template.HTML(`<svg class="i" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">` +
		dashIcons[name] + `</svg>`)
}

type dashNavGroup struct {
	dashGroup
	Icon  template.HTML
	First string // the group's first page, where its tab goes
	On    bool
	Pages []dashNavPage
}

type dashNavPage struct {
	Slug, Name, No string
	Icon           template.HTML
	On             bool
}

// dashNav is the menu with the current page marked. Pages are numbered in
// menu order, the number shown on each page's tag.
func dashNav(current string) []dashNavGroup {
	var out []dashNavGroup
	n := 0
	for _, g := range dashGroups {
		ng := dashNavGroup{dashGroup: g, Icon: dashIcon(g.Icon)}
		for _, s := range dashSections {
			info := dashInfo(s.Slug)
			if info.Group != g.Key {
				continue
			}
			n++
			p := dashNavPage{Slug: s.Slug, Name: s.Name, No: fmt.Sprintf("%02d", n), Icon: dashIcon(info.Icon), On: s.Slug == current}
			if ng.First == "" {
				ng.First = s.Slug
			}
			ng.On = ng.On || p.On
			ng.Pages = append(ng.Pages, p)
		}
		if len(ng.Pages) > 0 {
			out = append(out, ng)
		}
	}
	return out
}

// dashStatic serves the page's script and fonts.
func dashStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}
	var data []byte
	var kind string
	switch name, ok := strings.CutPrefix(r.URL.Path, "/fonts/"); {
	case r.URL.Path == "/dash.js":
		data, kind = dashJS, "text/javascript; charset=utf-8"
	case ok && strings.HasPrefix(name, "Inter-") && strings.HasSuffix(name, ".otf") && !strings.Contains(name, "/"):
		var err error
		if data, err = cardFonts.ReadFile("assets/fonts/" + name); err != nil {
			http.NotFound(w, r)
			return
		}
		kind = "font/otf"
	default:
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", kind)
	h.Set("Cache-Control", "max-age=86400")
	h.Set("X-Content-Type-Options", "nosniff")
	w.Write(data)
}

// PageNo is the current page's number in the menu.
func (p dashPage) PageNo() string {
	for _, g := range p.Groups {
		for _, pg := range g.Pages {
			if pg.On {
				return pg.No
			}
		}
	}
	return ""
}

// Chevron is the arrow on the phone page picker.
func (dashPage) Chevron() template.HTML {
	return template.HTML(strings.Replace(string(dashIcon("chev")), `class="i"`, `class="i chev"`, 1))
}

// Group is the current page's menu group.
func (p dashPage) Group() dashNavGroup {
	for _, g := range p.Groups {
		if g.On {
			return g
		}
	}
	return dashNavGroup{}
}

var dashTmpl = template.Must(template.New("dash").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="color-scheme" content="dark">
{{if not .Current.Controls}}<meta http-equiv="refresh" content="60">{{end}}
{{if .Base}}<base href="{{.Base}}">{{end}}
<title>Otakase Bot · {{.Current.Name}}</title>
<style>
@font-face{font-family:"Inter";src:url("fonts/Inter-Medium.otf") format("opentype");font-weight:400 500;font-display:swap}
@font-face{font-family:"Inter";src:url("fonts/Inter-SemiBold.otf") format("opentype");font-weight:600 700;font-display:swap}
@font-face{font-family:"Inter";src:url("fonts/Inter-ExtraBold.otf") format("opentype");font-weight:800 900;font-display:swap}
:root{--bg:#111113;--tile:#19191c;--tile2:#202024;--line:#2a2a30;--text:#f4efe6;--soft:#cfcac2;--muted:#8f8b85;--shu:#d2492f;--accent:#e8664b;--yes:#3f8f5a;--side:236px;--jpf:"Hiragino Sans","Yu Gothic UI","Meiryo","Noto Sans JP","Noto Sans CJK JP",sans-serif}
*{box-sizing:border-box}
html{background:var(--bg);scrollbar-width:none}
::-webkit-scrollbar{display:none}
body{margin:0;color:var(--text);font:500 15px/1.5 "Inter",system-ui,-apple-system,"Segoe UI",sans-serif;min-height:100vh;overflow-x:hidden}
body::before,body::after{content:"";position:fixed;top:0;left:0;height:3px;background:var(--shu);z-index:30}
body::before{right:0;transition:opacity .3s}
body::after{width:0}
html.long body::before{opacity:.5}
html.long body::after{width:var(--scrolled,0%)}
a{color:inherit}
svg.i{width:1.15em;height:1.15em;vertical-align:-.2em;flex:none}
.jp{font-family:var(--jpf)}
.bgtext{position:fixed;left:50%;top:50%;width:260vmax;height:260vmax;margin:-130vmax 0 0 -130vmax;transform:rotate(-56deg);z-index:-1;pointer-events:none;overflow:hidden;display:flex;flex-direction:column;justify-content:center;gap:18px;opacity:.015}
.bgtext div{white-space:nowrap;font:800 34px/1 var(--jpf);letter-spacing:.12em;color:var(--text);animation:drift var(--t,90s) linear infinite;will-change:transform}
.bgtext div:nth-child(even){animation-direction:reverse}
@keyframes drift{from{transform:translateX(0)}to{transform:translateX(-50%)}}

/* side menu */
.side{position:fixed;top:0;bottom:0;left:0;width:var(--side);padding:22px 12px 16px;border-right:1px solid var(--line);background:var(--bg);overflow-y:auto;scrollbar-width:none;display:flex;flex-direction:column;z-index:20}
.brand{display:flex;align-items:baseline;gap:8px;padding:0 10px 18px;text-decoration:none}
.brand b{font-size:19px;font-weight:800;letter-spacing:-.02em}
.brand b span{color:var(--accent)}
.brand small{color:var(--muted);font-size:12px}
.grp{margin-bottom:14px}
.gh{display:flex;align-items:center;justify-content:space-between;padding:0 10px 4px;font-size:11px;font-weight:700;text-transform:uppercase;letter-spacing:.1em;color:var(--muted)}
.gh .jp{font-size:10px;letter-spacing:.08em;color:var(--shu);text-transform:none;font-weight:600}
.nv{display:flex;align-items:center;gap:10px;padding:7px 10px;border-radius:6px;text-decoration:none;color:var(--soft);font-size:14px;transition:background .15s,color .15s}
.nv svg.i{color:var(--muted);transition:color .15s}
.nv em{margin-left:auto;font-style:normal;font-size:11px;font-weight:700;color:var(--muted);opacity:0;font-variant-numeric:tabular-nums;transition:opacity .15s}
.nv:hover{background:var(--tile);color:var(--text)}
.nv:hover em{opacity:1}
.nv.on{background:var(--tile2);color:var(--text);box-shadow:inset 3px 0 0 var(--shu)}
.nv.on svg.i,.nv.on em{color:var(--accent);opacity:1}
.side .foot{margin-top:auto;padding:12px 10px 0;color:var(--muted);font-size:12px}

/* phone menu */
.top,.sub,.tabbar{display:none}
.top{align-items:center;justify-content:space-between;gap:10px;padding:16px 16px 4px}
.top .brand{padding:0}
.top small.up{color:var(--muted);font-size:12px}
.sub{position:relative;margin:10px 16px 0;z-index:15}
.sub summary{display:flex;align-items:center;gap:10px;list-style:none;cursor:pointer;padding:11px 14px;border-radius:8px;background:var(--tile);border:1px solid var(--line);font-weight:600;font-size:15px}
.sub summary::-webkit-details-marker{display:none}
.sub summary svg.i{color:var(--accent)}
.sub summary small{margin-left:auto;color:var(--muted);font-size:12px;font-weight:600;font-variant-numeric:tabular-nums}
.sub summary .chev{color:var(--muted);transition:transform .2s}
.sub[open] summary{border-color:var(--accent)}
.sub[open] summary .chev{transform:rotate(180deg)}
.sub .menu{position:absolute;left:0;right:0;top:calc(100% + 4px);background:var(--tile2);border:1px solid var(--line);border-radius:8px;padding:4px;box-shadow:0 12px 30px rgba(0,0,0,.5)}
.sub .menu a{display:flex;align-items:center;gap:10px;padding:11px 12px;border-radius:6px;text-decoration:none;color:var(--soft);font-size:15px}
.sub .menu a svg.i{color:var(--muted)}
.sub .menu a em{margin-left:auto;font-style:normal;font-size:12px;font-weight:700;color:var(--muted);font-variant-numeric:tabular-nums}
.sub .menu a.on{background:var(--bg);color:var(--text);box-shadow:inset 3px 0 0 var(--shu)}
.sub .menu a.on svg.i,.sub .menu a.on em{color:var(--accent)}
.tabbar{position:fixed;left:0;right:0;bottom:0;z-index:20;background:rgba(17,17,19,.96);border-top:1px solid var(--line);padding:0 4px env(safe-area-inset-bottom)}
.tabbar a{flex:1;display:flex;flex-direction:column;align-items:center;gap:3px;padding:9px 2px 8px;text-decoration:none;color:var(--muted);font-size:11px;font-weight:600;position:relative;min-width:0}
.tabbar a svg.i{width:22px;height:22px}
.tabbar a.on{color:var(--text)}
.tabbar a.on svg.i{color:var(--accent)}
.tabbar a.on::before{content:"";position:absolute;top:-1px;left:28%;right:28%;height:2px;background:var(--shu)}

/* page */
.wrap{margin-left:var(--side);position:relative;z-index:0}
main{max-width:940px;margin:0 auto;padding:28px 28px 24px}
main>*{animation:rise .3s cubic-bezier(.2,.8,.2,1) both}
@keyframes rise{from{opacity:0;transform:translateY(6px)}}
.ph{display:flex;align-items:flex-end;gap:14px;padding:0 0 16px;margin-bottom:20px;border-bottom:1px solid var(--line)}
.ph>div{flex:1;min-width:0}
.ph .no{font-weight:800;font-size:12px;letter-spacing:.1em;color:var(--accent);text-transform:uppercase}
.ph .no span{color:var(--muted);margin-left:6px}
.ph h1{margin:2px 0 0;font-size:30px;font-weight:800;letter-spacing:-.02em;line-height:1.15}
.ph .tag{align-self:flex-start;flex:none;display:flex;flex-direction:column;align-items:center;gap:3px;width:22px;font:600 12px/1 var(--jpf);color:#fff;background:var(--shu);padding:7px 0;border-radius:3px}
.ph .tag i{display:block;width:1em;height:1em;text-align:center;font-style:normal}
.ph .up{color:var(--muted);font-size:12px;white-space:nowrap}
h2{display:flex;align-items:center;gap:8px;font-size:12px;font-weight:700;text-transform:uppercase;letter-spacing:.1em;color:var(--muted);margin:26px 0 8px}
h2::before{content:"";width:10px;height:2px;background:var(--shu)}
p{margin:10px 0}
.note{color:var(--muted);font-size:14px}
td .note{font-size:13px}
.stats{display:grid;grid-template-columns:repeat(auto-fill,minmax(130px,1fr));gap:10px}
.stat{background:var(--tile);border:1px solid var(--line);border-top:3px solid var(--shu);border-radius:8px;padding:12px 14px}
.stat .v{font-size:22px;font-weight:800;letter-spacing:-.01em;font-variant-numeric:tabular-nums;overflow-wrap:anywhere}
.stat .l{color:var(--muted);font-size:11px;font-weight:600;text-transform:uppercase;letter-spacing:.06em;margin-top:2px}
.tw{overflow-x:auto;margin-top:10px;background:var(--tile);border:1px solid var(--line);border-radius:8px;scrollbar-width:none}
table{border-collapse:collapse;width:100%}
th,td{text-align:left;padding:10px 14px;border-bottom:1px solid var(--line);vertical-align:top}
th{color:var(--muted);font-weight:700;font-size:11px;text-transform:uppercase;letter-spacing:.08em;white-space:nowrap}
tbody tr:hover{background:var(--tile2)}
tr:last-child td{border-bottom:0}
.flash{background:var(--tile);border:1px solid var(--line);border-left:3px solid var(--yes);border-radius:8px;padding:10px 14px;margin-bottom:16px}
.flash.bad{border-left-color:var(--shu)}
.card{background:var(--tile);border:1px solid var(--line);border-radius:8px;padding:14px 16px;margin-top:10px}
.card .now{color:var(--accent);font-size:13px;font-weight:600;margin-left:6px}
.card .note{font-size:13px;margin-top:2px}
.ctls{display:flex;flex-wrap:wrap;gap:8px;margin-top:10px}
td .ctls{margin-top:0}
.ctl{display:flex;flex-wrap:wrap;gap:6px;align-items:center;margin:0;max-width:100%}
.ctl select,.ctl input,.ctl textarea{font:inherit;font-size:14px;color:var(--text);background:var(--bg);border:1px solid var(--line);border-radius:6px;padding:7px 9px;min-width:0;outline:none;transition:border-color .15s}
.ctl select:focus,.ctl input:focus,.ctl textarea:focus{border-color:var(--accent)}
.ctl input[type=number]{width:7em}
.ctl input[type=text]{width:16em;max-width:100%}
.ctl input[type=file]{max-width:100%;font-size:13px}
.ctl input[type=checkbox]{accent-color:var(--shu)}
.ctl.wide{flex-direction:column;align-items:stretch;width:100%}
.ctl textarea{width:100%;resize:vertical}
.ctl .sure{color:var(--muted);font-size:13px;display:flex;gap:5px;align-items:center}
.ctl.wide button{align-self:flex-start}
.ctl button,a.btn{display:inline-flex;align-items:center;gap:6px;font:inherit;font-size:14px;font-weight:600;cursor:pointer;border-radius:6px;padding:7px 16px;border:1px solid var(--shu);background:var(--shu);color:#fff;text-decoration:none;transition:background .15s,transform .1s}
.ctl button:hover,a.btn:hover{background:var(--accent)}
.ctl button:active,a.btn:active{transform:scale(.97)}
.ctl button.quiet,a.btn{background:var(--tile2);color:var(--soft);border-color:var(--line)}
.ctl button.quiet:hover,a.btn:hover{border-color:var(--muted);background:var(--tile2)}
pre.log{background:var(--tile);border:1px solid var(--line);border-radius:8px;padding:12px 14px;overflow-x:auto;font:12px/1.5 ui-monospace,"SF Mono",Menlo,Consolas,monospace;white-space:pre-wrap;overflow-wrap:anywhere;color:var(--soft)}
.cols{display:grid;grid-template-columns:1fr 1fr;gap:0 18px}
.cols>section{min-width:0}
ol.rank{list-style:none;padding:0;margin:10px 0 0}
ol.rank li{position:relative;overflow:hidden;display:flex;align-items:center;gap:12px;background:var(--tile);border:1px solid var(--line);border-radius:8px;padding:9px 14px;margin-bottom:6px;animation:rise .3s cubic-bezier(.2,.8,.2,1) both}
ol.rank li::before{content:"";position:absolute;inset:0 auto 0 0;width:var(--w);background:rgba(210,73,47,.13);transform-origin:left;animation:grow .8s cubic-bezier(.2,.8,.2,1) both}
@keyframes grow{from{transform:scaleX(0)}}
ol.rank li>*{position:relative}
ol.rank .r{width:22px;text-align:center;font-weight:800;color:var(--muted);font-variant-numeric:tabular-nums}
ol.rank li:first-child .r{color:#d9a441}
ol.rank .who{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
ol.rank .who small{color:var(--muted);font-size:12px;margin-left:8px}
ol.rank b{font-weight:800;font-variant-numeric:tabular-nums}
.podium{display:grid;grid-template-columns:repeat(3,1fr);align-items:end;gap:8px;max-width:460px;margin:14px auto 4px}
.podium>div{text-align:center;min-width:0;animation:rise .4s cubic-bezier(.2,.8,.2,1) both}
.podium .m{display:inline-grid;place-items:center;width:28px;height:28px;border-radius:50%;font-weight:800;font-size:13px;color:#111;margin-bottom:6px;background:#b9b6b0}
.podium .name{font-weight:600;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.podium .step{margin-top:6px;border-radius:6px 6px 2px 2px;padding:8px 4px;font-weight:800;font-size:18px;background:var(--tile);border:1px solid var(--line);border-top:3px solid #b9b6b0;height:64px;font-variant-numeric:tabular-nums}
.podium .p1 .m{background:#d9a441}.podium .p1 .step{height:88px;border-top-color:#d9a441}
.podium .p3 .m{background:#b8774a}.podium .p3 .step{height:50px;border-top-color:#b8774a}
footer{color:var(--muted);font-size:12px;text-align:center;padding:8px 16px 24px}
@media (max-width:899px){
 .side{display:none}
 .wrap{margin-left:0}
 .top,.tabbar{display:flex}
 .sub{display:block}
 main{padding:18px 16px calc(88px + env(safe-area-inset-bottom))}
 .ph{margin-bottom:16px}
 .ph h1{font-size:25px}
 .ph .tag{flex-direction:row;width:auto;font-size:11px;gap:2px;padding:5px 8px}
 .ph .up{display:none}
 .stats{grid-template-columns:repeat(auto-fill,minmax(130px,1fr));gap:8px}
 .stat{padding:10px 12px}
 .stat .v{font-size:19px}
 footer{display:none}
 .cols{grid-template-columns:1fr}
}
@media (prefers-reduced-motion:reduce){*,*::before,*::after{animation:none!important;transition:none!important}}
</style>
</head>
<body>
<div class="bgtext" id="bgtext" aria-hidden="true"></div>
<aside class="side" aria-label="Pages">
<a class="brand" href="?s=overview"><b>Otakase <span>Bot</span></b><small>v{{.Version}}</small></a>
<nav>{{range .Groups}}<div class="grp"><div class="gh"><span>{{.Name}}</span><span class="jp">{{.JP}}</span></div>
{{range .Pages}}<a class="nv{{if .On}} on{{end}}" href="?s={{.Slug}}"{{if .On}} aria-current="page"{{end}}>{{.Icon}}{{.Name}}<em>{{.No}}</em></a>
{{end}}</div>
{{end}}</nav>
<div class="foot">Updated {{.Updated}}{{if not .Current.Controls}} · refreshes every minute{{end}}</div>
</aside>
<div class="wrap">
<header class="top"><a class="brand" href="?s=overview"><b>Otakase <span>Bot</span></b><small>v{{.Version}}</small></a><small class="up">{{.Updated}}</small></header>
{{with .Group}}{{if gt (len .Pages) 1}}<details class="sub"><summary aria-label="{{.Name}} pages, pick a page">{{range .Pages}}{{if .On}}{{.Icon}}<span>{{.Name}}</span>{{end}}{{end}}<small>{{len .Pages}} pages</small>{{$.Chevron}}</summary>
<nav class="menu" aria-label="{{.Name}} pages">{{range .Pages}}<a href="?s={{.Slug}}"{{if .On}} class="on" aria-current="page"{{end}}>{{.Icon}}{{.Name}}<em>{{.No}}</em></a>{{end}}</nav></details>{{end}}{{end}}
<main>
<div class="ph"><div><div class="no">{{.PageNo}}<span>{{.Group.Name}}</span></div><h1>{{.Current.Name}}</h1></div><span class="up">Updated {{.Updated}}</span><span class="tag" aria-hidden="true">{{range .Info.Chars}}<i>{{.}}</i>{{end}}</span></div>
{{if .Flash}}<div class="flash{{if .FlashBad}} bad{{end}}" role="status">{{.Flash}}</div>{{end}}
{{.Body}}
</main>
<footer>Otakase Bot v{{.Version}}</footer>
</div>
<nav class="tabbar" aria-label="Groups">{{range .Groups}}<a href="?s={{.First}}"{{if .On}} class="on"{{end}}>{{.Icon}}<span>{{.Name}}</span></a>{{end}}</nav>
<script src="dash.js"></script>
</body>
</html>
`))
