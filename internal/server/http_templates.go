package server

import (
	"fmt"
	"html/template"
	"strings"
)

type landingPageView struct {
	Title      string
	Badge      string
	Sections   []landingSectionView
	FooterHref string
	FooterText string
}

type landingSectionView struct {
	Title string
	Links []landingLink
}

type browsePageView struct {
	Title   string
	Badge   string
	Headers []string
	Entries []browseEntry
}

var pageTemplates = template.Must(template.New("pages").Funcs(template.FuncMap{
	"cardActionLabel": func(title string) string {
		if strings.EqualFold(strings.TrimSpace(title), "deck") {
			return "down"
		}
		return "open"
	},
	"entrySize": func(entry browseEntry) string {
		if entry.Kind == "file" && entry.Size > 0 {
			return fmt.Sprintf("%d bytes", entry.Size)
		}
		return "-"
	},
	"entryKind": func(entry browseEntry) string {
		if strings.TrimSpace(entry.Kind) == "" {
			return "file"
		}
		return entry.Kind
	},
	"entryMeta": func(entry browseEntry) string {
		if strings.TrimSpace(entry.Meta) == "" {
			return "-"
		}
		return entry.Meta
	},
}).Parse(`
{{define "layout_start"}}<!doctype html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}}</title>
<style>
:root{
  --bg:#f5f7fa;--surface:#fff;--border:#d0d7de;--border-sub:#e8ecf0;
  --text:#16202a;--muted:#57606a;
  --accent:#0969da;--accent-sub:#ddf4ff;--accent-fg:#0550ae;
  --ok:#1a7f37;--ok-sub:rgba(26,127,55,.12);
  --radius:8px;
}
@media(prefers-color-scheme:dark){
  :root{
    --bg:#0d1117;--surface:#161b22;--border:#30363d;--border-sub:#21262d;
    --text:#e6edf3;--muted:#8b949e;
    --accent:#58a6ff;--accent-sub:#1c2d40;--accent-fg:#79c0ff;
    --ok:#3fb950;--ok-sub:rgba(63,185,80,.12);
  }
  .pill-tag{background:#3d2600;color:#e3b341}
  .pill-meta{background:#122316;color:#3fb950}
  tbody tr:hover td{background:rgba(255,255,255,.03)}
}
*,*::before,*::after{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--text);font:14px/1.5 ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}
.shell{max-width:1100px;margin:0 auto;padding:40px 24px 56px;min-height:100vh;display:flex;flex-direction:column}
.hero{display:flex;justify-content:space-between;align-items:baseline;gap:16px;margin-bottom:36px;padding-bottom:24px;border-bottom:1px solid var(--border-sub)}
.eyebrow{margin:0 0 6px;color:var(--muted);font-size:12px;font-weight:600}
.eyebrow a{color:inherit;text-decoration:none}
.eyebrow a:hover{color:var(--accent)}
h1{margin:0;font-size:26px;line-height:1.2;letter-spacing:-.02em;font-weight:700}
.status-badge{display:inline-flex;align-items:center;gap:7px;padding:4px 10px 4px 8px;background:var(--surface);border:1px solid var(--border);border-radius:20px;white-space:nowrap;font-size:12px;font-weight:500;color:var(--muted);flex-shrink:0}
.status-dot{width:8px;height:8px;border-radius:50%;background:var(--ok);box-shadow:0 0 0 3px var(--ok-sub);flex-shrink:0}
.section{margin-bottom:40px}
.section-head{margin-bottom:14px}
.section-head h2{margin:0;font-size:13px;font-weight:600;color:var(--muted);text-transform:uppercase;letter-spacing:.06em}
.cards{display:grid;grid-template-columns:repeat(auto-fill,minmax(200px,1fr));gap:12px}
.card{background:var(--surface);border:1px solid var(--border);border-radius:var(--radius);padding:16px;text-decoration:none;color:inherit;display:block;transition:border-color .12s,box-shadow .12s}
.card:hover{border-color:var(--accent);box-shadow:0 0 0 3px var(--accent-sub)}
.card h2{margin:0 0 5px;font-size:14px;font-weight:600}
.card p{margin:0;font-size:13px;color:var(--muted);line-height:1.4}
.card-action{display:inline-block;margin-top:12px;font-size:12px;color:var(--accent);font-weight:600}
.panel{background:var(--surface);border:1px solid var(--border);border-radius:var(--radius);overflow:hidden}
table{width:100%;border-collapse:collapse}
th,td{padding:10px 16px;border-bottom:1px solid var(--border-sub);text-align:left;vertical-align:middle}
th{font-size:11px;font-weight:700;color:var(--muted);background:var(--bg);text-transform:uppercase;letter-spacing:.06em}
tr:last-child td{border-bottom:0}
tbody tr:hover td{background:rgba(0,0,0,.02)}
.name a{text-decoration:none;color:var(--accent);font-weight:500}
.name a:hover{text-decoration:underline}
.size,.meta{color:var(--muted);font-size:13px}
.pill{display:inline-flex;align-items:center;border-radius:4px;padding:2px 6px;font-size:11px;font-weight:600;letter-spacing:.02em}
.pill-dir,.pill-repo{background:var(--accent-sub);color:var(--accent-fg)}
.pill-file,.pill-link{background:var(--border-sub);color:var(--muted)}
.pill-tag{background:#fff8c5;color:#9a6700}
.pill-meta{background:#dafbe1;color:#116329}
.footer{padding-top:32px;margin-top:auto;color:var(--muted);font-size:13px}
.footer-link{color:var(--muted);text-decoration:none}
.footer-link:hover{color:var(--accent)}
@media(max-width:640px){
  .shell{padding:24px 16px 40px}
  .hero{flex-direction:column;align-items:flex-start;gap:12px;margin-bottom:28px}
  h1{font-size:22px}
  th:nth-child(3),td:nth-child(3),th:nth-child(4),td:nth-child(4){display:none}
}
</style>
</head>
<body>
<div class="shell">
{{end}}

{{define "layout_end"}}</div></body></html>{{end}}

{{define "hero"}}
<section class="hero">
  <div>
    <p class="eyebrow"><a href="/">deck server</a></p>
    <h1>{{.Title}}</h1>
  </div>
  <div class="status-badge"><span class="status-dot"></span>{{.Badge}}</div>
</section>
{{end}}

{{define "landing"}}{{template "layout_start" .}}{{template "landingBody" .}}{{template "layout_end" .}}{{end}}

{{define "landingBody"}}
{{template "hero" .}}
{{range .Sections}}
<section class="section">
  <div class="section-head"><h2>{{.Title}}</h2></div>
  <div class="cards">
    {{range .Links}}
    <a class="card" href="{{.Href}}">
      <h2>{{.Title}}</h2>
      <p>{{.Desc}}</p>
      <span class="card-action">{{cardActionLabel .Title}} →</span>
    </a>
    {{end}}
  </div>
</section>
{{end}}
<footer class="footer"><a class="footer-link" href="{{.FooterHref}}">{{.FooterText}}</a></footer>
{{end}}

{{define "browse"}}{{template "layout_start" .}}{{template "browseBody" .}}{{template "layout_end" .}}{{end}}

{{define "browseBody"}}
{{template "hero" .}}
<div class="panel">
  <table>
    <thead><tr>{{range .Headers}}<th>{{.}}</th>{{end}}</tr></thead>
    <tbody>
      {{range .Entries}}
      <tr>
        <td class="name">{{if .Href}}<a href="{{.Href}}">{{.Name}}</a>{{else}}{{.Name}}{{end}}</td>
        <td><span class="pill pill-{{entryKind .}}">{{entryKind .}}</span></td>
        <td class="size">{{entrySize .}}</td>
        <td class="meta">{{entryMeta .}}</td>
      </tr>
      {{end}}
    </tbody>
  </table>
</div>
{{end}}
`))

func renderLandingPage(view landingPageView) (string, error) {
	var b strings.Builder
	if err := pageTemplates.ExecuteTemplate(&b, "landing", view); err != nil {
		return "", err
	}
	return b.String(), nil
}

func renderBrowsePage(title string, entries []browseEntry) (string, error) {
	view := browsePageView{
		Title:   title,
		Badge:   fmt.Sprintf("%d entries", len(entries)),
		Headers: []string{"Name", "Type", "Size", "Details"},
		Entries: entries,
	}
	var b strings.Builder
	if err := pageTemplates.ExecuteTemplate(&b, "browse", view); err != nil {
		return "", err
	}
	return b.String(), nil
}
