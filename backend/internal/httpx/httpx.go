package httpx

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func write(c *gin.Context, status int, key string, errorArgs ...map[string]any) {
	h := gin.H{"error": key}
	if len(errorArgs) > 0 && len(errorArgs[0]) > 0 {
		h["errorArgs"] = errorArgs[0]
	}
	c.JSON(status, h)
}

func ErrT(c *gin.Context, status int, key string, errorArgs ...map[string]any) {
	write(c, status, key, errorArgs...)
}

func BadRequestT(c *gin.Context, key string, errorArgs ...map[string]any) {
	write(c, http.StatusBadRequest, key, errorArgs...)
}

func UnauthorizedT(c *gin.Context, key string, errorArgs ...map[string]any) {
	write(c, http.StatusUnauthorized, key, errorArgs...)
}

func ForbiddenT(c *gin.Context, key string, errorArgs ...map[string]any) {
	write(c, http.StatusForbidden, key, errorArgs...)
}

func NotFoundT(c *gin.Context, key string, errorArgs ...map[string]any) {
	write(c, http.StatusNotFound, key, errorArgs...)
}

func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"data": data})
}

func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, gin.H{"data": data})
}

func Bind(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		BadRequestT(c, "bad_request")
		return false
	}
	return true
}
