package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/base-admin/base-admin-core/backend/internal/service"
	"github.com/base-admin/base-admin-core/backend/pkg/response"
)

type createUserRequest struct {
	Username string  `json:"username" binding:"required,min=3,max=32"`
	Password string  `json:"password" binding:"required,min=8,max=72"`
	Nickname string  `json:"nickname" binding:"max=64"`
	Email    string  `json:"email" binding:"omitempty,email,max=128"`
	Phone    string  `json:"phone" binding:"max=32"`
	Remark   string  `json:"remark" binding:"max=255"`
	Status   *int16  `json:"status"`
	RoleIDs  []int64 `json:"roleIds"`
}

type updateUserRequest struct {
	Nickname string  `json:"nickname" binding:"max=64"`
	Email    string  `json:"email" binding:"omitempty,email,max=128"`
	Phone    string  `json:"phone" binding:"max=32"`
	Remark   string  `json:"remark" binding:"max=255"`
	Status   *int16  `json:"status"`
	RoleIDs  []int64 `json:"roleIds"`
}

type resetPasswordRequest struct {
	Password string `json:"password" binding:"required,min=8,max=72"`
}

type userStatusRequest struct {
	Status *int16 `json:"status" binding:"required,oneof=0 1"`
}

// ListUsers returns a page of users.
func (h *Handler) ListUsers(c *gin.Context) {
	page, pageSize := pagination(c)
	roleID, _ := strconv.ParseInt(c.Query("roleId"), 10, 64)

	items, total, err := h.Users.List(c.Request.Context(), service.ListUsersInput{
		Keyword:  c.Query("keyword"),
		Status:   statusFilter(c),
		RoleID:   roleID,
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, response.NewPage(items, total, page, pageSize))
}

// CreateUser creates a user and returns its identifier.
func (h *Handler) CreateUser(c *gin.Context) {
	var request createUserRequest
	if err := response.BindJSON(c, &request); err != nil {
		response.Fail(c, err)
		return
	}

	status := int16(1)
	if request.Status != nil {
		status = *request.Status
	}

	id, err := h.Users.Create(c.Request.Context(), service.CreateUserInput{
		Username: request.Username,
		Password: request.Password,
		Nickname: request.Nickname,
		Email:    request.Email,
		Phone:    request.Phone,
		Remark:   request.Remark,
		Status:   status,
		RoleIDs:  request.RoleIDs,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, gin.H{"id": id})
}

// UpdateUser edits a user.
func (h *Handler) UpdateUser(c *gin.Context) {
	id, err := idParam(c)
	if err != nil {
		response.Fail(c, err)
		return
	}
	principal, err := actor(c)
	if err != nil {
		response.Fail(c, err)
		return
	}

	var request updateUserRequest
	if err := response.BindJSON(c, &request); err != nil {
		response.Fail(c, err)
		return
	}

	status := int16(1)
	if request.Status != nil {
		status = *request.Status
	}

	if err := h.Users.Update(c.Request.Context(), principal.UserID, id, service.UpdateUserInput{
		Nickname: request.Nickname,
		Email:    request.Email,
		Phone:    request.Phone,
		Remark:   request.Remark,
		Status:   status,
		RoleIDs:  request.RoleIDs,
	}); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}

// DeleteUser removes a user.
func (h *Handler) DeleteUser(c *gin.Context) {
	id, err := idParam(c)
	if err != nil {
		response.Fail(c, err)
		return
	}
	principal, err := actor(c)
	if err != nil {
		response.Fail(c, err)
		return
	}

	if err := h.Users.Delete(c.Request.Context(), principal.UserID, id); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}

// ResetUserPassword sets a new password and revokes the user's sessions.
func (h *Handler) ResetUserPassword(c *gin.Context) {
	id, err := idParam(c)
	if err != nil {
		response.Fail(c, err)
		return
	}

	var request resetPasswordRequest
	if err := response.BindJSON(c, &request); err != nil {
		response.Fail(c, err)
		return
	}

	if err := h.Users.ResetPassword(c.Request.Context(), id, request.Password); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}

// SetUserStatus enables or disables a user.
func (h *Handler) SetUserStatus(c *gin.Context) {
	id, err := idParam(c)
	if err != nil {
		response.Fail(c, err)
		return
	}
	principal, err := actor(c)
	if err != nil {
		response.Fail(c, err)
		return
	}

	var request userStatusRequest
	if err := response.BindJSON(c, &request); err != nil {
		response.Fail(c, err)
		return
	}

	if err := h.Users.SetStatus(c.Request.Context(), principal.UserID, id, *request.Status); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}
