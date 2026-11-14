# 接口文档

- Base URL：`http://127.0.0.1:8080/api/v1`
- 认证：除登录/刷新外，所有接口都需要 `Authorization: Bearer <accessToken>`
- 响应体统一为：

```json
{ "code": 0, "message": "ok", "data": { } }
```

`code = 0` 表示成功，非 0 见文末错误码。分页响应的 `data` 为：

```json
{ "list": [], "total": 0, "page": 1, "pageSize": 20 }
```

## 健康检查

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/healthz` | 存活检查，恒返回 `{"status":"ok","time":...}` |
| GET | `/readyz` | 就绪检查，探测 PostgreSQL + Redis，失败返回 503 |

## 认证

### POST /auth/login

```json
{ "username": "admin", "password": "Admin@123456" }
```

```json
{
  "code": 0,
  "data": {
    "accessToken": "eyJhbGciOi...",
    "refreshToken": "eyJhbGciOi...",
    "tokenType": "Bearer",
    "expiresIn": 1800
  }
}
```

失败：`40010` 用户名或密码错误；`40301` 账号已禁用；`42901` 登录过于频繁（`message` 含剩余时间）。

### POST /auth/refresh

```json
{ "refreshToken": "eyJhbGciOi..." }
```

返回与登录相同的结构（两个 token 都会轮换）。refresh token 无效/会话被撤销 → `401`。

### POST /auth/logout

删除当前会话，无请求体，`data: null`。

### GET /auth/profile

```json
{
  "userId": 1,
  "username": "admin",
  "realName": "超级管理员",
  "nickname": "超级管理员",
  "email": "admin@example.com",
  "phone": "",
  "avatar": "",
  "roles": ["super_admin"],
  "homePath": "/dashboard/workspace"
}
```

### PUT /auth/profile

```json
{ "nickname": "张三", "email": "zhangsan@example.com", "phone": "13800000000" }
```

### PUT /auth/password

```json
{ "oldPassword": "Admin@123456", "newPassword": "NewPass@2026" }
```

成功后该用户所有会话失效，需要重新登录。

### GET /auth/codes

```json
["system:user:list", "system:user:create", "system:role:list"]
```

### GET /auth/menus

返回当前用户可见的路由树（Vben 后端模式格式）：

```json
[
  {
    "path": "/dashboard",
    "name": "Dashboard",
    "redirect": "/dashboard/workspace",
    "meta": { "title": "仪表盘", "icon": "lucide:layout-dashboard", "order": 10 },
    "children": [
      {
        "path": "/dashboard/workspace",
        "name": "DashboardWorkspace",
        "component": "/dashboard/index",
        "meta": { "title": "工作台", "keepAlive": true, "affixTab": true }
      }
    ]
  }
]
```

## 仪表盘

### GET /dashboard/overview

```json
{
  "userTotal": 12,
  "userEnabled": 11,
  "roleTotal": 3,
  "menuTotal": 19,
  "loginToday": 5,
  "loginFailedToday": 1,
  "operationToday": 23,
  "trend": [{ "date": "2026-11-14", "total": 5, "success": 4, "failed": 1 }],
  "generatedAt": "2026-11-14T23:10:00+08:00"
}
```

## 用户

| 方法 | 路径 | 权限码 | 说明 |
| --- | --- | --- | --- |
| GET | `/users?keyword=&status=&roleId=&page=1&pageSize=20` | `system:user:list` | 分页列表 |
| POST | `/users` | `system:user:create` | 创建 |
| PUT | `/users/:id` | `system:user:update` | 更新 |
| DELETE | `/users/:id` | `system:user:delete` | 软删除 |
| PUT | `/users/:id/password` | `system:user:reset-password` | 重置密码 |
| PUT | `/users/:id/status` | `system:user:update` | 启用/禁用 |

列表项：

```json
{
  "id": 2,
  "username": "demo",
  "nickname": "演示账号",
  "email": "demo@example.com",
  "phone": "",
  "avatar": "",
  "remark": "",
  "status": 1,
  "roleIds": [2],
  "roleCodes": ["admin"],
  "roleNames": ["管理员"],
  "lastLoginAt": "2026-11-14T22:00:00+08:00",
  "lastLoginIp": "127.0.0.1",
  "createdAt": "2026-11-14T21:00:00+08:00"
}
```

创建：

```json
{
  "username": "demo",
  "password": "Demo@123456",
  "nickname": "演示账号",
  "email": "demo@example.com",
  "phone": "",
  "remark": "",
  "status": 1,
  "roleIds": [2]
}
```

更新不含 `username`（用户名不可改）与密码。重置密码 `{"password":"..."}`，状态 `{"status":1}`。

## 角色

| 方法 | 路径 | 权限码 | 说明 |
| --- | --- | --- | --- |
| GET | `/roles?keyword=&status=&page=1&pageSize=20` | `system:role:list` | 分页列表 |
| GET | `/roles/options` | `system:user:list` | 下拉选项 `[{id,code,name}]` |
| POST | `/roles` | `system:role:create` | 创建 |
| PUT | `/roles/:id` | `system:role:update` | 更新（内置角色不可改 code/status） |
| DELETE | `/roles/:id` | `system:role:delete` | 删除（内置角色或仍有用户时拒绝） |
| GET | `/roles/:id/menus` | `system:role:assign` | 已授权菜单 ID 数组 |
| PUT | `/roles/:id/menus` | `system:role:assign` | 覆盖式授权 `{"menuIds":[1,2]}` |

角色字段：`{id,code,name,description,sort,status,builtIn,userCount,createdAt}`。

## 菜单

| 方法 | 路径 | 权限码 | 说明 |
| --- | --- | --- | --- |
| GET | `/menus/tree` | `system:menu:list` | 全量菜单树（含按钮） |
| POST | `/menus` | `system:menu:create` | 创建 |
| PUT | `/menus/:id` | `system:menu:update` | 更新 |
| DELETE | `/menus/:id` | `system:menu:delete` | 删除（存在子菜单时拒绝） |

菜单字段：

```json
{
  "id": 21,
  "parentId": 20,
  "type": "menu",
  "name": "SystemUser",
  "title": "用户管理",
  "path": "/system/user",
  "component": "/system/user/index",
  "redirect": "",
  "icon": "lucide:users",
  "link": "",
  "permission": "",
  "sort": 1,
  "visible": true,
  "keepAlive": true,
  "affixTab": false,
  "status": 1,
  "createdAt": "2026-11-14T21:00:00+08:00",
  "children": []
}
```

`type` 取值与要求：

- `catalog`：目录，必须有以 `/` 开头的 `path`，忽略 `component`；
- `menu`：页面，`path` 与 `component` 都必须以 `/` 开头，`component` 为 `views` 下的相对路径（不带 `.vue`）；
- `button`：按钮，`parentId > 0` 且必须有 `permission`；
- `embedded` / `link`：外链，`link` 必须是 `http(s)://` 地址。

## 日志

| 方法 | 路径 | 权限码 | 说明 |
| --- | --- | --- | --- |
| GET | `/logs/login?keyword=&userId=&success=1&from=&to=&page=&pageSize=` | `system:log:list` | 登录日志 |
| GET | `/logs/operation?...` | `system:log:list` | 操作日志 |

`from` / `to` 支持 RFC3339 或 `2006-01-02`。

登录日志项：`{id,userId,username,ip,userAgent,success,message,createdAt}`。
操作日志项：`{id,userId,username,method,path,ip,userAgent,statusCode,latencyMs,requestBody,createdAt}`。

## 错误码

| code | HTTP | 含义 |
| --- | --- | --- |
| 0 | 200 | 成功 |
| 40000 | 400 | 参数错误 |
| 40010 | 400 | 用户名或密码错误 |
| 40011 | 400 | 原密码不正确 |
| 40012 | 400 | 密码强度不足 |
| 40013 | 400 | 新密码不能与旧密码相同 |
| 40014 | 400 | 用户名不可修改 |
| 40100 | 401 | 未登录或会话已失效 |
| 40101 | 401 | Token 无效 |
| 40102 | 401 | Token 已过期 |
| 40300 | 403 | 无权限 |
| 40301 | 403 | 账号已被禁用 |
| 40302 | 403 | 账号已被管理员停用 |
| 40400 | 404 | 资源不存在 |
| 40401 / 40402 / 40403 | 404 | 用户 / 角色 / 菜单不存在 |
| 40900 | 409 | 冲突 |
| 40901 / 40902 / 40903 | 409 | 用户名 / 角色标识 / 菜单名称已存在 |
| 40904 | 409 | 不能删除自己 |
| 40905 | 409 | 不能停用自己 |
| 40906 | 409 | 内置角色或最后一个超级管理员受保护 |
| 40907 | 409 | 内置菜单受保护 |
| 40908 | 409 | 角色仍被用户使用 |
| 42900 | 429 | 请求过于频繁 |
| 42901 | 429 | 登录失败次数过多 |
| 50000 | 500 | 服务器内部错误 |
