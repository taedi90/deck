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
}).Parse(`{{define "layout_start"}}<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>{{.Title}}</title><style>
body{margin:0;background:#f6f8fb;color:#16202a;font:14px/1.5 ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}
.shell{max-width:1120px;margin:0 auto;padding:32px 20px 48px}
.hero{display:flex;justify-content:space-between;gap:24px;align-items:flex-start;margin-bottom:24px}.eyebrow{margin:0 0 8px;color:#57606a;text-transform:uppercase;letter-spacing:.08em;font-size:12px;font-weight:700}.eyebrow a{color:inherit;text-decoration:none}
h1{margin:0;font-size:32px;line-height:1.15}
.status-card{display:inline-flex;align-items:center;gap:10px;padding:2px 0;white-space:nowrap;font-weight:600;color:#57606a}.status-dot{width:10px;height:10px;border-radius:999px;background:#1a7f37;box-shadow:0 0 0 4px rgba(26,127,55,.12)}
.cards{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:16px;margin-bottom:24px}.cards-fixed{grid-template-columns:repeat(auto-fill,minmax(220px,220px));justify-content:start}.card,.panel{background:#fff;border:1px solid #d0d7de;border-radius:16px;box-shadow:0 1px 2px rgba(22,32,42,.04)}
.section{margin-bottom:28px}.section-head{margin:0 0 12px}.section-head h2{margin:0;font-size:20px}
.card{padding:18px;text-decoration:none;color:inherit}.card h2{margin:0 0 8px;font-size:18px}.card p{margin:0;color:#57606a}.card-link{display:inline-block;margin-top:14px;color:#0969da;font-weight:600}
.footer{padding-top:20px;margin-top:auto;color:#57606a;text-align:center}.footer-link{color:#0969da;text-decoration:none;font-weight:600}.panel-inline{padding:16px 18px}.panel{overflow:hidden}table{width:100%;border-collapse:collapse}th,td{padding:14px 16px;border-bottom:1px solid #d8dee4;text-align:left;vertical-align:top}th{font-size:12px;letter-spacing:.06em;text-transform:uppercase;color:#57606a;background:#f6f8fb}tr:last-child td{border-bottom:0}.name a{text-decoration:none;color:#0969da;font-weight:600}.size,.meta{color:#57606a}
.pill{display:inline-flex;align-items:center;border-radius:999px;padding:4px 10px;font-size:12px;font-weight:700;text-transform:uppercase;letter-spacing:.04em}.pill-dir,.pill-repo{background:#ddf4ff;color:#0550ae}.pill-file,.pill-link{background:#f6f8fa;color:#57606a}.pill-tag{background:#fff8c5;color:#9a6700}.pill-meta{background:#dafbe1;color:#116329}
@media (max-width:720px){.hero{flex-direction:column}.shell{padding:20px 14px 32px}th:nth-child(3),td:nth-child(3),th:nth-child(4),td:nth-child(4){display:none}}
</style></head><body><div class="shell" style="min-height:100vh;display:flex;flex-direction:column;box-sizing:border-box;">{{end}}
{{define "layout_end"}}</div></body></html>{{end}}
{{define "hero"}}<section class="hero"><div><p class="eyebrow"><a href="/">deck server</a></p><h1>{{.Title}}</h1></div><div class="status-card"><span class="status-dot"></span>{{.Badge}}</div></section>{{end}}
{{define "landing"}}{{template "layout_start" .}}{{template "landingBody" .}}{{template "layout_end" .}}{{end}}
{{define "landingBody"}}{{template "hero" .}}{{range .Sections}}<section class="section"><div class="section-head"><h2>{{.Title}}</h2></div><section class="cards cards-fixed">{{range .Links}}<a class="card" href="{{.Href}}"><h2>{{.Title}}</h2><p>{{.Desc}}</p><span class="card-link">{{cardActionLabel .Title}}</span></a>{{end}}</section></section>{{end}}<footer class="footer"><a class="footer-link" href="{{.FooterHref}}">{{.FooterText}}</a></footer>{{end}}
{{define "browse"}}{{template "layout_start" .}}{{template "browseBody" .}}{{template "layout_end" .}}{{end}}
{{define "browseBody"}}{{template "hero" .}}<div class="panel"><table><thead><tr>{{range .Headers}}<th>{{.}}</th>{{end}}</tr></thead><tbody>{{range .Entries}}<tr><td class="name">{{if .Href}}<a href="{{.Href}}">{{.Name}}</a>{{else}}{{.Name}}{{end}}</td><td><span class="pill pill-{{entryKind .}}">{{entryKind .}}</span></td><td class="size">{{entrySize .}}</td><td class="meta">{{entryMeta .}}</td></tr>{{end}}</tbody></table></div>{{end}}`))

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
