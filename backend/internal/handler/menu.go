package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/base-admin/base-admin-core/backend/internal/response"
	"github.com/base-admin/base-admin-core/backend/internal/service"
)

type menuRequest struct {
	ParentID   int64  `json:"parentId"`
	Type       string `json:"type" binding:"required,oneof=catalog menu button embedded link"`
	Name       string `json:"name" binding:"required,min=2,max=64"`
	Title      string `json:"title" binding:"required,max=64"`
	Path       string `json:"path" binding:"max=255"`
	Component  string `json:"component" binding:"max=255"`
	Redirect   string `json:"redirect" binding:"max=255"`
	Icon       string `json:"icon" binding:"max=64"`
	Link       string `json:"link" binding:"max=512"`
	Permission string `json:"permission" binding:"max=128"`
	Sort       int    `json:"sort"`
	Visible    *bool  `json:"visible"`
	KeepAlive  bool   `json:"keepAlive"`
	AffixTab   bool   `json:"affixTab"`
	Status     *int16 `json:"status"`
}

func (r menuRequest) toInput() service.MenuInput {
	visible := true
	if r.Visible != nil {
		visible = *r.Visible
	}
	status := int16(1)
	if r.Status != nil {
		status = *r.Status
	}

	return service.MenuInput{
		ParentID:   r.ParentID,
		Type:       r.Type,
		Name:       r.Name,
		Title:      r.Title,
		Path:       r.Path,
		Component:  r.Component,
		Redirect:   r.Redirect,
		Icon:       r.Icon,
		Link:       r.Link,
		Permission: r.Permission,
		Sort:       r.Sort,
		Visible:    visible,
		KeepAlive:  r.KeepAlive,
		AffixTab:   r.AffixTab,
		Status:     status,
	}
}

// MenuTree returns the whole menu tree for management screens.
func (h *Handler) MenuTree(c *gin.Context) {
	tree, err := h.Menus.Tree(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, tree)
}

// CreateMenu creates a menu entry.
func (h *Handler) CreateMenu(c *gin.Context) {
	var request menuRequest
	if err := response.BindJSON(c, &request); err != nil {
		response.Fail(c, err)
		return
	}

	id, err := h.Menus.Create(c.Request.Context(), request.toInput())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.Created(c, gin.H{"id": id})
}

// UpdateMenu edits a menu entry.
func (h *Handler) UpdateMenu(c *gin.Context) {
	id, err := idParam(c)
	if err != nil {
		response.Fail(c, err)
		return
	}

	var request menuRequest
	if err := response.BindJSON(c, &request); err != nil {
		response.Fail(c, err)
		return
	}

	if err := h.Menus.Update(c.Request.Context(), id, request.toInput()); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}

// DeleteMenu removes a leaf menu entry.
func (h *Handler) DeleteMenu(c *gin.Context) {
	id, err := idParam(c)
	if err != nil {
		response.Fail(c, err)
		return
	}

	if err := h.Menus.Delete(c.Request.Context(), id); err != nil {
		response.Fail(c, err)
		return
	}
	response.NoContent(c)
}
