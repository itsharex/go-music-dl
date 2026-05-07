package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/guohuiyuan/music-lib/model"
	"github.com/guohuiyuan/music-lib/soda"
	"github.com/guohuiyuan/music-lib/utils"
)

type DownloadedSong struct {
	Data        []byte
	Ext         string
	ContentType string
	Filename    string
	SavedPath   string
	Warning     string
}

func DownloadSongData(song *model.Song, withCover bool, withLyrics bool) (*DownloadedSong, error) {
	return DownloadSongDataWithTemplate(song, withCover, withLyrics, DefaultDownloadFilenameTemplate)
}

func DownloadSongDataWithTemplate(song *model.Song, withCover bool, withLyrics bool, filenameTemplate string) (*DownloadedSong, error) {
	if song == nil {
		return nil, errors.New("song is nil")
	}
	if strings.TrimSpace(song.ID) == "" || strings.TrimSpace(song.Source) == "" {
		return nil, errors.New("missing song id or source")
	}

	normalized := *song
	normalized.Name = strings.TrimSpace(normalized.Name)
	normalized.Artist = strings.TrimSpace(normalized.Artist)
	normalized.Album = strings.TrimSpace(normalized.Album)
	if normalized.Name == "" {
		normalized.Name = "Unknown"
	}
	if normalized.Artist == "" {
		normalized.Artist = "Unknown"
	}

	audioData, contentType, err := fetchSongAudio(&normalized)
	if err != nil {
		return nil, err
	}

	signatureExt := DetectAudioExtBySignature(audioData)
	ext := signatureExt
	if ext == "" {
		ext = DetectAudioExtByContentType(contentType)
	}
	if ext == "" {
		ext = DetectAudioExt(audioData)
	}

	var lyric string
	if withLyrics {
		if lyricFn := GetLyricFunc(normalized.Source); lyricFn != nil {
			lyric, _ = lyricFn(&normalized)
		}
	}

	var coverData []byte
	var coverMime string
	if withCover && strings.TrimSpace(normalized.Cover) != "" {
		coverData, coverMime, _ = FetchBytesWithMime(normalized.Cover, normalized.Source)
	}

	finalData := audioData
	warning := ""
	if (ext == "mp3" || ext == "flac" || ext == "m4a" || ext == "wma") && (normalized.Album != "" || lyric != "" || len(coverData) > 0) {
		embeddedData, embedErr := EmbedSongMetadata(audioData, &normalized, lyric, coverData, coverMime)
		switch {
		case embedErr == nil:
			finalData = embeddedData
		case errors.Is(embedErr, ErrFFmpegNotFound):
			warning = "ffmpeg not found, metadata embedding skipped"
		default:
			warning = "metadata embedding failed, using original audio"
		}
	}

	if ext == "" {
		ext = DetectAudioExt(finalData)
	}

	return &DownloadedSong{
		Data:        finalData,
		Ext:         ext,
		ContentType: AudioMimeByExt(ext),
		Filename:    BuildDownloadFilename(&normalized, ext, filenameTemplate),
		Warning:     warning,
	}, nil
}

func SaveSongToFile(song *model.Song, outDir string, withCover bool, withLyrics bool) (*DownloadedSong, error) {
	return SaveSongToFileWithTemplate(song, outDir, withCover, withLyrics, DefaultDownloadFilenameTemplate)
}

func SaveSongToFileWithTemplate(song *model.Song, outDir string, withCover bool, withLyrics bool, filenameTemplate string) (*DownloadedSong, error) {
	result, err := DownloadSongDataWithTemplate(song, withCover, withLyrics, filenameTemplate)
	if err != nil {
		return nil, err
	}

	targetDir := strings.TrimSpace(outDir)
	if targetDir == "" {
		targetDir = DefaultWebDownloadDir
	}
	targetDir = filepath.Clean(targetDir)

	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return nil, err
	}

	fileName := result.Filename
	filePath := filepath.Join(targetDir, fileName)
	if err := os.WriteFile(filePath, result.Data, 0644); err != nil {
		return nil, err
	}

	result.Filename = fileName
	result.SavedPath = filePath
	return result, nil
}

func BuildDownloadFilename(song *model.Song, ext string, filenameTemplate string) string {
	template := strings.TrimSpace(filenameTemplate)
	if template == "" {
		template = DefaultDownloadFilenameTemplate
	}
	ext = strings.TrimSpace(strings.TrimPrefix(ext, "."))

	name := "Unknown"
	artist := "Unknown"
	album := ""
	source := ""
	id := ""
	if song != nil {
		if strings.TrimSpace(song.Name) != "" {
			name = strings.TrimSpace(song.Name)
		}
		if strings.TrimSpace(song.Artist) != "" {
			artist = strings.TrimSpace(song.Artist)
		}
		album = strings.TrimSpace(song.Album)
		source = strings.TrimSpace(song.Source)
		id = strings.TrimSpace(song.ID)
	}

	hasExtToken := strings.Contains(template, "{ext}")
	rendered := strings.NewReplacer(
		"{name}", name,
		"{artist}", artist,
		"{album}", album,
		"{source}", source,
		"{id}", id,
		"{ext}", ext,
	).Replace(template)
	rendered = strings.TrimSpace(rendered)
	if rendered == "" {
		rendered = strings.TrimSpace(DefaultDownloadFilenameTemplate)
		rendered = strings.NewReplacer("{name}", name, "{artist}", artist, "{album}", album, "{source}", source, "{id}", id, "{ext}", ext).Replace(rendered)
	}
	if !hasExtToken && ext != "" {
		rendered += "." + ext
	}

	return utils.SanitizeFilename(rendered)
}

func fetchSongAudio(song *model.Song) ([]byte, string, error) {
	if song.Source == "soda" {
		cookie := CM.Get("soda")
		sodaInst := soda.New(cookie)
		info, err := sodaInst.GetDownloadInfo(song)
		if err != nil {
			return nil, "", err
		}

		encryptedData, _, err := FetchBytesWithMime(info.URL, "soda")
		if err != nil {
			return nil, "", err
		}

		finalData, err := soda.DecryptAudio(encryptedData, info.PlayAuth)
		if err != nil {
			return nil, "", err
		}
		return finalData, "", nil
	}

	dlFunc := GetDownloadFunc(song.Source)
	if dlFunc == nil {
		return nil, "", fmt.Errorf("unsupported source: %s", song.Source)
	}

	urlStr, err := dlFunc(song)
	if err != nil {
		return nil, "", err
	}
	if urlStr == "" {
		return nil, "", errors.New("empty download url")
	}

	return FetchBytesWithMime(urlStr, song.Source)
}
