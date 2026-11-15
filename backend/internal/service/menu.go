package service

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/base-admin/base-admin-core/backend/internal/model"
	"github.com/base-admin/base-admin-core/backend/internal/repository"
	"github.com/base-admin/base-admin-core/backend/pkg/apperr"
)

// routeNamePattern matches what vue-router accepts as a route name.
var routeNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{1,63}$`)

// MenuItem is one node of the menu management tree; buttons are included.
type MenuItem struct {
	ID         int64       `json:"id"`
	ParentID   int64       `json:"parentId"`
	Type       string      `json:"type"`
	Name       string      `json:"name"`
	Title      string      `json:"title"`
	Path       string      `json:"path"`
	Component  string      `json:"component"`
	Redirect   string      `json:"redirect"`
	Icon       string      `json:"icon"`
	Link       string      `json:"link"`
	Permission string      `json:"permission"`
	Sort       int         `json:"sort"`
	Visible    bool        `json:"visible"`
	KeepAlive  bool        `json:"keepAlive"`
	AffixTab   bool        `json:"affixTab"`
	Status     int16       `json:"status"`
	CreatedAt  time.Time   `json:"createdAt"`
	Children   []*MenuItem `json:"children"`
}

// MenuInput is the create/update payload.
type MenuInput struct {
	ParentID   int64
	Type       string
	Name       string
	Title      string
	Path       string
	Component  string
	Redirect   string
	Icon       string
	Link       string
	Permission string
	Sort       int
	Visible    bool
	KeepAlive  bool
	AffixTab   bool
	Status     int16
}

// RouteMeta mirrors the vben route meta contract.
type RouteMeta struct {
	Title      string `json:"title"`
	Icon       string `json:"icon,omitempty"`
	Order      int    `json:"order,omitempty"`
	HideInMenu bool   `json:"hideInMenu,omitempty"`
	KeepAlive  bool   `json:"keepAlive,omitempty"`
	AffixTab   bool   `json:"affixTab,omitempty"`
	Link       string `json:"link,omitempty"`
	IframeSrc  string `json:"iframeSrc,omitempty"`
}

// Route is a route record the vben router can consume directly: `component` is
// a view path such as "/system/user/index" (the ".vue" suffix is added by the
// frontend) or the layout/iframe sentinel.
type Route struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Component string    `json:"component,omitempty"`
	Redirect  string    `json:"redirect,omitempty"`
	Meta      RouteMeta `json:"meta"`
	Children  []*Route  `json:"children,omitempty"`
}

// MenuService owns the navigation tree and the permission registry.
type MenuService struct {
	menus *repository.MenuRepository
	users *repository.UserRepository
}

// NewMenuService wires the menu use cases.
func NewMenuService(menus *repository.MenuRepository, users *repository.UserRepository) *MenuService {
	return &MenuService{menus: menus, users: users}
}

// Tree returns every menu entry, buttons included, as a tree.
func (s *MenuService) Tree(ctx context.Context) ([]*MenuItem, error) {
	menus, err := s.menus.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	return buildMenuTree(menus), nil
}

// Create registers a menu entry.
func (s *MenuService) Create(ctx context.Context, in MenuInput) (int64, error) {
	menu, err := s.normalize(ctx, 0, in)
	if err != nil {
		return 0, err
	}
	if err := s.menus.Create(ctx, menu); err != nil {
		return 0, err
	}
	return menu.ID, nil
}

// Update edits a menu entry.
func (s *MenuService) Update(ctx context.Context, id int64, in MenuInput) error {
	if _, err := s.menus.FindByID(ctx, id); err != nil {
		return err
	}

	menu, err := s.normalize(ctx, id, in)
	if err != nil {
		return err
	}

	return s.menus.UpdateFields(ctx, id, map[string]any{
		"parent_id":  menu.ParentID,
		"type":       menu.Type,
		"name":       menu.Name,
		"title":      menu.Title,
		"path":       menu.Path,
		"component":  menu.Component,
		"redirect":   menu.Redirect,
		"icon":       menu.Icon,
		"link":       menu.Link,
		"permission": menu.Permission,
		"sort":       menu.Sort,
		"visible":    menu.Visible,
		"keep_alive": menu.KeepAlive,
		"affix_tab":  menu.AffixTab,
		"status":     menu.Status,
	})
}

// Delete removes a leaf menu.
func (s *MenuService) Delete(ctx context.Context, id int64) error {
	if _, err := s.menus.FindByID(ctx, id); err != nil {
		return err
	}

	children, err := s.menus.CountChildren(ctx, id)
	if err != nil {
		return err
	}
	if children > 0 {
		return apperr.ErrConflict.WithMessage("请先删除该菜单下的子菜单")
	}
	return s.menus.Delete(ctx, id)
}

// PermissionCodesFor returns the permission codes granted to a user. A super
// administrator receives the whole catalog, since its role is not granted
// entry by entry.
func (s *MenuService) PermissionCodesFor(ctx context.Context, userID int64) ([]string, error) {
	superAdmin, err := s.users.IsSuperAdmin(ctx, userID)
	if err != nil {
		return nil, err
	}
	if superAdmin {
		return s.menus.AllPermissionCodes(ctx)
	}
	return s.users.PermissionCodes(ctx, userID)
}

// Routes builds the route records visible to a user, ready for the frontend
// router. Buttons are excluded: they only contribute permission codes.
func (s *MenuService) Routes(ctx context.Context, userID int64) ([]*Route, error) {
	superAdmin, err := s.users.IsSuperAdmin(ctx, userID)
	if err != nil {
		return nil, err
	}

	var menus []*model.Menu
	if superAdmin {
		menus, err = s.menus.ListAll(ctx)
	} else {
		menus, err = s.menus.ListForUser(ctx, userID)
	}
	if err != nil {
		return nil, err
	}

	routable := make([]*model.Menu, 0, len(menus))
	for _, menu := range menus {
		if menu.IsRoutable() && menu.IsEnabled() {
			routable = append(routable, menu)
		}
	}
	sort.SliceStable(routable, func(i, j int) bool {
		if routable[i].Sort != routable[j].Sort {
			return routable[i].Sort < routable[j].Sort
		}
		return routable[i].ID < routable[j].ID
	})

	return buildRoutes(routable), nil
}

func (s *MenuService) normalize(ctx context.Context, id int64, in MenuInput) (*model.Menu, error) {
	menuType := strings.TrimSpace(in.Type)
	if !model.IsValidMenuType(menuType) {
		return nil, apperr.ErrInvalidParams.WithMessage("菜单类型不合法")
	}

	name := strings.TrimSpace(in.Name)
	if !routeNamePattern.MatchString(name) {
		return nil, apperr.ErrInvalidParams.WithMessage("路由名称需为 2-64 位字母、数字、下划线或中划线，且以字母开头")
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, apperr.ErrInvalidParams.WithMessage("请填写菜单标题")
	}

	exists, err := s.menus.ExistsByName(ctx, name, id)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, apperr.ErrMenuNameTaken
	}

	parentID := in.ParentID
	if parentID < 0 {
		parentID = 0
	}
	if parentID > 0 {
		if parentID == id {
			return nil, apperr.ErrInvalidParams.WithMessage("上级菜单不能是自身")
		}
		parent, err := s.menus.FindByID(ctx, parentID)
		if err != nil {
			return nil, apperr.ErrInvalidParams.WithMessage("上级菜单不存在")
		}
		if parent.Type == model.MenuTypeButton {
			return nil, apperr.ErrInvalidParams.WithMessage("按钮下不能再挂载子菜单")
		}
		if id > 0 {
			if err := s.assertNoCycle(ctx, id, parentID); err != nil {
				return nil, err
			}
		}
	}

	path := strings.TrimSpace(in.Path)
	component := strings.TrimSpace(in.Component)
	link := strings.TrimSpace(in.Link)
	permission := strings.TrimSpace(in.Permission)

	switch menuType {
	case model.MenuTypeCatalog:
		if !strings.HasPrefix(path, "/") {
			return nil, apperr.ErrInvalidParams.WithMessage("目录路径需以 / 开头")
		}
		component = ""
		permission = ""
	case model.MenuTypeMenu:
		if !strings.HasPrefix(path, "/") {
			return nil, apperr.ErrInvalidParams.WithMessage("页面路径需以 / 开头")
		}
		if !strings.HasPrefix(component, "/") {
			return nil, apperr.ErrInvalidParams.WithMessage("页面组件需以 / 开头，例如 /system/user/index")
		}
	case model.MenuTypeEmbedded, model.MenuTypeLink:
		if !isHTTPURL(link) {
			return nil, apperr.ErrInvalidParams.WithMessage("请填写以 http:// 或 https:// 开头的外部地址")
		}
		component = ""
		if path == "" {
			path = "/external/" + strings.ToLower(name)
		}
	case model.MenuTypeButton:
		if parentID == 0 {
			return nil, apperr.ErrInvalidParams.WithMessage("按钮必须挂在某个页面下")
		}
		if permission == "" {
			return nil, apperr.ErrInvalidParams.WithMessage("请填写权限标识，例如 system:user:create")
		}
		path = ""
		component = ""
	}

	return &model.Menu{
		ParentID:   parentID,
		Type:       menuType,
		Name:       name,
		Title:      title,
		Path:       path,
		Component:  component,
		Redirect:   strings.TrimSpace(in.Redirect),
		Icon:       strings.TrimSpace(in.Icon),
		Link:       link,
		Permission: permission,
		Sort:       in.Sort,
		Visible:    in.Visible,
		KeepAlive:  in.KeepAlive,
		AffixTab:   in.AffixTab,
		Status:     normalizeStatus(in.Status),
	}, nil
}

// assertNoCycle walks up from candidateParent and refuses a parent that
// eventually points back at the menu being edited.
func (s *MenuService) assertNoCycle(ctx context.Context, id, candidateParent int64) error {
	visited := map[int64]struct{}{id: {}}
	current := candidateParent
	for current > 0 {
		if _, ok := visited[current]; ok {
			return apperr.ErrInvalidParams.WithMessage("上级菜单不能是自身的下级")
		}
		visited[current] = struct{}{}

		parent, err := s.menus.FindByID(ctx, current)
		if err != nil {
			return nil
		}
		current = parent.ParentID
	}
	return nil
}

func buildMenuTree(menus []*model.Menu) []*MenuItem {
	nodes := make(map[int64]*MenuItem, len(menus))
	for _, menu := range menus {
		nodes[menu.ID] = &MenuItem{
			ID:         menu.ID,
			ParentID:   menu.ParentID,
			Type:       menu.Type,
			Name:       menu.Name,
			Title:      menu.Title,
			Path:       menu.Path,
			Component:  menu.Component,
			Redirect:   menu.Redirect,
			Icon:       menu.Icon,
			Link:       menu.Link,
			Permission: menu.Permission,
			Sort:       menu.Sort,
			Visible:    menu.Visible,
			KeepAlive:  menu.KeepAlive,
			AffixTab:   menu.AffixTab,
			Status:     menu.Status,
			CreatedAt:  menu.CreatedAt,
			Children:   make([]*MenuItem, 0, 4),
		}
	}

	roots := make([]*MenuItem, 0, 8)
	for _, menu := range menus {
		node := nodes[menu.ID]
		if parent, ok := nodes[menu.ParentID]; ok && menu.ParentID != 0 {
			parent.Children = append(parent.Children, node)
			continue
		}
		roots = append(roots, node)
	}

	var order func(items []*MenuItem)
	order = func(items []*MenuItem) {
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].Sort != items[j].Sort {
				return items[i].Sort < items[j].Sort
			}
			return items[i].ID < items[j].ID
		})
		for _, item := range items {
			order(item.Children)
		}
	}
	order(roots)
	return roots
}

func buildRoutes(menus []*model.Menu) []*Route {
	nodes := make(map[int64]*Route, len(menus))
	for _, menu := range menus {
		nodes[menu.ID] = toRoute(menu)
	}

	roots := make([]*Route, 0, 8)
	for _, menu := range menus {
		route := nodes[menu.ID]
		if parent, ok := nodes[menu.ParentID]; ok && menu.ParentID != 0 {
			parent.Children = append(parent.Children, route)
			continue
		}
		roots = append(roots, route)
	}

	var finish func(items []*Route)
	finish = func(items []*Route) {
		for _, item := range items {
			if len(item.Children) > 0 {
				// vben deletes the component of any route that has children and
				// falls back to the first child when no redirect is configured.
				item.Component = ""
				if item.Redirect == "" && strings.HasPrefix(item.Children[0].Path, "/") {
					item.Redirect = item.Children[0].Path
				}
			}
			finish(item.Children)
		}
	}
	finish(roots)
	return roots
}

func toRoute(menu *model.Menu) *Route {
	route := &Route{
		Name: menu.Name,
		Path: menu.Path,
		Meta: RouteMeta{
			Title:      menu.Title,
			Icon:       menu.Icon,
			Order:      menu.Sort,
			HideInMenu: !menu.Visible,
			KeepAlive:  menu.KeepAlive,
			AffixTab:   menu.AffixTab,
		},
	}

	switch menu.Type {
	case model.MenuTypeMenu:
		route.Component = menu.Component
	case model.MenuTypeEmbedded:
		route.Component = "IFrameView"
		route.Meta.IframeSrc = menu.Link
	case model.MenuTypeLink:
		route.Component = "IFrameView"
		route.Meta.Link = menu.Link
	}
	route.Redirect = menu.Redirect
	return route
}

func isHTTPURL(value string) bool {
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}
