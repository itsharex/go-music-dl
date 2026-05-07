package web

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/guohuiyuan/go-music-dl/core"
	"github.com/guohuiyuan/music-lib/model"
)

func RegisterQRLoginRoutes(api *gin.RouterGroup) {
	api.POST("/qr_login/:source", func(c *gin.Context) {
		source := strings.TrimSpace(c.Param("source"))
		fn := core.GetQRLoginCreateFunc(source)
		if fn == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "unsupported qr login source"})
			return
		}
		session, err := fn()
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, session)
	})

	api.GET("/qr_login/:source", func(c *gin.Context) {
		source := strings.TrimSpace(c.Param("source"))
		key := strings.TrimSpace(c.Query("key"))
		if key == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "missing qr login key"})
			return
		}
		fn := core.GetQRLoginCheckFunc(source)
		if fn == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "unsupported qr login source"})
			return
		}
		result, err := fn(key)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		if result != nil && result.Status == model.QRLoginStatusSuccess && strings.TrimSpace(result.Cookie) != "" {
			core.CM.SetAll(map[string]string{source: result.Cookie})
			core.CM.Save()
		}
		c.JSON(http.StatusOK, result)
	})
}
