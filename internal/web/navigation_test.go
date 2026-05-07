package web

import (
	"encoding/json"
	"html/template"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guohuiyuan/music-lib/model"
)

func newTestTemplate(t *testing.T) *template.Template {
	t.Helper()

	return template.Must(template.New("").Funcs(template.FuncMap{
		"artistTokens":       splitArtistTokens,
		"albumID":            songAlbumID,
		"playlistDetailURL":  playlistDetailURL,
		"playlistExtraValue": playlistExtraValue,
		"tojson": func(v interface{}) string {
			if v == nil {
				return ""
			}
			b, err := json.Marshal(v)
			if err != nil {
				return ""
			}
			return string(b)
		},
	}).ParseFS(templateFS,
		"templates/pages/*.html",
		"templates/partials/*.html",
	))
}

func TestRenderIndexPlaylistCardsUseAjaxNavigation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.SetHTMLTemplate(newTestTemplate(t))
	router.GET(RoutePrefix, func(c *gin.Context) {
		renderIndex(c, nil, []model.Playlist{
			{
				ID:         "123",
				Name:       "Top Hits",
				TrackCount: 18,
				Creator:    "Tester",
				Source:     "qq",
				Cover:      "https://example.com/cover.jpg",
			},
		}, "", []string{"qq"}, "", collectionContentPlaylist, "", "", "", false, "", nil)
	})

	req := httptest.NewRequest("GET", RoutePrefix, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if strings.Contains(body, `onclick="location.href=`) {
		t.Fatalf("rendered html still uses location.href navigation: %s", body)
	}
	if !strings.Contains(body, `onclick="navigateTo('`) {
		t.Fatalf("rendered html missing navigateTo playlist navigation: %s", body)
	}
}

func TestAppJSIncludesAjaxNavigationEntryPoints(t *testing.T) {
	content, err := templateFS.ReadFile("templates/static/js/app.js")
	if err != nil {
		t.Fatalf("ReadFile(app.js): %v", err)
	}

	js := string(content)
	if !strings.Contains(js, "async function navigateTo(url, options = {})") {
		t.Fatal("app.js missing navigateTo function")
	}
	if !strings.Contains(js, "function bindPageNavigationEvents()") {
		t.Fatal("app.js missing bindPageNavigationEvents function")
	}
	if !strings.Contains(js, "function handlePaginationShortcut(event)") {
		t.Fatal("app.js missing pagination shortcut handler")
	}
	if !strings.Contains(js, "document.addEventListener('keydown', handlePaginationShortcut);") {
		t.Fatal("app.js missing pagination shortcut binding")
	}
	if !strings.Contains(js, "initializePageContent(document);") {
		t.Fatal("app.js missing initializePageContent bootstrap")
	}
	if !strings.Contains(js, "function refreshDownloadLinks(root = document)") {
		t.Fatal("app.js missing scoped refreshDownloadLinks helper")
	}
	if !strings.Contains(js, "refreshDownloadLinks(root);") {
		t.Fatal("app.js missing download link refresh during page initialization")
	}
}

func TestAppJSPlaybackURLIgnoresEmbedDownloadSetting(t *testing.T) {
	content, err := templateFS.ReadFile("templates/static/js/app.js")
	if err != nil {
		t.Fatalf("ReadFile(app.js): %v", err)
	}

	js := string(content)
	if !strings.Contains(js, "function buildStreamURL(id, source, name, artist, album, cover, extra)") {
		t.Fatal("app.js missing buildStreamURL")
	}
	if !strings.Contains(js, "stream: true") {
		t.Fatal("buildStreamURL should force stream=1")
	}
	if strings.Contains(js, "function buildStreamURL(id, source, name, artist, album, cover, extra) {\n    return buildDownloadRequestURL(id, source, name, artist, album, cover, extra, {\n        embed: webSettings.embedDownload") {
		t.Fatal("buildStreamURL must not follow embedDownload; playback should always use plain streaming")
	}
	if !strings.Contains(js, "preload: 'metadata'") {
		t.Fatal("APlayer should preload metadata instead of full audio")
	}
}

func TestDownloadURLsCarryAlbumForMetadataEmbedding(t *testing.T) {
	jsContent, err := templateFS.ReadFile("templates/static/js/app.js")
	if err != nil {
		t.Fatalf("ReadFile(app.js): %v", err)
	}
	js := string(jsContent)
	for _, want := range []string{
		"function buildDownloadRequestURL(id, source, name, artist, album, cover, extra, options = {})",
		"params.set('album', albumValue);",
		"buildDownloadURL(ds.id, ds.source, ds.name, ds.artist, ds.album || ''",
		"buildDownloadURL(song.id, song.source, song.name, song.artist, song.album || ''",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("app.js missing album download URL token %q", want)
		}
	}

	htmlContent, err := templateFS.ReadFile("templates/partials/song_list.html")
	if err != nil {
		t.Fatalf("ReadFile(song_list.html): %v", err)
	}
	if !strings.Contains(string(htmlContent), `&album={{urlquery .Album}}`) {
		t.Fatal("song_list.html download link should include album query parameter")
	}
}

func TestSettingsModalIncludesDownloadDirPresets(t *testing.T) {
	content, err := templateFS.ReadFile("templates/partials/modals.html")
	if err != nil {
		t.Fatalf("ReadFile(modals.html): %v", err)
	}

	html := string(content)
	for _, want := range []string{
		`id="setting-download-dir-preset"`,
		`id="setting-download-filename-template"`,
		`id="setting-floating-lyrics"`,
		`{album}`,
		`{source}`,
		`{ext}`,
		`PC 默认：data/downloads`,
		`PC 示例：D:/Music/Downloads`,
		`Android 默认：/sdcard/Music`,
		`Android 兼容：/storage/emulated/0/Music`,
		`自定义目录...`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("settings modal missing download dir preset %q", want)
		}
	}
}

func TestPaginationTemplatesExposeShortcutMetadata(t *testing.T) {
	paths := []string{
		"templates/partials/song_list.html",
		"templates/partials/playlist_grid.html",
	}

	for _, path := range paths {
		content, err := templateFS.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", path, err)
		}

		html := string(content)
		if !strings.Contains(html, `data-current-page="{{ .Page }}"`) {
			t.Fatalf("%s missing current page metadata", path)
		}
		if !strings.Contains(html, `data-total-pages="{{ .TotalPages }}"`) {
			t.Fatalf("%s missing total pages metadata", path)
		}
		if !strings.Contains(html, `data-shortcut-hint="PgUp / PgDn"`) {
			t.Fatalf("%s missing pagination shortcut hint", path)
		}
	}
}
