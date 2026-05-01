package core

import (
	"bytes"
	"testing"

	"github.com/dhowden/tag"
	"github.com/guohuiyuan/music-lib/model"
)

func TestEmbedSongMetadataWritesReadableMP3ID3v23LyricsAndCover(t *testing.T) {
	audioData := []byte{0xff, 0xfb, 0x90, 0x64, 0x00, 0x00, 0x00, 0x00}
	lyric := "[00:01.00]歌词测试"
	cover := []byte{0xff, 0xd8, 0xff, 0xd9}

	embedded, err := EmbedSongMetadata(audioData, &model.Song{
		Name:   "测试歌",
		Artist: "测试歌手",
		Ext:    "mp3",
	}, lyric, cover, "image/jpeg")
	if err != nil {
		t.Fatalf("EmbedSongMetadata() error = %v", err)
	}
	if bytes.HasPrefix(embedded, audioData) {
		t.Fatal("embedded data should prepend an ID3 tag before MP3 audio frames")
	}
	if !bytes.HasSuffix(embedded, audioData) {
		t.Fatal("embedded data should preserve original MP3 audio frames")
	}

	metadata, err := tag.ReadFrom(bytes.NewReader(embedded))
	if err != nil {
		t.Fatalf("ReadFrom(embedded): %v", err)
	}
	if metadata.Format() != tag.ID3v2_3 {
		t.Fatalf("metadata format = %v, want ID3v2.3", metadata.Format())
	}
	if metadata.Title() != "测试歌" {
		t.Fatalf("metadata title = %q, want 测试歌", metadata.Title())
	}
	if metadata.Artist() != "测试歌手" {
		t.Fatalf("metadata artist = %q, want 测试歌手", metadata.Artist())
	}
	if metadata.Lyrics() != lyric {
		t.Fatalf("metadata lyrics = %q, want %q", metadata.Lyrics(), lyric)
	}
	if picture := metadata.Picture(); picture == nil || !bytes.Equal(picture.Data, cover) {
		t.Fatalf("metadata picture = %#v, want embedded cover bytes", picture)
	}
}

func TestEmbedSongMetadataReplacesExistingMP3ID3Tag(t *testing.T) {
	audioData := []byte{0xff, 0xfb, 0x90, 0x64}
	first, err := EmbedSongMetadata(audioData, &model.Song{Name: "旧歌", Artist: "旧歌手", Ext: "mp3"}, "旧歌词", nil, "")
	if err != nil {
		t.Fatalf("first EmbedSongMetadata() error = %v", err)
	}

	second, err := EmbedSongMetadata(first, &model.Song{Name: "新歌", Artist: "新歌手", Ext: "mp3"}, "新歌词", nil, "")
	if err != nil {
		t.Fatalf("second EmbedSongMetadata() error = %v", err)
	}

	metadata, err := tag.ReadFrom(bytes.NewReader(second))
	if err != nil {
		t.Fatalf("ReadFrom(second): %v", err)
	}
	if metadata.Title() != "新歌" || metadata.Artist() != "新歌手" || metadata.Lyrics() != "新歌词" {
		t.Fatalf("metadata = %q/%q/%q, want 新歌/新歌手/新歌词", metadata.Title(), metadata.Artist(), metadata.Lyrics())
	}
	if !bytes.HasSuffix(second, audioData) {
		t.Fatal("re-embedded data should keep the original MP3 audio frames once")
	}
}
