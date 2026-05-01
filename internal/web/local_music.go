package web

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dhowden/tag"
	"github.com/gin-gonic/gin"
	"github.com/guohuiyuan/go-music-dl/core"
	"github.com/guohuiyuan/music-lib/model"
	"github.com/guohuiyuan/music-lib/utils"
	"gorm.io/gorm/clause"
)

const (
	localMusicSource       = "local"
	legacyLocalMusicSource = "local-file"
)

var localMusicDownloadDirProvider = func() string {
	return core.GetWebSettings().DownloadDir
}

var localMusicAudioExts = map[string]struct{}{
	".aac":  {},
	".flac": {},
	".m4a":  {},
	".mp3":  {},
	".ogg":  {},
	".wav":  {},
	".wma":  {},
}

type localMusicTrack struct {
	ID           string            `json:"id"`
	Source       string            `json:"source"`
	Name         string            `json:"name"`
	Artist       string            `json:"artist"`
	Album        string            `json:"album"`
	Cover        string            `json:"cover"`
	Duration     int               `json:"duration"`
	Filename     string            `json:"filename"`
	RelPath      string            `json:"rel_path"`
	Ext          string            `json:"ext"`
	Size         int64             `json:"size"`
	SizeText     string            `json:"size_text"`
	ModifiedAt   time.Time         `json:"modified_at"`
	Missing      []string          `json:"missing"`
	AlreadyAdded bool              `json:"already_added,omitempty"`
	Extra        map[string]string `json:"extra"`

	absPath string
	modTime time.Time
}

func RegisterLocalMusicRoutes(api *gin.RouterGroup) {
	api.GET("/local_music_page", func(c *gin.Context) {
		tracks, _, exists, err := scanLocalMusicTracks()
		errMsg := ""
		if err != nil {
			errMsg = "加载本地音乐失败: " + err.Error()
		} else if !exists {
			errMsg = "本地下载目录不存在，可上传音乐或在设置中调整下载目录"
		}

		renderIndex(c, localMusicTracksToSongs(tracks), nil, "", nil, errMsg, "local_music", "", "", "", false, "", nil)
	})

	api.GET("/local_music", func(c *gin.Context) {
		tracks, dir, exists, err := scanLocalMusicTracks()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		markAlreadyAddedLocalTracks(c.Query("collection_id"), tracks)
		c.JSON(http.StatusOK, gin.H{
			"download_dir": filepath.ToSlash(dir),
			"exists":       exists,
			"tracks":       tracks,
		})
	})

	api.GET("/local_music/cover", func(c *gin.Context) {
		track, err := localMusicTrackByID(c.Query("id"))
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}

		picture, err := readLocalMusicPicture(track.absPath)
		if err != nil || picture == nil || len(picture.Data) == 0 {
			c.Status(http.StatusNotFound)
			return
		}
		mimeType := strings.TrimSpace(picture.MIMEType)
		if mimeType == "" {
			mimeType = "image/jpeg"
		}
		c.Header("Cache-Control", "public, max-age=21600")
		c.Data(http.StatusOK, mimeType, picture.Data)
	})

	api.POST("/local_music/upload", func(c *gin.Context) {
		file, err := c.FormFile("file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请选择要上传的音乐文件"})
			return
		}

		track, err := saveUploadedLocalMusic(file)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"track":  track,
		})
	})

	api.DELETE("/local_music", func(c *gin.Context) {
		if err := deleteLocalMusicTrack(c.Query("id")); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	colAPI := api.Group("/collections")
	colAPI.POST("/:id/local_music", func(c *gin.Context) {
		collection, err := loadCollection(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "歌单不存在"})
			return
		}
		if collection.isImported() {
			c.JSON(http.StatusBadRequest, gin.H{"error": "外部导入歌单/专辑不支持直接添加本地音乐"})
			return
		}

		var req struct {
			ID string `json:"id" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "缺少本地音乐 ID"})
			return
		}

		track, err := localMusicTrackByID(req.ID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "本地音乐不存在或已不在下载目录内"})
			return
		}

		extra, _ := json.Marshal(track.Extra)
		song := SavedSong{
			CollectionID: collection.ID,
			SongID:       track.ID,
			Source:       localMusicSource,
			Extra:        string(extra),
			Name:         track.Name,
			Artist:       track.Artist,
			Cover:        track.Cover,
			Duration:     track.Duration,
			AddedAt:      time.Now(),
		}

		tx := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&song)
		if tx.Error != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "添加失败: " + tx.Error.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status":    "ok",
			"duplicate": tx.RowsAffected == 0,
			"song":      song,
		})
	})
}

func isLocalMusicSource(source string) bool {
	source = strings.TrimSpace(source)
	return source == localMusicSource || source == legacyLocalMusicSource
}

func localMusicTracksToSongs(tracks []*localMusicTrack) []model.Song {
	songs := make([]model.Song, 0, len(tracks))
	for _, track := range tracks {
		if track == nil {
			continue
		}
		songs = append(songs, model.Song{
			ID:       track.ID,
			Source:   localMusicSource,
			Name:     track.Name,
			Artist:   track.Artist,
			Album:    track.Album,
			Cover:    track.Cover,
			Duration: track.Duration,
			Extra:    track.Extra,
		})
	}
	return songs
}

func scanLocalMusicTracks() ([]*localMusicTrack, string, bool, error) {
	dir := localMusicDownloadDir()
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*localMusicTrack{}, dir, false, nil
		}
		return nil, dir, false, err
	}
	if !info.IsDir() {
		return nil, dir, false, fmt.Errorf("本地下载路径不是目录: %s", dir)
	}

	rootAbs, err := filepath.Abs(dir)
	if err != nil {
		return nil, dir, true, err
	}

	tracks := make([]*localMusicTrack, 0)
	err = filepath.WalkDir(rootAbs, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || strings.HasPrefix(entry.Name(), ".") {
				if path != rootAbs {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !isLocalMusicAudioFile(path) {
			return nil
		}

		track, err := buildLocalMusicTrack(rootAbs, path)
		if err == nil {
			tracks = append(tracks, track)
		}
		return nil
	})
	if err != nil {
		return nil, dir, true, err
	}

	sort.SliceStable(tracks, func(i, j int) bool {
		if !tracks[i].modTime.Equal(tracks[j].modTime) {
			return tracks[i].modTime.After(tracks[j].modTime)
		}
		return strings.ToLower(tracks[i].RelPath) < strings.ToLower(tracks[j].RelPath)
	})

	return tracks, dir, true, nil
}

func markAlreadyAddedLocalTracks(collectionID string, tracks []*localMusicTrack) {
	if strings.TrimSpace(collectionID) == "" || len(tracks) == 0 || db == nil {
		return
	}

	collection, err := loadCollection(collectionID)
	if err != nil || collection.isImported() {
		return
	}

	ids := make([]string, 0, len(tracks))
	for _, track := range tracks {
		ids = append(ids, track.ID)
	}

	var saved []SavedSong
	if err := db.Where(
		"collection_id = ? AND source IN ? AND song_id IN ?",
		collection.ID,
		[]string{localMusicSource, legacyLocalMusicSource},
		ids,
	).Find(&saved).Error; err != nil {
		return
	}

	added := make(map[string]struct{}, len(saved))
	for _, song := range saved {
		added[song.SongID] = struct{}{}
	}
	for _, track := range tracks {
		_, track.AlreadyAdded = added[track.ID]
	}
}

func localMusicDownloadDir() string {
	dir := strings.TrimSpace(localMusicDownloadDirProvider())
	if dir == "" {
		dir = core.DefaultWebDownloadDir
	}
	return filepath.Clean(dir)
}

func isLocalMusicAudioFile(path string) bool {
	_, ok := localMusicAudioExts[strings.ToLower(filepath.Ext(path))]
	return ok
}

func buildLocalMusicTrack(rootAbs string, audioPath string) (*localMusicTrack, error) {
	absPath, err := filepath.Abs(audioPath)
	if err != nil {
		return nil, err
	}
	if !isPathInside(rootAbs, absPath) {
		return nil, errors.New("path is outside local music dir")
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return nil, err
	}
	if info.IsDir() || !isLocalMusicAudioFile(absPath) {
		return nil, errors.New("not a supported audio file")
	}

	rel, err := filepath.Rel(rootAbs, absPath)
	if err != nil {
		return nil, err
	}
	rel = filepath.ToSlash(rel)

	filename := info.Name()
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	fallbackName := strings.TrimSuffix(filename, filepath.Ext(filename))
	name := ""
	artist := ""
	album := ""
	hasCover := false

	if file, err := os.Open(absPath); err == nil {
		if metadata, readErr := tag.ReadFrom(file); readErr == nil {
			name = strings.TrimSpace(metadata.Title())
			artist = strings.TrimSpace(metadata.Artist())
			album = strings.TrimSpace(metadata.Album())
			hasCover = metadata.Picture() != nil
		}
		_ = file.Close()
	}

	missing := make([]string, 0, 3)
	if strings.TrimSpace(name) == "" {
		name = fallbackName
		missing = append(missing, "title")
	}
	if strings.TrimSpace(artist) == "" {
		artist = "未知歌手"
		missing = append(missing, "artist")
	}
	if strings.TrimSpace(album) == "" {
		missing = append(missing, "album")
	}

	id := encodeLocalMusicID(rel)
	extra := map[string]string{
		"local_music": "true",
		"file_id":     id,
		"filename":    filename,
		"rel_path":    rel,
		"ext":         ext,
		"size":        strconv.FormatInt(info.Size(), 10),
	}
	if album != "" {
		extra["album"] = album
	}

	cover := ""
	if hasCover {
		cover = RoutePrefix + "/local_music/cover?id=" + url.QueryEscape(id)
	}

	return &localMusicTrack{
		ID:         id,
		Source:     localMusicSource,
		Name:       strings.TrimSpace(name),
		Artist:     strings.TrimSpace(artist),
		Album:      strings.TrimSpace(album),
		Cover:      cover,
		Duration:   0,
		Filename:   filename,
		RelPath:    rel,
		Ext:        ext,
		Size:       info.Size(),
		SizeText:   core.FormatSize(info.Size()),
		ModifiedAt: info.ModTime(),
		Missing:    missing,
		Extra:      extra,
		absPath:    absPath,
		modTime:    info.ModTime(),
	}, nil
}

func localMusicTrackByID(id string) (*localMusicTrack, error) {
	rel, err := decodeLocalMusicID(id)
	if err != nil {
		return nil, err
	}
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return nil, errors.New("empty local music id")
	}

	cleanRel := filepath.Clean(filepath.FromSlash(rel))
	if filepath.IsAbs(cleanRel) || cleanRel == "." || cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(filepath.Separator)) {
		return nil, errors.New("invalid local music path")
	}

	rootAbs, err := filepath.Abs(localMusicDownloadDir())
	if err != nil {
		return nil, err
	}
	audioPath := filepath.Join(rootAbs, cleanRel)
	absPath, err := filepath.Abs(audioPath)
	if err != nil {
		return nil, err
	}
	if !isPathInside(rootAbs, absPath) {
		return nil, errors.New("local music path escaped root")
	}

	return buildLocalMusicTrack(rootAbs, absPath)
}

func encodeLocalMusicID(relPath string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(filepath.ToSlash(relPath)))
}

func decodeLocalMusicID(id string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(id))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func isPathInside(rootAbs string, targetAbs string) bool {
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func saveUploadedLocalMusic(file *multipart.FileHeader) (*localMusicTrack, error) {
	filename, err := sanitizeLocalMusicUploadName(file.Filename)
	if err != nil {
		return nil, err
	}

	dir := localMusicDownloadDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	rootAbs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	dstPath := uniqueLocalMusicPath(rootAbs, filename)

	src, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()

	dst, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return nil, err
	}
	_, copyErr := io.Copy(dst, src)
	closeErr := dst.Close()
	if copyErr != nil {
		_ = os.Remove(dstPath)
		return nil, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(dstPath)
		return nil, closeErr
	}

	return buildLocalMusicTrack(rootAbs, dstPath)
}

func sanitizeLocalMusicUploadName(name string) (string, error) {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name)
	ext := strings.ToLower(filepath.Ext(name))
	if _, ok := localMusicAudioExts[ext]; !ok {
		return "", fmt.Errorf("仅支持 mp3、flac、m4a、ogg、wav、wma、aac 音频文件")
	}

	base := strings.TrimSuffix(name, filepath.Ext(name))
	base = strings.TrimSpace(utils.SanitizeFilename(base))
	if base == "" {
		base = "local-music"
	}
	return base + ext, nil
}

func uniqueLocalMusicPath(dir string, filename string) string {
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	candidate := filepath.Join(dir, filename)
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate
	}
	for i := 1; ; i++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

type localProbeResult struct {
	Duration int
	Bitrate  int
	Title    string
	Artist   string
	Album    string
}

func probeLocalMusicTrack(track *localMusicTrack) (*localProbeResult, error) {
	if track == nil || strings.TrimSpace(track.absPath) == "" {
		return nil, errors.New("empty local music track")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return nil, err
	}

	cmd := exec.Command("ffprobe", "-v", "quiet", "-print_format", "json", "-show_format", "-show_streams", track.absPath)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var payload struct {
		Format struct {
			Duration string            `json:"duration"`
			BitRate  string            `json:"bit_rate"`
			Tags     map[string]string `json:"tags"`
		} `json:"format"`
		Streams []struct {
			CodecType string            `json:"codec_type"`
			Duration  string            `json:"duration"`
			BitRate   string            `json:"bit_rate"`
			Tags      map[string]string `json:"tags"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil, err
	}

	result := &localProbeResult{
		Duration: secondsFromProbe(payload.Format.Duration),
		Bitrate:  kbpsFromProbe(payload.Format.BitRate),
		Title:    probeTag(payload.Format.Tags, "title"),
		Artist:   probeTag(payload.Format.Tags, "artist"),
		Album:    probeTag(payload.Format.Tags, "album"),
	}

	for _, stream := range payload.Streams {
		if stream.CodecType != "audio" {
			continue
		}
		if result.Duration <= 0 {
			result.Duration = secondsFromProbe(stream.Duration)
		}
		if result.Bitrate <= 0 {
			result.Bitrate = kbpsFromProbe(stream.BitRate)
		}
		if result.Title == "" {
			result.Title = probeTag(stream.Tags, "title")
		}
		if result.Artist == "" {
			result.Artist = probeTag(stream.Tags, "artist")
		}
		if result.Album == "" {
			result.Album = probeTag(stream.Tags, "album")
		}
		break
	}

	return result, nil
}

func secondsFromProbe(raw string) int {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || value <= 0 {
		return 0
	}
	return int(value + 0.5)
}

func kbpsFromProbe(raw string) int {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value <= 0 {
		return 0
	}
	return int(value / 1000)
}

func probeTag(tags map[string]string, key string) string {
	if len(tags) == 0 {
		return ""
	}
	for k, v := range tags {
		if strings.EqualFold(strings.TrimSpace(k), key) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func readLocalMusicPicture(audioPath string) (*tag.Picture, error) {
	file, err := os.Open(audioPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	metadata, err := tag.ReadFrom(file)
	if err != nil {
		return nil, err
	}
	return metadata.Picture(), nil
}

func inspectLocalMusicFile(id string, duration string) (gin.H, error) {
	track, err := localMusicTrackByID(id)
	if err != nil {
		return gin.H{"valid": false}, err
	}

	if probe, err := probeLocalMusicTrack(track); err == nil && probe != nil {
		if probe.Duration > 0 {
			track.Duration = probe.Duration
			track.Extra["duration"] = strconv.Itoa(probe.Duration)
		}
		if probe.Title != "" && containsString(track.Missing, "title") {
			track.Name = probe.Title
			track.Extra["title"] = probe.Title
		}
		if probe.Artist != "" && containsString(track.Missing, "artist") {
			track.Artist = probe.Artist
			track.Extra["artist"] = probe.Artist
		}
		if probe.Album != "" && containsString(track.Missing, "album") {
			track.Album = probe.Album
			track.Extra["album"] = probe.Album
		}
		if probe.Bitrate > 0 {
			track.Extra["bitrate"] = strconv.Itoa(probe.Bitrate)
		}
	}

	bitrate := "-"
	if kbps, _ := strconv.Atoi(track.Extra["bitrate"]); kbps > 0 {
		bitrate = fmt.Sprintf("%d kbps", kbps)
	} else if seconds := track.Duration; seconds > 0 && track.Size > 0 {
		bitrate = fmt.Sprintf("%d kbps", int((track.Size*8)/int64(seconds)/1000))
	} else if seconds, _ := strconv.Atoi(strings.TrimSpace(duration)); seconds > 0 && track.Size > 0 {
		bitrate = fmt.Sprintf("%d kbps", int((track.Size*8)/int64(seconds)/1000))
	}

	return gin.H{
		"valid":    true,
		"url":      "",
		"size":     track.SizeText,
		"bitrate":  bitrate,
		"duration": track.Duration,
		"song": gin.H{
			"id":       track.ID,
			"source":   track.Source,
			"name":     track.Name,
			"artist":   track.Artist,
			"album":    track.Album,
			"cover":    track.Cover,
			"duration": track.Duration,
			"extra":    track.Extra,
		},
	}, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func deleteLocalMusicTrack(id string) error {
	track, err := localMusicTrackByID(id)
	if err != nil {
		return errors.New("本地音乐不存在或已不在下载目录内")
	}
	if err := os.Remove(track.absPath); err != nil {
		return err
	}
	if db != nil {
		_ = db.Where("song_id = ? AND source IN ?", track.ID, []string{localMusicSource, legacyLocalMusicSource}).Delete(&SavedSong{}).Error
	}
	return nil
}

func serveLocalMusicDownload(c *gin.Context, id string, saveLocal bool) {
	track, err := localMusicTrackByID(id)
	if err != nil {
		c.String(http.StatusNotFound, "Local music not found")
		return
	}

	if saveLocal {
		c.JSON(http.StatusOK, gin.H{
			"status":   "ok",
			"saved":    true,
			"path":     track.absPath,
			"filename": track.Filename,
		})
		return
	}

	file, err := os.Open(track.absPath)
	if err != nil {
		c.String(http.StatusNotFound, "Local music not found")
		return
	}
	defer file.Close()

	c.Header("Content-Type", localAudioMimeByExt(track.Ext))
	setDownloadHeader(c, track.Filename)
	http.ServeContent(c.Writer, c.Request, track.Filename, track.modTime, file)
}

func localAudioMimeByExt(ext string) string {
	switch strings.ToLower(strings.TrimPrefix(ext, ".")) {
	case "aac":
		return "audio/aac"
	case "wav":
		return "audio/wav"
	default:
		return core.AudioMimeByExt(ext)
	}
}
