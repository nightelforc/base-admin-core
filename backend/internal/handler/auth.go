package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/base-admin/base-admin-core/backend/internal/response"
	"github.com/base-admin/base-admin-core/backend/internal/service"
)

type loginRequest struct {
	Username string `json:"username" binding:"required,min=1,max=64"`
	Password string `json:"password" binding:"required,min=1,max=128"`
}

type refreshRequest struct {
	RefreshToken string `json:"refreshToken" binding:"required"`
}

type profileRequest struct {
	Nickname string `json:"nickname" binding:"max=64"`
	Email    string `json:"email" binding:"omitempty,email,max=128"`
	Phone    string `json:"phone" binding:"max=32"`
}

type passwordRequest struct {
	OldPassword string `json:"oldPassword" binding:"required"`
	NewPassword string `json:"newPassword" binding:"required,min=8,max=72"`
}

// Login authenticates a user and returns a token pair.
func (h *Handler) Login(c *gin.Context) {
	var request loginRequest
	if err := response.BindJSON(c, &request); err != nil {
		response.Fail(c, err)
		return
	}

	pair, err := h.Auth.Login(c.Request.Context(), service.LoginInput{
		Username:  request.Username,
		Password:  request.Password,
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, pair)
}

// Refresh exchanges a refresh token for a new pair.
func (h *Handler) Refresh(c *gin.Context) {
	var request refreshRequest
	if err := response.BindJSON(c, &request); err != nil {
		response.Fail(c, err)
		return
	}

	pair, err := h.Auth.Refresh(c.Request.Context(), request.RefreshToken)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, pair)
}

// Logout revokes the current session.
func (h *Handler) Logout(c *gin.Context) {
	principal, err := actor(c)
	if err != nil {
		response.Fail(c, err)
		return
	}

	if err := h.Auth.Logout(c.Request.Context(), principal.SessionID); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}

// Profile returns the identity of the current user.
func (h *Handler) Profile(c *gin.Context) {
	principal, err := actor(c)
	if err != nil {
		response.Fail(c, err)
		return
	}

	profile, err := h.Auth.Profile(c.Request.Context(), principal.UserID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, profile)
}

// UpdateProfile edits the current user's own fields.
func (h *Handler) UpdateProfile(c *gin.Context) {
	principal, err := actor(c)
	if err != nil {
		response.Fail(c, err)
		return
	}

	var request profileRequest
	if err := response.BindJSON(c, &request); err != nil {
		response.Fail(c, err)
		return
	}

	profile, err := h.Auth.UpdateProfile(c.Request.Context(), principal.UserID, request.Nickname, request.Email, request.Phone)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, profile)
}

// ChangePassword updates the current user's password and revokes other sessions.
func (h *Handler) ChangePassword(c *gin.Context) {
	principal, err := actor(c)
	if err != nil {
		response.Fail(c, err)
		return
	}

	var request passwordRequest
	if err := response.BindJSON(c, &request); err != nil {
		response.Fail(c, err)
		return
	}

	if err := h.Auth.ChangePassword(c.Request.Context(), principal.UserID, request.OldPassword, request.NewPassword); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}

// AccessCodes returns the permission codes of the current user, used by the
// frontend to hide actions the caller cannot perform.
func (h *Handler) AccessCodes(c *gin.Context) {
	principal, err := actor(c)
	if err != nil {
		response.Fail(c, err)
		return
	}

	codes, err := h.Menus.PermissionCodesFor(c.Request.Context(), principal.UserID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, codes)
}

// RouteMenus returns the route records the frontend router renders.
func (h *Handler) RouteMenus(c *gin.Context) {
	principal, err := actor(c)
	if err != nil {
		response.Fail(c, err)
		return
	}

	routes, err := h.Menus.Routes(c.Request.Context(), principal.UserID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, routes)
}
