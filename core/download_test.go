package core

import (
	"testing"

	"github.com/guohuiyuan/music-lib/model"
)

func TestBuildDownloadFilenameUsesTemplate(t *testing.T) {
	song := &model.Song{
		ID:     "12345",
		Source: "netease",
		Name:   "没地址的信",
		Artist: "阮俊霖",
		Album:  "专辑/测试",
	}

	tests := []struct {
		name     string
		template string
		ext      string
		want     string
	}{
		{
			name:     "default template appends extension",
			template: "",
			ext:      "mp3",
			want:     "没地址的信 - 阮俊霖.mp3",
		},
		{
			name:     "custom template appends extension",
			template: "{artist}/{album}/{name}",
			ext:      "flac",
			want:     "阮俊霖_专辑_测试_没地址的信.flac",
		},
		{
			name:     "extension token controls extension position",
			template: "{source}-{id}-{name}.{ext}",
			ext:      "m4a",
			want:     "netease-12345-没地址的信.m4a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BuildDownloadFilename(song, tt.ext, tt.template); got != tt.want {
				t.Fatalf("BuildDownloadFilename() = %q, want %q", got, tt.want)
			}
		})
	}
}
