# 团队与协作

说明团队的成员模型、角色权限、邀请加入流程，以及清单/逐条共享机制。相关代码：
`backend/internal/modules/team/`（团队、成员、邀请）、`backend/internal/middleware/auth.go`（角色与鉴权）、
`backend/internal/models/models.go`（模型）、`frontend/src/pages/TeamPage.tsx`（团队页）。

> 日历数据的可见性规则见 `docs/calendar.md`；CalDAV 兼容性见 `docs/CALDAV.md`。

## 团队与空间模型

- 注册时自动创建一个团队（`models.Team`），创建者即 **owner**（`Team.OwnerID`）。
- 用户通过 `models.TeamMember` 关联团队，**一个用户可属于多个团队**（`TeamMember` 是复合主键
  `(UserID, TeamID)`），可随时切换当前所在团队。
- 团队成员身份（角色）保存在 `TeamMember.Role`，登录时同步到 `User.Role`。

## 角色与权限

两个角色（`backend/internal/middleware/auth.go`）：

| 角色 | 值 | 能力 |
|---|---|---|
| **parent（家长/管理员）** | `"parent"` | 团队管理：改团队名、创建邀请、设置成员角色、重置 Calendar Token |
| **child（子成员/普通）** | `"child"` | 使用共享内容，无团队管理权限 |

- 所有登录用户（parent + child）都可读写自己有权访问的共享内容（`RequireWriteRole`）。
- `owner` 始终是 parent，且**不能被降级**；parent 可把其他成员在 parent/child 之间切换
  （`PATCH /api/v1/team/members/:id/role`，`setMemberRole`，仅 parent，禁止改 owner 角色）。
- 注册者为 parent；通过邀请加入的默认 child（除非邀请创建时指定 parent）。
- 鉴权中间件 `RequireRole` 每次请求从 DB 现读角色，角色改动即时生效。

## 邀请与加入

- parent 在团队页创建邀请链接（`POST /api/v1/invites`），默认角色 child，**7 天有效**、一次性。
- 受邀人打开 `/join?code=<code>`（`JoinPage`）输入邀请码加入：
  - 之前已在同一团队 → **保留已有角色**（不会被邀请角色覆盖，如已由 parent 手动提升的成员不被降级）；
  - 未加入 → 按邀请角色创建 `TeamMember`。
- 加入后自动切换到新团队，并返回该团队的新会话 Token。
- 邀请信息只读接口 `GET /api/v1/invites/info`（无需登录）供加入页展示团队名与角色。

## 多团队切换

- `GET /api/v1/auth/families` 列出用户所属全部团队；`POST /api/v1/auth/switch-team` 切换。
- 所有数据均按 `team_id` 隔离（中间件 `middleware.DB` 自动限定当前团队），跨团队互不可见。

## 共享机制（成员级）

除内置的"个人（self）/ 团队（team）"归属外，共享按成员粒度控制：

### 清单级共享（待办清单 / 笔记清单）

- 自定义清单 = 一个自定义日历行（`models.Calendar`，`Components` 为 `VTODO` 或 `VJOURNAL`），
  `Access=members` 时仅 **owner + `CalendarShare` 中的成员** 可见可写。
- `CalendarShare(TeamID, Calendar, UserID)` 成员级共享表，owner 恒有权限（无需行）。
- REST：`/api/v1/todo-lists`、`/api/v1/note-lists`（增删改、重设共享成员，**仅 owner**）。
- 权限判断：`canWriteTodo`（todos/handler.go）、`canWrite`（notes/handler.go）——self 仅创建者、
  team 全员、自定义清单按 `TodoListRole`（成员名单）判定；清单改名/换图标/改共享/删除仅 owner。
- CalDAV 对 `Access=members` 清单做成员级过滤（非成员不可见不可写），`legacy` 日历维持全员可见旧行为。

### 逐条邀请（事件 / 待办 / 笔记）

把单个条目"分享给指定成员"采用 **副本（copy）机制**，不引入新协议：

- 主行记录受邀者（`Attendees` JSON，RFC 5545 ATTENDEE 语义），同时为每位受邀成员在**其个人
  `self` 空间** upsert 一条**私有副本**（同一 UID）。
- 规则（`calendar/attendee.go` `needsInviteCopy`/`SyncEventInvites`）：
  - 受邀者**本来就能看到**该条目（如团队共享项、已有清单权限）→ 不建副本，避免重复；
  - 移除受邀者 / 删除主条目 → 级联删除/软删其副本（软删的副本与主条目一起进回收站）；
  - 受邀者本地编辑（完成状态、内容）只影响自己的副本，不回写 organizer（副本一律 private）。
- 适用：日历事件邀请（`attendees` 字段）、待办邀请（`models.Todo.Attendees`）、笔记逐条分享
  （复用 `SyncEventInvites`，`/api/v1/notes` 加 `attendees`）。
- VEVENT/VTODO/VJOURNAL 序列化/解析均读写 `ATTENDEE;CN=…;PARTSTAT=…`，CalDAV 客户端 PUT 回传
  的 ATTENDEE 也会落库。
- 附件不复制到副本（附件随 organizer 主行）。

### 文件库

- 双 scope：`public`（团队公共，全员可见）与 `personal`（仅本人）。
- 列表显示上传者；无权限修改/删除时前端给出错误提示（403）。
- 普通成员只能删除自己上传的附件；parent 可删除全部。

## 协作数据可见性一览

| 数据 | 归属 / 共享方式 | 谁可见 | 谁能写 |
|---|---|---|---|
| 个人事件 / 待办 / 笔记 | `self` | 仅创建者 | 创建者 |
| 团队事件 / 待办 / 笔记 | `team` | 全员 | 全员 |
| busy 事件 | `team` + 脱敏 | 全员（他人只见"忙碌"占位） | 全员 |
| 自定义清单 | `members` + `CalendarShare` | owner + 共享成员 | 清单内全员 |
| 逐条邀请条目 | 主行 + 受邀者 `self` 副本 | 受邀者个人可见 | 各自副本独立 |
| 文件 | `public` / `personal` | 全员 / 本人 | 见文件库规则 |

> 忙碌脱敏、个人/团队 iCal 口径等细节见 `docs/calendar.md`。
