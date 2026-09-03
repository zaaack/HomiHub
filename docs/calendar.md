# 日历归属与可见性

说明网页端新建的事件 / 待办进入哪个日历，以及邀请成员后谁能看到什么。相关代码：
`backend/internal/modules/calendar/`（事件、CalDAV、iCal 订阅）、`backend/internal/modules/todos/`（待办）、`backend/internal/models/models.go`（模型）。

## 两个内置日历

每条日历数据按 `calendar` 字段归属（`models.Todo.Calendar` / `models.CalendarEvent.Calendar`，取值 `"self"` 或 `"team"`）：

- **`self`（个人日历）**：只属于创建者自己。
- **`team`（团队共享日历）**：属于整个团队，所有成员可见。
- 另有通过 CalDAV `MKCALENDAR` 创建的自定义日历（存 `models.Calendar`），同样挂在团队名下。

## 网页端新建的东西去哪了

### 日历页新建事件（`CalendarPage.tsx` → `POST /api/v1/events`）

事件的可见性决定归属（`calendar/handler.go` 的 `calForVisibility`）：

| 可见性 | 值 | 存储到 |
|---|---|---|
| 仅自己（private） | `VisibilityPrivate = 1` | `self`（个人日历） |
| 仅显示忙碌（busy） | `VisibilityBusy = 2` | **`team`（团队日历）** |
| 团队可见（team，默认） | `VisibilityTeam = 3` | `team`（团队日历） |

> 所以“仅显示忙碌”的事件**属于团队日历**，不是个人日历——它是为了让团队成员知道
> 你该时段被占用，但不暴露具体安排。

### 待办页新建待办（`TodosPage.tsx` → `POST /api/v1/todos`）

- 顶部快捷添加、或弹窗中不勾选 **“共享给团队”**：`calendar = "self"`，**个人待办**，只有自己能看。
- 勾选“共享给团队”：`calendar = "team"`，**团队共享待办**，全体成员可见、可共同勾选/编辑/删除。
- 待办没有“可见性（private/busy/team）”概念，只有共享与否；有截止日期的个人待办会以
  `[待办]` 形式出现在自己的网页日历上（仅自己）。

## 忙碌（busy）事件的显示规则

busy 事件存于团队日历，但对不同人有不同展示：

- **自己**：看到完整标题 / 地点 / 详情。
- **其他成员**：只看到“忙碌”占位（标题、地点、详情被脱敏）。Web 月视图
  （`calendar/handler.go list`）、CalDAV 团队日历（`caldav.go toObject/expandObject`）都会脱敏。
- **注意**：`GET /calendar/team.ics?token=<团队 Calendar Token>` 目前**不脱敏**他人忙碌事件的标题
  （`feed.go feedTeam` 直接序列化），与 CalDAV/Web 的行为不一致；若在意隐私应补上脱敏。

## 邀请其他人之后（成员视角）

按成员身份区分可见范围：

| 数据 | 谁可见 |
|---|---|
| 个人事件（`visibility=private`） | 仅创建者 |
| 忙碌事件（`visibility=busy`） | 全员（他人只见“忙碌”占位） |
| 团队可见事件（`visibility=team`） | 全员（完整内容） |
| 个人待办（`calendar=self`） | 仅创建者 |
| 团队共享待办（`calendar=team`） | 全员（都可编辑/勾选/删除） |

外部同步时的口径一致：

- **个人 iCal**（`GET /calendar/feed.ics?token=<应用密码>`）与 CalDAV `self` 日历只含个人内容：
  自己的 private 事件 / 个人待办、全员的 team 事件、他人的 busy（脱敏“忙碌”）。
- **团队 iCal**（`GET /calendar/team.ics?token=<Calendar Token>`）与 CalDAV 团队日历含团队内容：
  team + busy 事件与团队共享待办，不含任何成员的 private 事件 / 个人待办。
