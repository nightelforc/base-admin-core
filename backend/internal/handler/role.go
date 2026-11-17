package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/nightelforc/base-admin-core/backend/internal/service"
	"github.com/nightelforc/base-admin-core/backend/pkg/response"
)

type roleRequest struct {
	Code        string `json:"code" binding:"required,min=2,max=64"`
	Name        string `json:"name" binding:"required,min=2,max=64"`
	Description string `json:"description" binding:"max=255"`
	Sort        int    `json:"sort"`
	Status      *int16 `json:"status"`
}

type assignMenusRequest struct {
	MenuIDs []int64 `json:"menuIds"`
}

// ListRoles returns a page of roles.
func (h *Handler) ListRoles(c *gin.Context) {
	page, pageSize := pagination(c)

	items, total, err := h.Roles.List(c.Request.Context(), c.Query("keyword"), statusFilter(c), page, pageSize)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, response.NewPage(items, total, page, pageSize))
}

// RoleOptions returns the enabled roles for select inputs.
func (h *Handler) RoleOptions(c *gin.Context) {
	options, err := h.Roles.Options(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, options)
}

// CreateRole creates a role.
func (h *Handler) CreateRole(c *gin.Context) {
	var request roleRequest
	if err := response.BindJSON(c, &request); err != nil {
		response.Fail(c, err)
		return
	}

	status := int16(1)
	if request.Status != nil {
		status = *request.Status
	}

	id, err := h.Roles.Create(c.Request.Context(), service.RoleInput{
		Code:        request.Code,
		Name:        request.Name,
		Description: request.Description,
		Sort:        request.Sort,
		Status:      status,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, gin.H{"id": id})
}

// UpdateRole edits a role.
func (h *Handler) UpdateRole(c *gin.Context) {
	id, err := idParam(c)
	if err != nil {
		response.Fail(c, err)
		return
	}

	var request roleRequest
	if err := response.BindJSON(c, &request); err != nil {
		response.Fail(c, err)
		return
	}

	status := int16(1)
	if request.Status != nil {
		status = *request.Status
	}

	if err := h.Roles.Update(c.Request.Context(), id, service.RoleInput{
		Code:        request.Code,
		Name:        request.Name,
		Description: request.Description,
		Sort:        request.Sort,
		Status:      status,
	}); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}

// DeleteRole removes a role that is not built in and has no members.
func (h *Handler) DeleteRole(c *gin.Context) {
	id, err := idParam(c)
	if err != nil {
		response.Fail(c, err)
		return
	}

	if err := h.Roles.Delete(c.Request.Context(), id); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}

// RoleMenus returns the menu identifiers granted to a role.
func (h *Handler) RoleMenus(c *gin.Context) {
	id, err := idParam(c)
	if err != nil {
		response.Fail(c, err)
		return
	}

	menuIDs, err := h.Roles.MenuIDs(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, menuIDs)
}

// AssignRoleMenus replaces the menus granted to a role.
func (h *Handler) AssignRoleMenus(c *gin.Context) {
	id, err := idParam(c)
	if err != nil {
		response.Fail(c, err)
		return
	}

	var request assignMenusRequest
	if err := response.BindJSON(c, &request); err != nil {
		response.Fail(c, err)
		return
	}

	if err := h.Roles.AssignMenus(c.Request.Context(), id, request.MenuIDs); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}
