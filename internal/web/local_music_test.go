package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/guohuiyuan/music-lib/model"
)

func withLocalMusicDownloadDir(t *testing.T, dir string) {
	t.Helper()

	original := localMusicDownloadDirProvider
	localMusicDownloadDirProvider = func() string {
		return dir
	}
	t.Cleanup(func() {
		localMusicDownloadDirProvider = original
	})
}

func newLocalMusicTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	group := r.Group(RoutePrefix)
	RegisterMusicRoutes(group)
	RegisterCollectionRoutes(group)
	RegisterLocalMusicRoutes(group)
	return r
}

func TestSearchBoxTemplateShowsLocalMusicEntryNextToCollections(t *testing.T) {
	content, err := templateFS.ReadFile("templates/partials/search_box.html")
	if err != nil {
		t.Fatalf("ReadFile(search_box.html): %v", err)
	}

	html := string(content)
	if !strings.Contains(html, `onclick="openCollectionManager()"`) {
		t.Fatal("search box missing custom collection entry")
	}
	if !strings.Contains(html, `onclick="openLocalMusicPage()"`) {
		t.Fatal("search box missing local music page entry")
	}
	if strings.Index(html, `onclick="openLocalMusicPage()"`) < strings.Index(html, `onclick="openCollectionManager()"`) {
		t.Fatal("local music entry should be placed to the right of custom collection entry")
	}
	if !strings.Contains(html, "本地音乐") {
		t.Fatal("search box missing local music label")
	}

	playlistGrid, err := templateFS.ReadFile("templates/partials/playlist_grid.html")
	if err != nil {
		t.Fatalf("ReadFile(playlist_grid.html): %v", err)
	}
	if strings.Contains(string(playlistGrid), `onclick="openLocalMusicModal()"`) {
		t.Fatal("local music entry should not be inside custom collection page header")
	}
}

func TestLocalMusicListScansDownloadDirWithFallbacks(t *testing.T) {
	initCollectionDBForTest(t)

	downloadDir := t.TempDir()
	withLocalMusicDownloadDir(t, downloadDir)

	audioPath := filepath.Join(downloadDir, "Plain Track.mp3")
	if err := os.WriteFile(audioPath, []byte("not a real mp3, but has a supported extension"), 0644); err != nil {
		t.Fatalf("write local audio: %v", err)
	}

	collection := Collection{
		Name:        "Local",
		Kind:        collectionKindManual,
		ContentType: collectionContentPlaylist,
		Source:      "local",
	}
	if err := db.Create(&collection).Error; err != nil {
		t.Fatalf("create collection: %v", err)
	}

	localID := encodeLocalMusicID("Plain Track.mp3")
	if err := db.Create(&SavedSong{
		CollectionID: collection.ID,
		SongID:       localID,
		Source:       localMusicSource,
		Name:         "Plain Track",
		Artist:       "未知歌手",
	}).Error; err != nil {
		t.Fatalf("create saved local song: %v", err)
	}

	router := newLocalMusicTestRouter()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("%s/local_music?collection_id=%d", RoutePrefix, collection.ID), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /local_music status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Exists bool              `json:"exists"`
		Tracks []localMusicTrack `json:"tracks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode local music response: %v", err)
	}
	if !resp.Exists {
		t.Fatal("local music response exists = false, want true")
	}
	if len(resp.Tracks) != 1 {
		t.Fatalf("local music tracks len = %d, want 1", len(resp.Tracks))
	}

	track := resp.Tracks[0]
	if track.ID != localID {
		t.Fatalf("track.ID = %q, want %q", track.ID, localID)
	}
	if track.Name != "Plain Track" {
		t.Fatalf("track.Name = %q, want Plain Track", track.Name)
	}
	if track.Artist != "未知歌手" {
		t.Fatalf("track.Artist = %q, want 未知歌手", track.Artist)
	}
	if !track.AlreadyAdded {
		t.Fatal("track.AlreadyAdded = false, want true")
	}
	if track.Source != localMusicSource {
		t.Fatalf("track.Source = %q, want %q", track.Source, localMusicSource)
	}
}

func TestLocalMusicPageRendersSongListWithoutUnsupportedActions(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.SetHTMLTemplate(newTestTemplate(t))
	router.GET(RoutePrefix, func(c *gin.Context) {
		renderIndex(c, []model.Song{
			{
				ID:       encodeLocalMusicID("Local Track.mp3"),
				Source:   localMusicSource,
				Name:     "Local Track",
				Artist:   "Local Artist",
				Album:    "Local Album",
				Duration: 125,
			},
		}, nil, "", nil, "", "local_music", "", "", "", false, "", nil)
	})

	req := httptest.NewRequest(http.MethodGet, RoutePrefix, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()
	required := []string{
		`id="localMusicPageUploadInput"`,
		`onchange="uploadLocalMusicForPage(this)"`,
		`id="btn-batch-delete-local"`,
		`onclick="batchDeleteLocalMusic()"`,
		`data-source="local"`,
		`class="tag tag-local"`,
		`>本地</span>`,
		`class="btn-circle btn-play"`,
	}
	for _, token := range required {
		if !strings.Contains(body, token) {
			t.Fatalf("local music page missing %q in rendered body: %s", token, body)
		}
	}

	forbidden := []string{
		`class="btn-circle btn-switch"`,
		`class="btn-circle btn-fav"`,
		`class="btn-circle btn-dl btn-download"`,
		`class="btn-circle btn-dl btn-lyric"`,
		`class="btn-circle btn-dl btn-cover"`,
		`id="btn-batch-switch"`,
		`id="btn-batch-dl"`,
		`selectInvalidSongs()`,
		`openAddToCollectionModal`,
		`removeSongFromCollection`,
	}
	for _, token := range forbidden {
		if strings.Contains(body, token) {
			t.Fatalf("local music page should not render %q: %s", token, body)
		}
	}
}

func TestUploadLocalMusicAddToCollectionAndDownload(t *testing.T) {
	initCollectionDBForTest(t)

	downloadDir := t.TempDir()
	withLocalMusicDownloadDir(t, downloadDir)

	collection := Collection{
		Name:        "Uploads",
		Kind:        collectionKindManual,
		ContentType: collectionContentPlaylist,
		Source:      "local",
	}
	if err := db.Create(&collection).Error; err != nil {
		t.Fatalf("create collection: %v", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "Uploaded Song.flac")
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := part.Write([]byte("fLaC uploaded audio bytes")); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	router := newLocalMusicTestRouter()
	req := httptest.NewRequest(http.MethodPost, RoutePrefix+"/local_music/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /local_music/upload status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var uploadResp struct {
		Track localMusicTrack `json:"track"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &uploadResp); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if uploadResp.Track.ID == "" {
		t.Fatal("uploaded track ID is empty")
	}
	if uploadResp.Track.Name != "Uploaded Song" {
		t.Fatalf("uploaded track name = %q, want Uploaded Song", uploadResp.Track.Name)
	}

	addBody, err := json.Marshal(map[string]string{"id": uploadResp.Track.ID})
	if err != nil {
		t.Fatalf("marshal add body: %v", err)
	}
	addPath := fmt.Sprintf("%s/collections/%d/local_music", RoutePrefix, collection.ID)
	req = httptest.NewRequest(http.MethodPost, addPath, bytes.NewReader(addBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST %s status = %d, want %d, body=%s", addPath, rec.Code, http.StatusOK, rec.Body.String())
	}

	var saved SavedSong
	if err := db.Where("collection_id = ? AND song_id = ? AND source = ?", collection.ID, uploadResp.Track.ID, localMusicSource).First(&saved).Error; err != nil {
		t.Fatalf("query saved local song: %v", err)
	}
	if saved.Name != "Uploaded Song" || saved.Artist != "未知歌手" {
		t.Fatalf("saved local song metadata = %q/%q, want Uploaded Song/未知歌手", saved.Name, saved.Artist)
	}

	downloadURL := fmt.Sprintf("%s/download?id=%s&source=%s", RoutePrefix, uploadResp.Track.ID, localMusicSource)
	req = httptest.NewRequest(http.MethodGet, downloadURL, nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET local download status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if rec.Body.String() != "fLaC uploaded audio bytes" {
		t.Fatalf("download body = %q, want uploaded bytes", rec.Body.String())
	}
}

func TestDeleteLocalMusicRemovesFileAndSavedRows(t *testing.T) {
	initCollectionDBForTest(t)

	downloadDir := t.TempDir()
	withLocalMusicDownloadDir(t, downloadDir)

	audioPath := filepath.Join(downloadDir, "Delete Me.mp3")
	if err := os.WriteFile(audioPath, []byte("delete me"), 0644); err != nil {
		t.Fatalf("write local audio: %v", err)
	}
	localID := encodeLocalMusicID("Delete Me.mp3")

	collections := []Collection{
		{Name: "Local One", Kind: collectionKindManual, ContentType: collectionContentPlaylist, Source: "local"},
		{Name: "Local Two", Kind: collectionKindManual, ContentType: collectionContentPlaylist, Source: "local"},
	}
	if err := db.Create(&collections).Error; err != nil {
		t.Fatalf("create collections: %v", err)
	}

	saved := []SavedSong{
		{CollectionID: collections[0].ID, SongID: localID, Source: localMusicSource, Name: "Delete Me"},
		{CollectionID: collections[1].ID, SongID: localID, Source: legacyLocalMusicSource, Name: "Delete Me"},
	}
	if err := db.Create(&saved).Error; err != nil {
		t.Fatalf("create saved local songs: %v", err)
	}

	router := newLocalMusicTestRouter()
	req := httptest.NewRequest(http.MethodDelete, RoutePrefix+"/local_music?id="+url.QueryEscape(localID), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /local_music status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if _, err := os.Stat(audioPath); !os.IsNotExist(err) {
		t.Fatalf("deleted local file stat err = %v, want not exists", err)
	}

	var count int64
	if err := db.Model(&SavedSong{}).
		Where("song_id = ? AND source IN ?", localID, []string{localMusicSource, legacyLocalMusicSource}).
		Count(&count).Error; err != nil {
		t.Fatalf("count saved local songs: %v", err)
	}
	if count != 0 {
		t.Fatalf("saved local songs count = %d, want 0", count)
	}
}
