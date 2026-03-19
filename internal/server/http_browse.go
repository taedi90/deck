package server

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/taedi90/deck/internal/deckignore"
	"github.com/taedi90/deck/internal/fsutil"
)

type browseEntry struct {
	Name string
	Href string
	Kind string
	Size int64
	Meta string
}

type landingLink struct {
	Title string
	Href  string
	Desc  string
}

func (h *serverHandler) handleLanding(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	fileLinks := make([]landingLink, 0, 4)
	if h.hasBrowseEntries("workflows") {
		fileLinks = append(fileLinks, landingLink{Title: "workflows", Href: "/browse/workflows/", Desc: "Scenarios and components"})
	}
	if h.hasBrowseEntries("files") {
		fileLinks = append(fileLinks, landingLink{Title: "files", Href: "/browse/files/", Desc: "Prepared files"})
	}
	if h.hasBrowseEntries("packages") {
		fileLinks = append(fileLinks, landingLink{Title: "packages", Href: "/browse/packages/", Desc: "Offline package repos"})
	}
	if h.hasDeckBinary() {
		fileLinks = append(fileLinks, landingLink{Title: "deck", Href: "/deck", Desc: "Current binary"})
	}
	imageLinks := make([]landingLink, 0, 1)
	if h.hasImageEntries() {
		imageLinks = append(imageLinks, landingLink{Title: "images", Href: "/browse/images/", Desc: "Image repos and tags"})
	}
	body, err := renderLandingPage(landingPageView{
		Title: "deck server",
		Badge: "server health: ok",
		Sections: func() []landingSectionView {
			out := make([]landingSectionView, 0, 2)
			if len(fileLinks) > 0 {
				out = append(out, landingSectionView{Title: "Files", Links: fileLinks})
			}
			if len(imageLinks) > 0 {
				out = append(out, landingSectionView{Title: "Images", Links: imageLinks})
			}
			return out
		}(),
		FooterHref: "/v2/",
		FooterText: "registry api",
	})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusOK)
	writeResponseBody(w, []byte(body))
}

func (h *serverHandler) handleBrowse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path == "/browse/images" || r.URL.Path == "/browse/images/" || strings.HasPrefix(r.URL.Path, "/browse/images/") {
		h.handleBrowseImages(w, r)
		return
	}
	for _, category := range []string{"files", "packages", "workflows"} {
		prefix := "/browse/" + category
		if r.URL.Path == prefix || r.URL.Path == prefix+"/" || strings.HasPrefix(r.URL.Path, prefix+"/") {
			h.handleBrowseDir(w, r, category)
			return
		}
	}
	http.NotFound(w, r)
}

func (h *serverHandler) handleBrowseDir(w http.ResponseWriter, r *http.Request, category string) {
	relPath := strings.TrimPrefix(r.URL.Path, "/browse/"+category)
	relPath = strings.Trim(strings.TrimPrefix(relPath, "/"), "/")
	entries, title, err := h.listBrowseEntries(category, relPath)
	if err != nil {
		status := http.StatusInternalServerError
		if os.IsNotExist(err) {
			status = http.StatusNotFound
		}
		w.WriteHeader(status)
		return
	}
	body, err := renderBrowsePage(title, entries)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusOK)
	writeResponseBody(w, []byte(body))
}

func (h *serverHandler) listBrowseEntries(category, relPath string) ([]browseEntry, string, error) {
	ignore, err := deckignore.Load(h.rootAbs)
	if err != nil {
		return nil, "", err
	}
	root, err := fsutil.NewRoot(h.rootAbs)
	if err != nil {
		return nil, "", err
	}
	baseDir := category
	if category == "files" || category == "packages" {
		baseDir = filepath.ToSlash(filepath.Join(serverOutputsDir, category))
	}
	info, _, err := root.Stat(filepath.FromSlash(baseDir), filepath.FromSlash(relPath))
	if err != nil {
		return nil, "", err
	}
	if !info.IsDir() {
		return nil, "", os.ErrNotExist
	}
	list, _, err := root.ReadDir(filepath.FromSlash(baseDir), filepath.FromSlash(relPath))
	if err != nil {
		return nil, "", err
	}
	entries := make([]browseEntry, 0, len(list)+1)
	baseHref := "/browse/" + category + "/"
	homeHref := "/"
	if relPath != "" {
		parent := path.Dir(strings.Trim(relPath, "/"))
		if parent == "." {
			parent = ""
		}
		parentHref := baseHref
		if parent != "" {
			parentHref += parent + "/"
		}
		entries = append(entries, browseEntry{Name: "..", Href: parentHref, Kind: "dir"})
	} else {
		entries = append(entries, browseEntry{Name: "..", Href: homeHref, Kind: "dir"})
	}
	for _, entry := range list {
		name := entry.Name()
		childRel := filepath.ToSlash(path.Join(relPath, name))
		ignoreRel := filepath.ToSlash(path.Join(baseDir, childRel))
		if ignore.Matches(ignoreRel, entry.IsDir()) {
			continue
		}
		childInfo, err := entry.Info()
		if err != nil {
			return nil, "", err
		}
		href := baseHref + childRel
		kind := "file"
		if entry.IsDir() {
			href += "/"
			kind = "dir"
		} else {
			href = "/" + category + "/" + childRel
		}
		entries = append(entries, browseEntry{Name: name, Href: href, Kind: kind, Size: childInfo.Size()})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Name == ".." {
			return true
		}
		if entries[j].Name == ".." {
			return false
		}
		if entries[i].Kind != entries[j].Kind {
			return entries[i].Kind == "dir"
		}
		return entries[i].Name < entries[j].Name
	})
	title := "/browse/" + category + "/"
	if relPath != "" {
		title += relPath
	}
	return entries, title, nil
}

func (h *serverHandler) handleBrowseImages(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, "/browse/images")
	rel = strings.Trim(rel, "/")
	body, err := h.renderImageBrowse(rel)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusOK)
	writeResponseBody(w, []byte(body))
}

func (h *serverHandler) renderImageBrowse(rel string) (string, error) {
	entries, err := h.scanRegistryCatalog()
	if err != nil {
		return "", err
	}
	if rel == "" {
		repos := map[string]bool{}
		for _, entry := range entries {
			repos[entry.repo] = true
		}
		items := make([]browseEntry, 0, len(repos))
		for repo := range repos {
			items = append(items, browseEntry{Name: repo, Href: "/browse/images/" + repo + "/", Kind: "repo"})
		}
		sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
		items = append([]browseEntry{{Name: "..", Href: "/", Kind: "dir"}}, items...)
		return renderBrowsePage("/browse/images/", items)
	}
	if repoExists(entries, rel) {
		repo := rel
		tags := map[string]bool{}
		for _, entry := range entries {
			if entry.repo == repo {
				tags[entry.tag] = true
			}
		}
		items := []browseEntry{{Name: "..", Href: "/browse/images/", Kind: "dir"}}
		for tag := range tags {
			items = append(items, browseEntry{Name: tag, Href: "/browse/images/" + repo + "/" + tag + "/", Kind: "tag"})
		}
		sort.Slice(items[1:], func(i, j int) bool { return items[1+i].Name < items[1+j].Name })
		return renderBrowsePage("/browse/images/"+repo+"/", items)
	}
	repo, tag := splitRepoTag(entries, rel)
	if repo == "" || tag == "" {
		items := []browseEntry{{Name: "..", Href: "/browse/images/", Kind: "dir"}}
		return renderBrowsePage("/browse/images/"+rel+"/", items)
	}
	resolved, resolveErr := h.resolveRegistryImage(repo, tag)
	if resolveErr != nil {
		return "", resolveErr
	}
	items := []browseEntry{{Name: "..", Href: "/browse/images/" + repo + "/", Kind: "dir"}}
	items = append(items,
		browseEntry{Name: "Repository", Kind: "meta", Meta: resolved.repo},
		browseEntry{Name: "tag", Kind: "meta", Meta: resolved.tag},
		browseEntry{Name: "digest", Kind: "meta", Meta: resolved.digest.String()},
		browseEntry{Name: "archive", Kind: "meta", Meta: resolved.tarPath},
		browseEntry{Name: "registry manifest", Href: "/v2/" + repo + "/manifests/" + tag, Kind: "link", Meta: "open raw manifest"},
	)
	if resolved.manifest != nil {
		for _, layer := range resolved.manifest.Layers {
			items = append(items, browseEntry{Name: "layer", Kind: "meta", Meta: layer.Digest.String()})
		}
	}
	return renderBrowsePage("/browse/images/"+repo+"/"+tag+"/", items)
}

func repoExists(entries []registryCatalogEntry, repo string) bool {
	for _, entry := range entries {
		if entry.repo == repo {
			return true
		}
	}
	return false
}

func splitRepoTag(entries []registryCatalogEntry, rel string) (string, string) {
	bestRepo := ""
	bestTag := ""
	for _, entry := range entries {
		prefix := entry.repo + "/"
		if !strings.HasPrefix(rel, prefix) {
			continue
		}
		tag := strings.TrimPrefix(rel, prefix)
		if tag == "" || strings.Contains(tag, "/") {
			continue
		}
		if len(entry.repo) > len(bestRepo) {
			bestRepo = entry.repo
			bestTag = tag
		}
	}
	return bestRepo, bestTag
}

func (h *serverHandler) hasBrowseEntries(category string) bool {
	entries, _, err := h.listBrowseEntries(category, "")
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.Name != ".." {
			return true
		}
	}
	return false
}

func (h *serverHandler) hasDeckBinary() bool {
	info, err := os.Stat(filepath.Join(h.rootAbs, "deck"))
	return err == nil && !info.IsDir()
}

func (h *serverHandler) hasImageEntries() bool {
	entries, err := h.scanRegistryCatalog()
	return err == nil && len(entries) > 0
}
